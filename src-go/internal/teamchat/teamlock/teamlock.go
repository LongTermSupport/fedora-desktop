// Package teamlock is a team's lock, `/run/teamchat-admin/team-<team>.lock` (Plan 00209
// SPEC-n2e.md TL1, SPEC-n2d.md HS1-HS2, DESIGN-v2-C-host.md 3.4): an exclusive flock on a file
// that is never unlinked, opened through no link in a 0700 directory the taker owns, whose
// holder writes one line naming itself so a waiter that gives up can say who holds it. Every
// root verb that changes or restarts a team takes it here; each caller owns its wait.
package teamlock

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// ErrBusy is a lock another process holds.
var ErrBusy = errors.New("teamlock: busy")

// BusyError is a team lock another verb holds, naming its holder as the line reads; root exits
// 75 on it.
type BusyError struct {
	Path, Holder string
}

func (e *BusyError) Error() string { return "team lock " + e.Path + " is held by: " + e.Holder }

// Is makes a BusyError ErrBusy.
func (e *BusyError) Is(target error) bool { return target == ErrBusy }

// lockPoll is how often a waiter tries the lock again.
const lockPoll = 250 * time.Millisecond

// maxHolder bounds the holder line read back.
const maxHolder = 256

// unknownHolder stands for a holder line that is not one printable line.
const unknownHolder = "an unreadable holder"

var (
	verbRe   = regexp.MustCompile(`^[a-z][a-z.-]{0,31}$`)
	holderRe = regexp.MustCompile(`^[ -~]{1,256}$`)
)

// Clock is the time a lock wait is measured by.
type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

// SystemClock is the wall clock.
func SystemClock() Clock { return systemClock{} }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) Sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Lock is a held team lock.
type Lock struct {
	f    *os.File
	team paths.TeamID
}

// Team is the team the lock is held for.
func (l *Lock) Team() paths.TeamID { return l.team }

// Take takes the team's lock in dir (paths.AdminRunDir on a host), waiting up to wait by clock
// (0 is one try), and writes the holder line `<verb> <team> pid=<n> since=<ms>` into it.
func Take(ctx context.Context, dir string, team paths.TeamID, verb string, wait time.Duration, clock Clock) (*Lock, error) {
	return take(ctx, dir, team, verb, wait, clock, os.Geteuid())
}

func take(ctx context.Context, dir string, team paths.TeamID, verb string, wait time.Duration, clock Clock, euid int) (*Lock, error) {
	if !verbRe.MatchString(verb) {
		return nil, fmt.Errorf("the lock's verb %q is not a lower-case word of at most 32 bytes", verb)
	}
	name := filepath.Base(paths.TeamLock(team))
	path := filepath.Join(dir, name)
	f, err := openLock(dir, name, euid)
	if err != nil {
		return nil, err
	}
	if err := flockWait(ctx, f, path, wait, clock); err != nil {
		return nil, errors.Join(err, f.Close())
	}
	line := verb + " " + team.String() + " pid=" + strconv.Itoa(os.Getpid()) + " since=" + strconv.FormatInt(clock.Now().UnixMilli(), 10) + "\n"
	if err := writeHolder(f, line); err != nil {
		return nil, errors.Join(fmt.Errorf("cannot write the holder of %s: %w", path, err), f.Close())
	}
	return &Lock{f: f, team: team}, nil
}

// openLock opens the regular file name in the lock directory, resolved once through the
// directory's own descriptor, so the directory checked is the one the file is opened in.
func openLock(dir, name string, euid int) (*os.File, error) {
	dirFD, err := lockDirectory(dir, euid)
	if err != nil {
		return nil, err
	}
	fd, openErr := unix.Openat(dirFD, name, unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	closeErr := unix.Close(dirFD)
	if openErr == nil && closeErr != nil {
		closeErr = errors.Join(closeErr, unix.Close(fd))
	}
	if err = errors.Join(openErr, closeErr); err != nil {
		return nil, fmt.Errorf("cannot open the team lock %s: %w", filepath.Join(dir, name), err)
	}
	f := os.NewFile(uintptr(fd), filepath.Join(dir, name))
	info, err := f.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = errors.New("not a regular file")
	}
	if err != nil {
		return nil, errors.Join(fmt.Errorf("the team lock %s: %w", f.Name(), err), f.Close())
	}
	return f, nil
}

// lockDirectory makes dir 0700 when it is missing and opens it, refusing one that is a link,
// not a directory, open to anyone but its owner, or owned by anyone but euid: a directory
// another user made first could hold every team's lock, or swap the holder lines.
func lockDirectory(dir string, euid int) (int, error) {
	err := os.Mkdir(dir, 0o700)
	if err != nil && !errors.Is(err, fs.ErrExist) {
		return -1, fmt.Errorf("cannot make the lock directory: %w", err)
	}
	fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("the lock directory %s must be a directory, not a link: %w", dir, err)
	}
	var st unix.Stat_t
	err = unix.Fstat(fd, &st)
	switch {
	case err != nil:
		err = fmt.Errorf("cannot inspect the lock directory %s: %w", dir, err)
	case st.Mode&0o7777 != 0o700:
		err = fmt.Errorf("the lock directory %s must be of mode 0700, not %04o", dir, st.Mode&0o7777)
	case int64(st.Uid) != int64(euid):
		err = fmt.Errorf("the lock directory %s is owned by uid %d, not by uid %d", dir, st.Uid, euid)
	}
	if err != nil {
		return -1, errors.Join(err, unix.Close(fd))
	}
	return fd, nil
}

func flockWait(ctx context.Context, f *os.File, path string, wait time.Duration, clock Clock) error {
	start := clock.Now()
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			return fmt.Errorf("cannot lock %s: %w", path, err)
		}
		if clock.Now().Sub(start) >= wait {
			return &BusyError{Path: path, Holder: holderOf(f)}
		}
		if err := clock.Sleep(ctx, lockPoll); err != nil {
			return err
		}
	}
}

// holderOf is the holder's line, or unknownHolder when it is not one printable line of at most
// maxHolder bytes: the line reaches a terminal and the journal, so raw bytes are never passed on.
func holderOf(f *os.File) string {
	data, err := io.ReadAll(io.NewSectionReader(f, 0, maxHolder+2))
	line := strings.TrimSuffix(string(data), "\n")
	if err != nil || !holderRe.MatchString(line) {
		return unknownHolder
	}
	return line
}

func writeHolder(f *os.File, line string) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	_, err := f.WriteAt([]byte(line), 0)
	return err
}

// Release clears the holder line and lets the lock go; the file stays.
func (l *Lock) Release() error {
	return errors.Join(l.f.Truncate(0), unix.Flock(int(l.f.Fd()), unix.LOCK_UN), l.f.Close())
}
