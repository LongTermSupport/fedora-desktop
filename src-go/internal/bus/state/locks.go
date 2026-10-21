package state

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

// The lock files' names (S7-S10). TODO(layout): layout.go owns these paths once it lands.
const (
	syncLockName     = "lock"        // <team>/state/lock
	seatLockName     = "seat.lock"   // <seat directory>/seat.lock
	keeperLockName   = "keeper.lock" // <session home>/keeper.lock
	injectDirName    = "inject"      // PINGBUS_HOME/inject/
	watchLockSuffix  = ".watch"      // inject/<key>.watch
	relayDirName     = "relay"       // PINGBUS_HOME/relay/
	deliveryLockName = "lock"        // relay/lock
	toldLockName     = "told.lock"   // PINGBUS_HOME/told.lock
)

const (
	// lockTries and lockRetry are v1's three tries 50 ms apart: a probe holds a shared lock for
	// an instant, and a taker must not mistake that for a holder (L1, L3).
	lockTries = 3
	lockRetry = 50 * time.Millisecond
	// waitPoll is how often a blocking lock is tried again: polled, so a context ends the wait.
	waitPoll = 10 * time.Millisecond
	// kindReadMax is how much of a lock file names its holder.
	kindReadMax = 64
	// maxClaim is the largest claim v1's reader reads back: 16 digits.
	maxClaim = 9999999999999999
	// sessionKeyLen is an inject key's length: 16 lowercase hex digits (S13).
	sessionKeyLen = 16
)

// Kind is what holds a path lock, written into the lock file (L1).
type Kind string

// The kinds v1 writes, and the one a stranger is read as.
const (
	KindWatch   Kind = "watch"
	KindWait    Kind = "wait"
	KindRecv    Kind = "recv"
	KindSeat    Kind = "seat"
	KindKeeper  Kind = "keeper"
	KindUnknown Kind = "unknown" // a holder whose first word is no kind v1 writes
)

// Waker is whether a sync lock holder of this kind wakes the session (L4).
func (k Kind) Waker() bool { return k == KindWatch || k == KindWait }

func (k Kind) syncKind() bool { return k == KindWatch || k == KindWait || k == KindRecv }

// holderKind is the kind a lock file's text names: its first word, when v1 writes that kind.
func holderKind(text string) Kind {
	if i := strings.IndexByte(text, ' '); i >= 0 {
		text = text[:i]
	}
	if k := Kind(text); k == KindWatch || k == KindWait || k == KindRecv || k == KindSeat || k == KindKeeper {
		return k
	}
	return KindUnknown
}

var (
	// ErrBusy is a lock another open file description holds; every *BusyError is one.
	ErrBusy = errors.New("held by another")
	// ErrLockOrder is a lock asked for where taking it could deadlock (L10).
	ErrLockOrder = errors.New("lock order")
)

// BusyError is a lock refused because another holds it; Holder is set for a path lock.
type BusyError struct {
	Path   string
	Holder Kind
}

func (e *BusyError) Error() string {
	if e.Holder == "" {
		return e.Path + ": " + ErrBusy.Error()
	}
	return fmt.Sprintf("%s: held by a %s process", e.Path, e.Holder)
}

// Is makes every BusyError an ErrBusy.
func (e *BusyError) Is(target error) bool { return target == ErrBusy }

// Locks takes the member's locks. Sleep is the wait between tries; nil is a real timer.
type Locks struct {
	Sleep func(ctx context.Context, d time.Duration) error
}

