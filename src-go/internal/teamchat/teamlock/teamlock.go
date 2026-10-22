// Package teamlock is a team's lock, `/run/teamchat-admin/team-<team>.lock` (Plan 00209
// SPEC-n2e.md TL1, DESIGN-v2-C-host.md 3.4): an exclusive flock on a file that is never
// unlinked, opened through no link, whose holder writes one line naming itself so a waiter that
// gives up can say who holds it.
package teamlock

import (
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// ErrBusy is a lock another process holds.
var ErrBusy = errors.New("teamlock: busy")

// BusyError names the holder of a busy lock, as its line reads.
type BusyError struct{ Holder string }

func (e *BusyError) Error() string { return "the team lock is held: " + e.Holder }

// Is makes a BusyError ErrBusy.
func (e *BusyError) Is(target error) bool { return target == ErrBusy }

// maxHolder bounds the holder line read back.
const maxHolder = 256

// unknownHolder stands for a holder line that is not one printable line.
const unknownHolder = "an unreadable holder"

var (
	verbRe   = regexp.MustCompile(`^[a-z][a-z.-]{0,31}$`)
	holderRe = regexp.MustCompile(`^[ -~]{1,256}$`)
)

// Holder is who takes the lock: the verb, the team, the process and since when.
type Holder struct {
	Verb  string
	Team  paths.TeamID
	PID   int
	Since time.Time
}

func (h Holder) line() (string, error) {
	if !verbRe.MatchString(h.Verb) || h.PID <= 0 {
		return "", errors.New("teamlock: the holder's verb or pid is not valid")
	}
	return h.Verb + " " + h.Team.String() + " pid=" + strconv.Itoa(h.PID) + " since=" + strconv.FormatInt(h.Since.UnixMilli(), 10), nil
}

// Lock is a held team lock.
type Lock struct{ f *os.File }

// TryLock takes the lock at path without waiting and writes the holder's line; a lock held by
// another is a BusyError (TL1).
func TryLock(path string, h Holder) (*Lock, error) {
	line, err := h.line()
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("cannot open the team lock %s: %w", path, err)
	}
	err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return nil, errors.Join(&BusyError{Holder: holderOf(f)}, f.Close())
	}
	if err == nil {
		err = writeHolder(f, line)
	}
	if err != nil {
		return nil, errors.Join(fmt.Errorf("cannot take the team lock %s: %w", path, err), f.Close())
	}
	return &Lock{f: f}, nil
}

func writeHolder(f *os.File, line string) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	_, err := f.WriteAt([]byte(line+"\n"), 0)
	return err
}

// holderOf is the holder's line, or unknownHolder when it is not one printable line.
func holderOf(f *os.File) string {
	data, err := io.ReadAll(io.LimitReader(f, maxHolder+2))
	line := strings.TrimSuffix(string(data), "\n")
	if err != nil || !holderRe.MatchString(line) {
		return unknownHolder
	}
	return line
}

// Unlock frees the lock; the file stays.
func (l *Lock) Unlock() error {
	return errors.Join(unix.Flock(int(l.f.Fd()), unix.LOCK_UN), l.f.Close())
}
