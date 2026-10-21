package state

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// sleeper is the injected wait: it records each wait asked for and runs onWait (the n-th wait,
// counting from 1) before returning the context's error, so no test waits on the clock.
type sleeper struct {
	mu     sync.Mutex
	waits  []time.Duration
	onWait func(n int)
}

func (s *sleeper) sleep(ctx context.Context, d time.Duration) error {
	s.mu.Lock()
	s.waits = append(s.waits, d)
	n := len(s.waits)
	s.mu.Unlock()
	if s.onWait != nil {
		s.onWait(n)
	}
	return ctx.Err()
}

func (s *sleeper) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.waits)
}

func (s *sleeper) locks() Locks { return Locks{Sleep: s.sleep} }

func readText(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func release(t *testing.T, lock *PathLock) {
	t.Helper()
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
}

// rawHold takes LOCK_EX on path on a description of its own, as some other holder would.
func rawHold(t *testing.T, path, text string) func() {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteAt([]byte(text), 0); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}
}

// heldElsewhere is whether another description fails LOCK_EX|LOCK_NB on path.
func heldElsewhere(t *testing.T, path string) bool {
	t.Helper()
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := unix.Close(fd); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return true
	}
	if err != nil {
		t.Fatal(err)
	}
	if err = unix.Flock(fd, unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	return false
}

// L1, L2: a free lock is taken at once, keeps what a dead holder left, says its kind and claim;
// a release empties it.
func TestASeatLockKeepsWhatADeadHolderLeftAndWritesItsClaim(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "seat.lock")
	if err := os.WriteFile(path, []byte("seat 123"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &sleeper{}
	lock, err := s.locks().Seat(t.Context(), dir, 456)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Previous() != "seat 123" || lock.Kind() != KindSeat {
		t.Errorf("previous %q kind %q", lock.Previous(), lock.Kind())
	}
	if claim, ok := ParseClaim(lock.Previous()); !ok || claim != 123 {
		t.Errorf("claim of the previous %d %v", claim, ok)
	}
	if got := readText(t, path); got != "seat 456" {
		t.Errorf("the lock says %q", got)
	}
	if s.count() != 0 {
		t.Errorf("a free lock waited %d times", s.count())
	}
	if !heldElsewhere(t, path) {
		t.Error("the seat lock is not held")
	}
	release(t, lock)
	if got := readText(t, path); got != "" {
		t.Errorf("released, the lock says %q", got)
	}
	if heldElsewhere(t, path) {
		t.Error("released, the seat lock is still held")
	}
	if err = lock.Release(); err != nil {
		t.Errorf("a second release: %v", err)
	}
}

// L1, L11: a second acquisition in this process is refused as one from another process is,
// after three tries 50 ms apart, naming the holder.
func TestAHeldPathLockIsBusyAfterThreeTriesNamingItsHolder(t *testing.T) {
	dir := t.TempDir()
	s := &sleeper{}
	first, err := s.locks().Sync(t.Context(), dir, KindWatch)
	if err != nil {
		t.Fatal(err)
	}
	defer release(t, first)
	_, err = s.locks().Sync(t.Context(), dir, KindRecv)
	busy, ok := errors.AsType[*BusyError](err)
	if !ok || busy.Holder != KindWatch || !errors.Is(err, ErrBusy) {
		t.Fatalf("got %v, want busy held by watch", err)
	}
	if busy.Path != filepath.Join(dir, "lock") || busy.Error() == "" {
		t.Errorf("busy path %q", busy.Path)
	}
	if s.count() != 2 || s.waits[0] != 50*time.Millisecond || s.waits[1] != 50*time.Millisecond {
		t.Errorf("waits %v, want two of 50ms", s.waits)
	}
	if got := readText(t, filepath.Join(dir, "lock")); got != "watch" {
		t.Errorf("a refused taker changed the lock to %q", got)
	}
}

// L1: a holder whose first word is not a kind v1 writes is `unknown`; the third try can win.
func TestAHolderOfNoKnownKindIsUnknownAndALockFreedBetweenTriesIsTaken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keeper.lock")
	free := rawHold(t, path, "stranger 1")
	_, err := (&sleeper{}).locks().Keeper(t.Context(), dir)
	if busy, ok := errors.AsType[*BusyError](err); !ok || busy.Holder != KindUnknown {
		t.Fatalf("got %v, want busy held by unknown", err)
	}
	s := &sleeper{onWait: func(n int) {
		if n == 2 {
			free()
		}
	}}
	lock, err := s.locks().Keeper(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release(t, lock)
	if lock.Previous() != "stranger 1" || readText(t, path) != "keeper" {
		t.Errorf("previous %q, file %q", lock.Previous(), readText(t, path))
	}
}

func TestTheWatchLockLivesUnderInjectByItsSessionKey(t *testing.T) {
	home := t.TempDir()
	key := "0123456789abcdef"
	lock, err := (&sleeper{}).locks().Watch(t.Context(), home, key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "inject", key+".watch")
	if readText(t, path) != "watch" || lock.Kind() != KindWatch {
		t.Errorf("file %q kind %q", readText(t, path), lock.Kind())
	}
	info, err := os.Stat(filepath.Join(home, "inject"))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("inject/ %v %v, want 0700", info, err)
	}
	if kind, held, probeErr := ProbeWatch(home, key); probeErr != nil || !held || kind != KindWatch {
		t.Errorf("probe %q %v %v", kind, held, probeErr)
	}
	release(t, lock)
	again, err := (&sleeper{}).locks().Watch(t.Context(), home, key)
	if err != nil {
		t.Fatalf("a second watch lock under an existing inject/: %v", err)
	}
	release(t, again)
}

