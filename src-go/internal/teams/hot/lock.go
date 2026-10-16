package hot

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// The two locks of SPEC-hot-control.md H20: one change at a time per session, one seat
// allocation at a time per checkout. Each is an flock taken without waiting; the holder writes
// what it is into the file, so a refused taker can say who holds it.
const (
	// changeLockFile is the session's lock, in its membership directory.
	changeLockFile = ".change.lock"
	lockFileMode   = 0o600
	lockDirMode    = 0o700
	// holderMax bounds what a refused taker reads of the holder's line.
	holderMax = 512
)

// busyError is a lock another process holds.
type busyError struct{ holder string }

func (e *busyError) Error() string { return "held by " + e.holder }

// lock is one held flock.
type lock struct{ f *os.File }

// takeLock takes the flock on path without waiting, never through a symlink, and writes holder
// into the file. A held lock is a *busyError naming what its holder wrote.
func takeLock(path, holder string) (*lock, error) {
	f, err := os.OpenFile(filepath.Clean(path), os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, lockFileMode)
	if err != nil {
		return nil, fmt.Errorf("could not open the lock %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errors.Join(fmt.Errorf("could not lock %s: %w", path, err), f.Close())
		}
		data, readErr := io.ReadAll(io.LimitReader(f, holderMax))
		who := strings.TrimSpace(string(data))
		if readErr != nil || who == "" {
			who = "another ccy-teams that has not said what it is yet"
		}
		return nil, errors.Join(&busyError{holder: who}, f.Close())
	}
	l := &lock{f: f}
	if err := f.Truncate(0); err != nil {
		return nil, errors.Join(fmt.Errorf("could not write the holder into %s: %w", path, err), l.release())
	}
	if _, err := f.WriteAt([]byte(holder+"\n"), 0); err != nil {
		return nil, errors.Join(fmt.Errorf("could not write the holder into %s: %w", path, err), l.release())
	}
	return l, nil
}

// release unlocks, then closes; a second release does nothing.
func (l *lock) release() error {
	if l == nil || l.f == nil {
		return nil
	}
	f := l.f
	l.f = nil
	unlockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	if unlockErr != nil {
		unlockErr = fmt.Errorf("could not unlock %s: %w", f.Name(), unlockErr)
	}
	return errors.Join(unlockErr, f.Close())
}

// allocName is a checkout's allocation lock's file name: the first 32 hex digits of the sha256
// of its resolved path, the same as agent-bus's seat take computes.
func allocName(resolved string) string {
	sum := sha256.Sum256([]byte(resolved))
	return hex.EncodeToString(sum[:16]) + ".lock"
}

// allocPath is the checkout's seat allocation lock. It lives in the owner's runtime directory,
// not in the checkout: every session of the checkout can write there, so a lock in it could be
// a symlink to anywhere or held for good by a container.
func allocPath(runtime, checkout string) (string, error) {
	resolved, err := filepath.EvalSymlinks(checkout)
	if err != nil {
		return "", fmt.Errorf("could not resolve the checkout %s: %w", checkout, err)
	}
	dir := filepath.Join(runtime, "ccy", "alloc")
	if err := os.MkdirAll(dir, lockDirMode); err != nil {
		return "", fmt.Errorf("could not make %s: %w", dir, err)
	}
	return filepath.Join(dir, allocName(resolved)), nil
}