func sleepFor(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// PathLock is a held path lock (L1): the sync, seat, keeper or watch lock.
type PathLock struct {
	file     *os.File
	kind     Kind
	previous string
}

// Kind is what this lock wrote.
func (p *PathLock) Kind() Kind { return p.kind }

// Previous is what the file said before this lock was taken: a holder that died leaves its
// kind and claim; a release leaves it empty (L2, L6).
func (p *PathLock) Previous() string { return p.previous }

// File is the lock's descriptor, close-on-exec (L5): hand it to a child in ExtraFiles, and the
// child's copy holds the lock as long as any copy is open.
func (p *PathLock) File() *os.File { return p.file }

// Release empties the file, unlocks it (freeing every copy of the descriptor) and closes it
// (L2). A second release does nothing.
func (p *PathLock) Release() error {
	if p.file == nil {
		return nil
	}
	file := p.file
	p.file = nil
	fd := int(file.Fd())
	return errors.Join(wrapPath(file.Name(), "empty", unix.Ftruncate(fd, 0)),
		wrapPath(file.Name(), "unlock", unix.Flock(fd, unix.LOCK_UN)), file.Close())
}

func wrapPath(path, what string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s %s: %w", what, path, err)
}

// Sync takes a team's sync lock (`<state>/lock`) with kind watch, wait or recv (L4). The
// state directory must exist: the team state makes it first.
func (l Locks) Sync(ctx context.Context, stateDir string, kind Kind) (*PathLock, error) {
	if !kind.syncKind() {
		return nil, fmt.Errorf("a sync lock is taken by watch, wait or recv, not %q", kind)
	}
	return l.pathLock(ctx, filepath.Join(stateDir, syncLockName), string(kind), kind)
}

// Seat takes a seat's lock, writing `seat <claimMS>` (L1, L6).
func (l Locks) Seat(ctx context.Context, seatDir string, claimMS int64) (*PathLock, error) {
	if claimMS < 0 || claimMS > maxClaim {
		return nil, fmt.Errorf("a seat claim is 0 to %d milliseconds, not %d", int64(maxClaim), claimMS)
	}
	text := string(KindSeat) + " " + strconv.FormatInt(claimMS, 10)
	return l.pathLock(ctx, filepath.Join(seatDir, seatLockName), text, KindSeat)
}

// Keeper takes a session home's keeper lock.
func (l Locks) Keeper(ctx context.Context, home string) (*PathLock, error) {
	return l.pathLock(ctx, filepath.Join(home, keeperLockName), string(KindKeeper), KindKeeper)
}

// Watch takes the session's watch lock `inject/<key>.watch`, making inject/ 0700 first.
func (l Locks) Watch(ctx context.Context, home, key string) (*PathLock, error) {
	path, err := watchLockPath(home, key)
	if err != nil {
		return nil, err
	}
	if err = ensureLockDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	return l.pathLock(ctx, path, string(KindWatch), KindWatch)
}

func watchLockPath(home, key string) (string, error) {
	if len(key) != sessionKeyLen || strings.Trim(key, "0123456789abcdef") != "" {
		return "", fmt.Errorf("a session key is %d lowercase hex digits", sessionKeyLen)
	}
	return filepath.Join(home, injectDirName, key+watchLockSuffix), nil
}

// ensureLockDir makes dir 0700 when absent and refuses one that is not a directory.
func ensureLockDir(dir string) error {
	err := os.Mkdir(dir, dirMode)
	if err == nil || !errors.Is(err, os.ErrExist) {
		return wrapPath(dir, "make", err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("stat %s: %w", dir, err)
	}
	if !info.IsDir() {
		return refuse(dir, ClassFile, "not a directory")
	}
	return nil
}

// readHolder is the first bytes of a lock file as v1 reads them: ASCII, each other byte U+FFFD.
func readHolder(file *os.File) (string, error) {
	buf := make([]byte, kindReadMax)
	n, err := unix.Pread(int(file.Fd()), buf, 0)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", file.Name(), err)
	}
	var text strings.Builder
	for _, b := range buf[:n] {
		if b < utf8.RuneSelf {
			text.WriteByte(b)
		} else {
			text.WriteRune(utf8.RuneError)
		}
	}
	return text.String(), nil
}

// ProbeSync is L3 on a team's sync lock: the holder's kind and true when held, never creating it.
func ProbeSync(stateDir string) (Kind, bool, error) {
	return probe(filepath.Join(stateDir, syncLockName))
}

// ProbeSeat is L3 on a seat's lock.
func ProbeSeat(seatDir string) (Kind, bool, error) {
	return probe(filepath.Join(seatDir, seatLockName))
}

// ProbeKeeper is L3 on a session home's keeper lock.
func ProbeKeeper(home string) (Kind, bool, error) {
	return probe(filepath.Join(home, keeperLockName))
}

// ProbeWatch is L3 on the session's watch lock.
func ProbeWatch(home, key string) (Kind, bool, error) {
	path, err := watchLockPath(home, key)
	if err != nil {
		return "", false, err
	}
	return probe(path)
}

// probe holds LOCK_SH for an instant: refused is held (the kind read), else free.
func probe(path string) (Kind, bool, error) {
	file, err := openRegular(path, unix.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	defer closeQuietly(file)
	fd := int(file.Fd())
	err = unix.Flock(fd, unix.LOCK_SH|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		holder, readErr := readHolder(file)
		return holderKind(holder), readErr == nil, readErr
	}
	if err != nil {
		return "", false, fmt.Errorf("probe %s: %w", path, err)
	}
	return "", false, wrapPath(path, "unlock", unix.Flock(fd, unix.LOCK_UN))
}

// ReadClaim is the claim the seat lock names, held or not, taking no lock; false when the
// file is absent or names none (L6).
func ReadClaim(seatDir string) (int64, bool, error) {
	path := filepath.Join(seatDir, seatLockName)
	file, err := openRegular(path, unix.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	defer closeQuietly(file)
	text, err := readHolder(file)
	if err != nil {
		return 0, false, err
	}
	claim, ok := ParseClaim(text)
	return claim, ok, nil
}

// ParseClaim is the claim in a seat lock's text, `seat <ms>` with no leading zero and at most
// 16 digits, as v1 reads it.
func ParseClaim(text string) (int64, bool) {
	digits, found := strings.CutPrefix(text, string(KindSeat)+" ")
	if !found || digits == "" || len(digits) > 16 || (digits[0] == '0' && digits != "0") ||
		strings.Trim(digits, "0123456789") != "" {
		return 0, false
	}
	claim, err := strconv.ParseInt(digits, 10, 64)
	return claim, err == nil
}

// heldKey marks a context with the delivery and outbox locks its caller holds (L10).
type heldKey struct{}

type held uint8

const (
	heldDelivery held = 1 << iota
	heldOutbox
)

func heldIn(ctx context.Context) held {
	h, ok := ctx.Value(heldKey{}).(held)
	if !ok {
		return 0
	}
	return h
}

// WithDelivery runs fn holding the delivery lock `relay/lock` (L8), waiting for another
// holder. fn's context records the hold: the delivery lock is never asked for under the
// outbox lock, nor twice (L10), and is refused with ErrLockOrder if it is.
func (l Locks) WithDelivery(ctx context.Context, home string, fn func(context.Context) error) error {
	return l.withDelivery(ctx, home, true, fn)
}

// TryDelivery is WithDelivery without the wait: a *BusyError when another holds it.
func (l Locks) TryDelivery(ctx context.Context, home string, fn func(context.Context) error) error {
	return l.withDelivery(ctx, home, false, fn)
}

// WithDeliveryAndOutbox runs fn holding the delivery lock and then a team's outbox lock, the
// one order the pair is taken in (L10).
func (l Locks) WithDeliveryAndOutbox(ctx context.Context, home, stateDir string, fn func(context.Context) error) error {
	return l.WithDelivery(ctx, home, func(ctx context.Context) error {
		return l.WithOutbox(ctx, stateDir, fn)
	})
}

// WithOutbox runs fn holding a team's outbox lock, LOCK_EX on the state directory itself (L7),
// waiting for another holder. The directory must exist. fn's context records the hold.
func (l Locks) WithOutbox(ctx context.Context, stateDir string, fn func(context.Context) error) error {
	if heldIn(ctx)&heldOutbox != 0 {
		return fmt.Errorf("%w: an outbox lock is never taken under another", ErrLockOrder)
	}
	fd, err := unix.Open(stateDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("open %s: %w", stateDir, os.ErrNotExist)
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", stateDir, err)
	}
	return l.holding(ctx, os.NewFile(uintptr(fd), stateDir), true, heldOutbox, fn)
}

// holding takes LOCK_EX on file (polled when wait, so ctx ends the wait), runs fn with the
// hold recorded in its context, and closes the file, which releases the lock, however fn ends.
func (l Locks) holding(ctx context.Context, file *os.File, wait bool, mark held, fn func(context.Context) error) (err error) {
	defer func() { err = errors.Join(err, wrapPath(file.Name(), "close", file.Close())) }()
	if err = ctx.Err(); err != nil {
		return err
	}
	fd := int(file.Fd())
	for {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			return fmt.Errorf("lock %s: %w", file.Name(), err)
		}
		if !wait {
			return &BusyError{Path: file.Name()}
		}
		if err = l.sleep(ctx, waitPoll); err != nil {
			return err
		}
	}
	return fn(context.WithValue(ctx, heldKey{}, heldIn(ctx)|mark))
}

// WithTold runs fn holding the session's told lock, after one try (L9). false, with fn not
// run, is another teller holding it, which is telling the same change: not an error.
func WithTold(home string, fn func() error) (bool, error) {
	file, err := openRegular(filepath.Join(home, toldLockName), unix.O_RDWR|unix.O_CREAT)
	if err != nil {
		return false, err
	}
	ran := false
	err = holdTold(file, func() error {
		ran = true
		return fn()
	})
	return ran, err
}

// holdTold runs fn after one LOCK_EX|LOCK_NB try on file, then unlocks and closes it however fn
// ends; another holder means fn does not run.
func holdTold(file *os.File, fn func() error) (err error) {
	defer func() { err = errors.Join(err, wrapPath(file.Name(), "close", file.Close())) }()
	fd := int(file.Fd())
	err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock %s: %w", file.Name(), err)
	}
	defer func() { err = errors.Join(err, wrapPath(file.Name(), "unlock", unix.Flock(fd, unix.LOCK_UN))) }()
	return fn()
}

func (l Locks) sleep(ctx context.Context, d time.Duration) error {
	if l.Sleep != nil {
		return l.Sleep(ctx, d)
	}
	return sleepFor(ctx, d)
}

// pathLock is L1: up to three LOCK_EX|LOCK_NB tries on a description of its own, then Busy
// naming the holder; on success the old text is kept as Previous and text replaces it.
func (l Locks) pathLock(ctx context.Context, path, text string, kind Kind) (*PathLock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := openRegular(path, unix.O_RDWR|unix.O_CREAT)
	if err != nil {
		return nil, err
	}
	fd := int(file.Fd())
	for try := 1; ; try++ {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			closeQuietly(file)
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		if try == lockTries {
			holder, readErr := readHolder(file)
			closeQuietly(file)
			if readErr != nil {
				return nil, readErr
			}
			return nil, &BusyError{Path: path, Holder: holderKind(holder)}
		}
		if err = l.sleep(ctx, lockRetry); err != nil {
			closeQuietly(file)
			return nil, err
		}
	}
	lock := &PathLock{file: file, kind: kind}
	if lock.previous, err = readHolder(file); err == nil {
		err = unix.Ftruncate(fd, 0)
	}
	if err == nil {
		_, err = file.WriteAt([]byte(text), 0)
	}
	if err != nil {
		return nil, errors.Join(fmt.Errorf("write %s: %w", path, err), lock.Release())
	}
	return lock, nil
}

func (l Locks) withDelivery(ctx context.Context, home string, wait bool, fn func(context.Context) error) error {
	if heldIn(ctx)&(heldDelivery|heldOutbox) != 0 {
		return fmt.Errorf("%w: the delivery lock is taken first and once", ErrLockOrder)
	}
	dir := filepath.Join(home, relayDirName)
	if err := ensureLockDir(dir); err != nil {
		return err
	}
	file, err := openRegular(filepath.Join(dir, deliveryLockName), unix.O_RDWR|unix.O_CREAT)
	if err != nil {
		return err
	}
	return l.holding(ctx, file, wait, heldDelivery, fn)
}
