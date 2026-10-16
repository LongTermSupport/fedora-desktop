// Package playlock is the Go side of the one lock every play run on a host takes
// (helpers/play_lock/lock.py, Plan 00137 T1.4): the same file in the user's runtime directory,
// opened with the same refusals, the same flock(2), and the same holder line, so a Go tool and
// run.bash, the panel and the unattended cycle exclude each other. It is not a second lock.
//
// What it adds is the bounded wait: Take tries the lock, and while another run holds it reads
// that run's holder line, tells the caller, sleeps and tries again, until the bound passes or
// the context ends. The wait ends only in flock succeeding on this descriptor, never in a
// check followed by a separate take, so nothing can come between the two.
//
// A holder runs its plays inside the hold by delegating it (Delegate): the child inherits the
// descriptor and FEDORA_DESKTOP_PLAY_LOCK_FD names it, which run.bash proves with
// `lock.py held` before it treats the lock as its own instead of refusing with exit 75.
package playlock

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Name is the lock file in the runtime directory.
const Name = "fedora-desktop-plays.lock"

// FDEnv is the variable a holder sets to delegate its descriptor to a child.
const FDEnv = "FEDORA_DESKTOP_PLAY_LOCK_FD"

// ExitBusy is run.bash's exit when another run holds the lock (EX_TEMPFAIL).
const ExitBusy = 75

// childFD is the descriptor the first extra file of a child is: 0, 1 and 2 are its streams.
const childFD = 3

// Poll is how often a waiting Take tries again.
const Poll = time.Second

// noteMax bounds what is read of a holder's line, as lock.py does.
const noteMax = 4096

// stampLayout is the holder line's start time, as lock.py and run.bash write it.
const stampLayout = "2006-01-02T15:04:05Z"

// ErrNotTaken is a caller that did not get the lock: it gave up at its bound, the person
// stopped the wait, or the lock could not be used. Whoever returns it has said why.
var ErrNotTaken = errors.New("the play lock was not taken")

// ErrStopped is a wait for the lock the person stopped (a Stop waiting row, Ctrl-C), beside
// ErrNotTaken: the run is cancelled rather than failed.
var ErrStopped = errors.New("the wait for the play lock was stopped")

// RuntimeDir is the directory whose lock serialises uid's play runs, taken by uid itself: its
// XDG_RUNTIME_DIR, else /run/user/<uid>.
func RuntimeDir(getenv func(string) string, uid int) string {
	if d := getenv("XDG_RUNTIME_DIR"); d != "" {
		return d
	}
	return "/run/user/" + strconv.Itoa(uid)
}

// Holder is what a holder wrote about itself; Note is the line as written, kept whole when it
// cannot be read as pid=, what= and since=.
type Holder struct {
	PID   int
	What  string
	Since time.Time
	Note  string
}

// ParseNote reads a holder line: "pid=<pid> what=<what> since=<UTC stamp>".
func ParseNote(note string) Holder {
	note = strings.TrimSpace(note)
	h := Holder{Note: note}
	rest, ok := strings.CutPrefix(note, "pid=")
	if !ok {
		return h
	}
	pid, rest, ok := strings.Cut(rest, " what=")
	if !ok {
		return h
	}
	n, err := strconv.Atoi(pid)
	if err != nil {
		return h
	}
	h.PID = n
	what, since, stamped := strings.Cut(rest, " since=")
	h.What = what
	if !stamped {
		return h
	}
	if at, err := time.Parse(stampLayout, since); err == nil {
		h.Since = at
	}
	return h
}

// Describe is the holder for a person: what it is, its pid, since when and for how long.
func (h Holder) Describe(now time.Time) string {
	if h.What == "" {
		if h.Note != "" {
			return h.Note
		}
		return "a play run that has not said what it is"
	}
	s := h.What + " (pid " + strconv.Itoa(h.PID) + ")"
	if !h.Since.IsZero() {
		s += ", held since " + h.Since.UTC().Format("15:04:05") + " UTC, for " + Duration(now.Sub(h.Since))
	}
	return s
}

// Duration is d for a person, in whole minutes: a line that stays on the screen while the
// wait goes on is wrong by the second, never by the minute.
func Duration(d time.Duration) string {
	if d < time.Minute {
		return "less than a minute"
	}
	return plural(int(d/time.Minute), "minute")
}

// Seconds is d in whole seconds, for a wait that has just ended.
func Seconds(d time.Duration) string {
	if d >= time.Minute {
		return Duration(d)
	}
	return plural(int(max(d, 0)/time.Second), "second")
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return strconv.Itoa(n) + " " + unit + "s"
}

// TimeoutError is a wait that reached its bound with the lock still held.
type TimeoutError struct {
	Holder Holder
	Waited time.Duration
	at     time.Time
}

func (e *TimeoutError) Error() string {
	return "the play lock is still held after " + Duration(e.Waited) + ", by " + e.Holder.Describe(e.at)
}