func TestALockIsRefusedForAWrongKindClaimOrKey(t *testing.T) {
	dir := t.TempDir()
	l := (&sleeper{}).locks()
	ctx := t.Context()
	for name, err := range map[string]error{
		"seat as a sync kind": second(l.Sync(ctx, dir, KindSeat)),
		"unknown as a kind":   second(l.Sync(ctx, dir, KindUnknown)),
		"a negative claim":    second(l.Seat(ctx, dir, -1)),
		"a 17-digit claim":    second(l.Seat(ctx, dir, 1e16)),
		"a short key":         second(l.Watch(ctx, dir, "0123")),
		"an upper-case key":   second(l.Watch(ctx, dir, "0123456789ABCDEF")),
		"a key with a slash":  second(l.Watch(ctx, dir, "0123456789abc/ef")),
	} {
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, _, err := ProbeWatch(dir, "../x"); err == nil {
		t.Error("probe of a bad key accepted")
	}
}

func second[T any](_ T, err error) error { return err }

func TestALockIsNeverTakenThroughASymlinkOrOnAFIFO(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "elsewhere"), filepath.Join(dir, "lock")); err != nil {
		t.Fatal(err)
	}
	fifoDir := t.TempDir()
	for _, name := range []string{"keeper.lock", "seat.lock"} {
		if err := unix.Mkfifo(filepath.Join(fifoDir, name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	l := (&sleeper{}).locks()
	for name, err := range map[string]error{
		"sync through a symlink": second(l.Sync(t.Context(), dir, KindWait)),
		"keeper on a FIFO":       second(l.Keeper(t.Context(), fifoDir)),
		"probe through symlink":  third(ProbeSync(dir)),
		"claim on a FIFO":        third(ReadClaim(fifoDir)),
	} {
		if e, ok := errors.AsType[*Error](err); !ok || e.Class != ClassFile {
			t.Errorf("%s: %v, want a file refusal", name, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, "elsewhere")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the symlink's target was created: %v", err)
	}
}

func third[A, B any](_ A, _ B, err error) error { return err }

func TestALockInAMissingDirectoryFails(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone")
	if _, err := (&sleeper{}).locks().Sync(t.Context(), gone, KindWatch); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("got %v, want not-exist", err)
	}
}

// Context cancellation ends the retries and the waits.
func TestACancelledContextEndsTheTries(t *testing.T) {
	dir := t.TempDir()
	defer rawHold(t, filepath.Join(dir, "lock"), "recv")()
	ctx, cancel := context.WithCancel(t.Context())
	s := &sleeper{onWait: func(int) { cancel() }}
	if _, err := s.locks().Sync(ctx, dir, KindWait); !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want cancelled", err)
	}
	if s.count() != 1 {
		t.Errorf("%d waits after the cancel", s.count())
	}
	if _, err := s.locks().Sync(ctx, dir, KindWait); !errors.Is(err, context.Canceled) {
		t.Errorf("an already cancelled context: %v", err)
	}
}

// L3: absent is free and creates nothing; a held lock names its holder; a free file is free.
func TestAProbeNamesTheHolderAndCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	if kind, held, err := ProbeSync(dir); err != nil || held || kind != "" {
		t.Errorf("absent: %q %v %v", kind, held, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "lock")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a probe created the lock: %v", err)
	}
	lock, err := (&sleeper{}).locks().Sync(t.Context(), dir, KindRecv)
	if err != nil {
		t.Fatal(err)
	}
	if kind, held, probeErr := ProbeSync(dir); probeErr != nil || !held || kind != KindRecv || kind.Waker() {
		t.Errorf("held: %q %v %v", kind, held, probeErr)
	}
	release(t, lock)
	if kind, held, probeErr := ProbeSync(dir); probeErr != nil || held {
		t.Errorf("released: %q %v %v", kind, held, probeErr)
	}
}

