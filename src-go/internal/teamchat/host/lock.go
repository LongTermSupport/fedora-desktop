// Package host is `team apply`'s host steps and `team remove` of `teamchat-admin` (Plan 00209,
// DESIGN-v2-C-host.md 3.2.2-3.2.5, 3.4, 3.5.2, 3.5.3; SPEC-n2d.md section 4, HS1-HS14): the
// team lock, the refusals before any change, the in-memory render of a team's homeserver
// config, unit drop-in and `team.json` with the five-way answer, the first apply's files, the
// firewall, the backup, the unit, readiness, the bound addresses, and the records written last.
// Every host effect (systemd, firewalld, the network, the readiness probe, the backup, the
// status record, bootstrap) is a seam in Deps; the files are written under Deps.Root.
package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// PlayWait is how long a play step waits for a team lock (C 3.4).
const PlayWait = 10 * time.Minute

// lockPoll is how often a waiter tries the lock again.
const lockPoll = 250 * time.Millisecond

// holderMax caps how much of a holder's line a waiter reads.
const holderMax = 512

// Clock is the time a lock wait and the readiness wait are measured by.
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

// BusyError is a team lock another verb holds; root exits 75 on it, naming the holder.
type BusyError struct {
	Path, Holder string
}

func (e *BusyError) Error() string { return "team lock " + e.Path + " is held by: " + e.Holder }

// Lock is a held team lock.
type Lock struct {
	f    *os.File
	team paths.TeamID
}

// Team is the team the lock is held for.
func (l *Lock) Team() paths.TeamID { return l.team }

// TakeLock takes the team's lock in dir (paths.AdminRunDir on a host), waiting up to wait by
// clock, and writes the holder line `<verb> <team> pid=<n> since=<ms>` into it (C 3.4).
func TakeLock(ctx context.Context, dir string, team paths.TeamID, verb string, wait time.Duration, clock Clock) (*Lock, error) {
	if verb == "" || strings.ContainsFunc(verb, func(r rune) bool { return r <= ' ' || r > '~' }) {
		return nil, fmt.Errorf("the lock's verb %q is not one printable word", verb)
	}
	if err := lockDirectory(dir); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, filepath.Base(paths.TeamLock(team)))
	f, err := os.OpenFile(filepath.Clean(path), os.O_RDWR|os.O_CREATE|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("cannot open the team lock: %w", err)
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

// lockDirectory makes dir 0700 when it is missing, and refuses one that is a link, not a
// directory, or open to anyone but its owner.
func lockDirectory(dir string) error {
	err := os.Mkdir(dir, 0o700)
	if err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("cannot make the lock directory: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("cannot inspect the lock directory: %w", err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		return fmt.Errorf("the lock directory %s must be a directory of mode 0700, not a link", dir)
	}
	return nil
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
			return &BusyError{Path: path, Holder: readHolder(f)}
		}
		if err := clock.Sleep(ctx, lockPoll); err != nil {
			return err
		}
	}
}

// readHolder is the holder's line, or what can be said when it cannot be read.
func readHolder(f *os.File) string {
	buf := make([]byte, holderMax)
	n, err := f.ReadAt(buf, 0)
	if n == 0 && err != nil {
		return "unknown (the holder has not written its line yet)"
	}
	line := buf[:n]
	if end := bytes.IndexByte(line, '\n'); end >= 0 {
		line = line[:end]
	}
	return string(line)
}

func writeHolder(f *os.File, line string) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	_, err := f.WriteAt([]byte(line), 0)
	return err
}

// Release clears the holder line and lets the lock go; the file stays (C 3.4).
func (l *Lock) Release() error {
	return errors.Join(l.f.Truncate(0), unix.Flock(int(l.f.Fd()), unix.LOCK_UN), l.f.Close())
}