// Waiter takes the lock for one user. Every seam is a field, so a test runs it over a scratch
// directory with a clock and a sleep of its own.
type Waiter struct {
	RuntimeDir string
	UID, GID   int
	PID        int // written as the holder: the process whose descriptor holds the lock
	Now        func() time.Time
	// Sleep waits d, or returns the context's error when it ends first.
	Sleep func(ctx context.Context, d time.Duration) error
}

// New is the Waiter of this process, for its own user.
func New(getenv func(string) string) *Waiter {
	uid := os.Getuid()
	return &Waiter{
		RuntimeDir: RuntimeDir(getenv, uid), UID: uid, GID: os.Getgid(), PID: os.Getpid(),
		Now: time.Now, Sleep: sleep,
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Lock is the held lock.
type Lock struct{ f *os.File }

// Delegate is what a child needs to run inside the hold: the descriptor, as its first extra
// file, and the variable naming it. A released lock delegates nothing.
func (l *Lock) Delegate() ([]*os.File, []string) {
	if l == nil || l.f == nil {
		return nil, nil
	}
	return []*os.File{l.f}, []string{FDEnv + "=" + strconv.Itoa(childFD)}
}

// Release lets the lock go; a second release does nothing.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	f := l.f
	l.f = nil
	if err := f.Close(); err != nil {
		return fmt.Errorf("could not release the play lock: %w", err)
	}
	return nil
}

// Take takes the lock for what, waiting while another run holds it: busy (when not nil) is
// told the holder and how long this has waited, before each wait. It gives up with a
// *TimeoutError once bound has passed, or with the context's error.
func (w *Waiter) Take(ctx context.Context, what string, bound time.Duration, busy func(Holder, time.Duration)) (*Lock, error) {
	f, err := w.open()
	if err != nil {
		return nil, err
	}
	began := w.Now()
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errors.Join(fmt.Errorf("could not take the play lock %s: %w", f.Name(), err), f.Close())
		}
		holder := readHolder(f)
		waited := w.Now().Sub(began)
		if waited >= bound {
			return nil, errors.Join(&TimeoutError{Holder: holder, Waited: waited, at: w.Now()}, f.Close())
		}
		if busy != nil {
			busy(holder, waited)
		}
		if err := w.Sleep(ctx, min(Poll, bound-waited)); err != nil {
			return nil, errors.Join(err, f.Close())
		}
	}
	lock := &Lock{f: f}
	note := "pid=" + strconv.Itoa(w.PID) + " what=" + what + " since=" + w.Now().UTC().Format(stampLayout) + "\n"
	if err := f.Truncate(0); err != nil {
		return nil, errors.Join(fmt.Errorf("could not write the holder into %s: %w", f.Name(), err), lock.Release())
	}
	if _, err := f.WriteAt([]byte(note), 0); err != nil {
		return nil, errors.Join(fmt.Errorf("could not write the holder into %s: %w", f.Name(), err), lock.Release())
	}
	return lock, nil
}

func readHolder(f *os.File) Holder {
	buf := make([]byte, noteMax)
	n, err := f.ReadAt(buf, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return Holder{Note: "a play run whose note cannot be read (" + err.Error() + ")"}
	}
	return ParseNote(string(buf[:n]))
}

// open opens (creating if needed) the lock file, refusing what lock.py refuses: a runtime
// directory that is not a real directory of the user's that only it can write, and a lock that
// is a symlink or not a regular file of the user's.
func (w *Waiter) open() (*os.File, error) {
	if err := w.checkDir(); err != nil {
		return nil, err
	}
	path := filepath.Clean(filepath.Join(w.RuntimeDir, Name))
	flags := os.O_RDWR | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	f, err := os.OpenFile(path, flags, 0)
	if errors.Is(err, os.ErrNotExist) {
		f, err = os.OpenFile(path, flags|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			f, err = os.OpenFile(path, flags, 0)
		}
	}
	if err != nil {
		if fi, lerr := os.Lstat(path); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%s is a symlink, and the play lock never follows one", path)
		}
		return nil, fmt.Errorf("%s cannot be opened: %w", path, err)
	}
	fi, err := f.Stat()
	if err != nil {
		return nil, errors.Join(fmt.Errorf("%s cannot be read: %w", path, err), f.Close())
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.Mode().IsRegular() || !ok || int(st.Uid) != w.UID {
		return nil, errors.Join(fmt.Errorf("%s is not a regular file owned by uid %d", path, w.UID), f.Close())
	}
	return f, nil
}

func (w *Waiter) checkDir() error {
	dir := w.RuntimeDir
	fi, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("the runtime directory %s does not exist, so there is nowhere to take the play lock; "+
			"it exists for any user with a login session or linger", dir)
	}
	if err != nil {
		return fmt.Errorf("the runtime directory %s cannot be read: %w", dir, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("the runtime directory %s is not a real directory", dir)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != w.UID {
		return fmt.Errorf("the runtime directory %s is owned by uid %d, not %d", dir, ownerOf(st), w.UID)
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("the runtime directory %s is writable by others", dir)
	}
	return nil
}

func ownerOf(st *syscall.Stat_t) int {
	if st == nil {
		return -1
	}
	return int(st.Uid)
}