func TestTheWakersAreWatchAndWaitAndAnAbsentLockIsFree(t *testing.T) {
	dir := t.TempDir()
	if !KindWatch.Waker() || !KindWait.Waker() || KindSeat.Waker() {
		t.Error("the wakers are watch and wait")
	}
	for name, probe := range map[string]func(string) (Kind, bool, error){"seat": ProbeSeat, "keeper": ProbeKeeper} {
		if _, held, probeErr := probe(dir); probeErr != nil || held {
			t.Errorf("%s absent: %v %v", name, held, probeErr)
		}
	}
}

// L6: the claim is read from the seat lock without taking it, held or not.
func TestAClaimIsReadAsV1WritesIt(t *testing.T) {
	dir := t.TempDir()
	if claim, ok, err := ReadClaim(dir); err != nil || ok || claim != 0 {
		t.Errorf("absent: %d %v %v", claim, ok, err)
	}
	for text, want := range map[string]int64{
		"seat 0": 0, "seat 1781000000000": 1781000000000, "seat 9999999999999999": 9999999999999999,
		"seat 012": -1, "seat 12345678901234567": -1, "seat": -1, "seat 12 ": -1, "keeper 12": -1,
		"seat -1": -1, "seat 1" + string(rune(0x0663)): -1, "": -1,
	} {
		if err := os.WriteFile(filepath.Join(dir, "seat.lock"), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		claim, ok, err := ReadClaim(dir)
		if err != nil || ok != (want >= 0) || (ok && claim != want) {
			t.Errorf("%q: %d %v %v, want %d", text, claim, ok, err, want)
		}
	}
}

// v1 reads the holder as ASCII with each other byte replaced; so does Previous.
func TestPreviousReadsBytesPastASCIIAsReplacement(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keeper.lock"), []byte("ke\xffper"), 0o600); err != nil {
		t.Fatal(err)
	}
	lock, err := (&sleeper{}).locks().Keeper(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release(t, lock)
	if lock.Previous() != "ke"+string(rune(0xfffd))+"per" {
		t.Errorf("previous %q", lock.Previous())
	}
}

// L5: the descriptor is close-on-exec; File is the route to hand it to a child.
func TestALockIsCloseOnExec(t *testing.T) {
	lock, err := (&sleeper{}).locks().Keeper(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer release(t, lock)
	flags, err := unix.FcntlInt(lock.File().Fd(), unix.F_GETFD, 0)
	if err != nil {
		t.Fatal(err)
	}
	if flags&unix.FD_CLOEXEC == 0 {
		t.Error("the lock's descriptor survives exec")
	}
}

// L7, L11: the outbox lock is flock on the state directory, held for fn, and a second
// transaction in this process waits for the first.
func TestTheOutboxLockHoldsTheStateDirectoryAndASecondWaits(t *testing.T) {
	dir := t.TempDir()
	inside := make(chan struct{})
	finish := make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- (&sleeper{}).locks().WithOutbox(t.Context(), dir, func(context.Context) error {
			close(inside)
			<-finish
			return nil
		})
	}()
	<-inside
	if !heldElsewhere(t, dir) {
		t.Error("the state directory is not locked inside the transaction")
	}
	s := &sleeper{onWait: func(n int) {
		if n == 1 {
			close(finish)
		}
	}}
	ran := false
	if err := s.locks().WithOutbox(t.Context(), dir, func(context.Context) error {
		ran = true
		return nil
	}); err != nil || !ran {
		t.Fatalf("the second transaction: %v ran %v", err, ran)
	}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if s.count() == 0 || s.waits[0] != 10*time.Millisecond {
		t.Errorf("the second never waited 10ms: %v", s.waits)
	}
	if heldElsewhere(t, dir) {
		t.Error("the state directory is still locked")
	}
}

func TestATransactionsErrorIsReturnedAndALockOnAMissingDirectoryFails(t *testing.T) {
	want := errors.New("refused inside")
	l := (&sleeper{}).locks()
	if err := l.WithOutbox(t.Context(), t.TempDir(), func(context.Context) error { return want }); !errors.Is(err, want) {
		t.Errorf("got %v", err)
	}
	gone := filepath.Join(t.TempDir(), "gone")
	never := func(context.Context) error { t.Error("ran without its lock"); return nil }
	if err := l.WithOutbox(t.Context(), gone, never); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("outbox in a missing directory: %v", err)
	}
	link := filepath.Join(t.TempDir(), "state")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	if err := l.WithOutbox(t.Context(), link, never); err == nil {
		t.Error("a symlinked state directory was locked")
	}
	if err := l.WithDelivery(t.Context(), gone, never); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("delivery under a missing home: %v", err)
	}
}

// L8: the delivery lock makes relay/ 0700 and locks relay/lock, writing nothing into it.
func TestTheDeliveryLockIsRelayLockAndTryIsRefusedWhenHeld(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "relay", "lock")
	l := (&sleeper{}).locks()
	// elsewhere is a context that holds nothing, as another goroutine's would: a try under it
	// is refused by the lock, not by the order.
	elsewhere := t.Context()
	err := l.WithDelivery(t.Context(), home, func(context.Context) error {
		if !heldElsewhere(t, path) {
			t.Error("relay/lock is not held")
		}
		tryErr := l.TryDelivery(elsewhere, home, func(context.Context) error {
			t.Error("a second delivery ran")
			return nil
		})
		if busy, ok := errors.AsType[*BusyError](tryErr); !ok || busy.Path != path {
			t.Errorf("try while held: %v", tryErr)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(home, "relay"))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("relay/ %v %v", info, err)
	}
	if readText(t, path) != "" {
		t.Errorf("relay/lock says %q", readText(t, path))
	}
	ran := false
	if err = l.TryDelivery(t.Context(), home, func(context.Context) error { ran = true; return nil }); err != nil || !ran {
		t.Errorf("try when free: %v %v", err, ran)
	}
}

// A cancelled context runs nothing, even when the lock is free.
func TestACancelledContextTakesNoBlockingLock(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	l := (&sleeper{}).locks()
	never := func(context.Context) error { t.Error("ran under a cancelled context"); return errors.New("ran") }
	home := t.TempDir()
	for name, err := range map[string]error{
		"outbox":   l.WithOutbox(ctx, t.TempDir(), never),
		"delivery": l.WithDelivery(ctx, home, never),
		"try":      l.TryDelivery(ctx, home, never),
	} {
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(home, "relay")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a cancelled delivery made relay/: %v", err)
	}
}

// A panic in fn still lets the lock go, as v1's finally does.
func TestAPanicInsideALockReleasesIt(t *testing.T) {
	stateDir, home := t.TempDir(), t.TempDir()
	mustPanic := func(name string, run func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s: no panic", name)
			}
		}()
		run()
	}
	boom := func(context.Context) error { panic("boom") }
	mustPanic("outbox", func() { t.Error((&sleeper{}).locks().WithOutbox(t.Context(), stateDir, boom)) })
	mustPanic("told", func() { t.Error(WithTold(home, func() error { panic("boom") })) })
	if heldElsewhere(t, stateDir) || heldElsewhere(t, filepath.Join(home, "told.lock")) {
		t.Error("a panic left a lock held")
	}
}

func TestADeliveryWaitEndsWithItsContext(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "relay"), 0o700); err != nil {
		t.Fatal(err)
	}
	defer rawHold(t, filepath.Join(home, "relay", "lock"), "")()
	ctx, cancel := context.WithCancel(t.Context())
	s := &sleeper{onWait: func(n int) {
		if n == 3 {
			cancel()
		}
	}}
	err := s.locks().WithDelivery(ctx, home, func(context.Context) error {
		t.Error("ran without the lock")
		return nil
	})
	if !errors.Is(err, context.Canceled) || s.count() != 3 {
		t.Errorf("got %v after %d waits", err, s.count())
	}
}

func TestADirectoryALockNeedsIsNeverASymlink(t *testing.T) {
	home := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(home, "relay")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "inject"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	l := (&sleeper{}).locks()
	err := l.WithDelivery(t.Context(), home, func(context.Context) error { return nil })
	if e, ok := errors.AsType[*Error](err); !ok || e.Class != ClassFile {
		t.Errorf("relay/ a symlink: %v", err)
	}
	_, err = l.Watch(t.Context(), home, "0123456789abcdef")
	if e, ok := errors.AsType[*Error](err); !ok || e.Class != ClassFile {
		t.Errorf("inject/ a file: %v", err)
	}
}

// L10: the delivery lock is never taken with the outbox lock held, and neither is taken twice.
func TestTheLockOrderIsDeliveryThenOutboxAndNeverNested(t *testing.T) {
	home, stateDir := t.TempDir(), t.TempDir()
	l := (&sleeper{}).locks()
	never := func(context.Context) error { t.Error("ran out of order"); return errors.New("out of order") }
	cases := map[string]func(ctx context.Context) error{
		"delivery inside outbox": func(ctx context.Context) error { return l.WithDelivery(ctx, home, never) },
		"try inside outbox":      func(ctx context.Context) error { return l.TryDelivery(ctx, home, never) },
		"outbox inside outbox":   func(ctx context.Context) error { return l.WithOutbox(ctx, stateDir, never) },
		"pair inside outbox": func(ctx context.Context) error {
			return l.WithDeliveryAndOutbox(ctx, home, stateDir, never)
		},
	}
	for name, inner := range cases {
		err := l.WithOutbox(t.Context(), stateDir, inner)
		if !errors.Is(err, ErrLockOrder) {
			t.Errorf("%s: %v, want a lock order error", name, err)
		}
	}
	err := l.WithDelivery(t.Context(), home, func(ctx context.Context) error {
		return l.WithDelivery(ctx, home, never)
	})
	if !errors.Is(err, ErrLockOrder) {
		t.Errorf("delivery inside delivery: %v", err)
	}
	err = l.WithDelivery(t.Context(), home, func(ctx context.Context) error {
		return l.WithOutbox(ctx, stateDir, func(context.Context) error { return nil })
	})
	if err != nil {
		t.Errorf("outbox inside delivery: %v", err)
	}
}

func TestThePairHoldsBothLocks(t *testing.T) {
	home, stateDir := t.TempDir(), t.TempDir()
	ran := false
	err := (&sleeper{}).locks().WithDeliveryAndOutbox(t.Context(), home, stateDir, func(context.Context) error {
		ran = true
		if !heldElsewhere(t, filepath.Join(home, "relay", "lock")) || !heldElsewhere(t, stateDir) {
			t.Error("the pair does not hold both")
		}
		return nil
	})
	if err != nil || !ran {
		t.Fatalf("%v %v", err, ran)
	}
	if heldElsewhere(t, stateDir) || heldElsewhere(t, filepath.Join(home, "relay", "lock")) {
		t.Error("the pair left a lock held")
	}
	gone := filepath.Join(t.TempDir(), "gone")
	if err = (&sleeper{}).locks().WithDeliveryAndOutbox(t.Context(), home, gone, func(context.Context) error {
		t.Error("ran without the outbox lock")
		return nil
	}); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the pair with a missing state directory: %v", err)
	}
}

// L9: one try; another teller holding it is no error, and fn does not run.
func TestTheToldLockIsOneTry(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "told.lock")
	ran, err := WithTold(home, func() error {
		if !heldElsewhere(t, path) {
			t.Error("told.lock is not held")
		}
		inner, innerErr := WithTold(home, func() error { t.Error("two tellers"); return nil })
		if inner || innerErr != nil {
			t.Errorf("a second teller: %v %v", inner, innerErr)
		}
		return nil
	})
	if !ran || err != nil {
		t.Fatalf("%v %v", ran, err)
	}
	if heldElsewhere(t, path) {
		t.Error("told.lock left held")
	}
	want := errors.New("tell failed")
	if ran, err = WithTold(home, func() error { return want }); !ran || !errors.Is(err, want) {
		t.Errorf("%v %v", ran, err)
	}
	if _, err = WithTold(filepath.Join(home, "gone"), func() error { return nil }); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a missing home: %v", err)
	}
}

// The default wait is a timer that the context ends.
func TestTheDefaultWaitIsATimerTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := (Locks{}).sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("got %v", err)
	}
	if err := (Locks{}).sleep(t.Context(), 0); err != nil {
		t.Errorf("a zero wait: %v", err)
	}
}

// A release whose descriptor is gone reports it rather than claiming the lock was let go.
func TestAReleaseOnADeadDescriptorReportsAnError(t *testing.T) {
	lock, err := (&sleeper{}).locks().Keeper(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = syscall.Close(int(lock.File().Fd())); err != nil {
		t.Fatal(err)
	}
	if err = lock.Release(); err == nil {
		t.Error("a release on a closed descriptor reported nothing")
	}
}
