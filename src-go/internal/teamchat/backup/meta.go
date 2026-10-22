package backup

import (
	"bytes"
	"context"
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

// Deps is every effect of a backup (SM1): the host prefix ("" on a host), the homeserver
// unit's state and its signal, the readiness probe, the clock, and the owners the trust rule
// checks: the teamchat-ctl account's for `ctl/<team>/`, and StateOwnerUID for the homeserver
// tree's root (0 on a host). Root's own files must be the running euid's.
type Deps struct {
	Root          string
	Units         Units
	Ready         Ready
	Clock         Clock
	CtlUID        uint32
	StateOwnerUID uint32
}

// UnitState is what a backup reads of the homeserver unit (SD10).
type UnitState struct {
	ActiveState  string
	InvocationID string
	NRestarts    int64
	Result       string
}

// Units is the homeserver unit's state, and a signal sent to one of its processes by D-Bus
// KillUnit (whom "main", DC17).
type Units interface {
	State(ctx context.Context, unit string) (UnitState, error)
	Kill(ctx context.Context, unit, whom string, signal int32) error
}

// Ready is the probe of a homeserver's `/_tuwunel/server_version` on loopback (H 5, H-C4).
type Ready interface {
	ServerVersion(ctx context.Context, port int) error
}

// Clock is the time of a run's record and of the wait for Tuwunel's backup.
type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

// maxMeta caps a meta file read (BT2).
const maxMeta = 64 << 10

var (
	decimalRe  = regexp.MustCompile(`^[1-9][0-9]{0,17}$`)
	unixTimeRe = regexp.MustCompile(`^[1-9][0-9]{0,11}$`)
)

// resolveBeneath is the trust rule's resolution in the homeserver tree: no link, nothing above
// the descriptor, no other mount.
const resolveBeneath = unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_BENEATH | unix.RESOLVE_NO_XDEV

// scanMeta is Tuwunel's backup numbers of team, each with the unix time its meta file's first
// line gives (0 when that line is not a whole unix time yet). Only regular files with decimal
// names count, so a temporary name, a link or a FIFO is skipped, and none is opened; a missing
// directory is none (BT2, n2g-D2).
func scanMeta(d Deps, team paths.TeamID) (map[int64]int64, error) {
	dirFD, found, err := openMetaDir(d, team)
	if err != nil || !found {
		return map[int64]int64{}, err
	}
	dir := os.NewFile(uintptr(dirFD), paths.HomeserverPrivateBackupMeta(team))
	got, err := readMetaDir(dir)
	return got, errors.Join(err, dir.Close())
}

func readMetaDir(dir *os.File) (map[int64]int64, error) {
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, fmt.Errorf("cannot list %s: %w", dir.Name(), err)
	}
	got := map[int64]int64{}
	for _, e := range entries {
		if !decimalRe.MatchString(e.Name()) {
			continue
		}
		var st unix.Stat_t
		if err := unix.Fstatat(int(dir.Fd()), e.Name(), &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return nil, fmt.Errorf("cannot inspect %s/%s: %w", dir.Name(), e.Name(), err)
		}
		if st.Mode&unix.S_IFMT != unix.S_IFREG {
			continue
		}
		n, err := strconv.ParseInt(e.Name(), 10, 64)
		if err != nil {
			return nil, err
		}
		if got[n], err = metaTime(dir, e.Name()); err != nil {
			return nil, err
		}
	}
	return got, nil
}

// metaTime is the unix time on a meta file's first line, 0 when there is no whole one.
func metaTime(dir *os.File, name string) (int64, error) {
	fd, err := unix.Openat2(int(dir.Fd()), name, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC,
		Resolve: resolveBeneath,
	})
	if err != nil {
		return 0, fmt.Errorf("cannot open %s/%s: %w", dir.Name(), name, err)
	}
	f := os.NewFile(uintptr(fd), dir.Name()+"/"+name)
	data, err := readCapped(f, maxMeta)
	if err = errors.Join(err, f.Close()); err != nil {
		return 0, err
	}
	line, _, whole := bytes.Cut(data, []byte("\n"))
	if !whole || !unixTimeRe.Match(line) {
		return 0, nil
	}
	return strconv.ParseInt(string(line), 10, 64)
}

// readCapped is an open regular file's content, refused over limit bytes.
func readCapped(f *os.File, limit int64) ([]byte, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("cannot stat %s: %w", f.Name(), err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", f.Name())
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", f.Name(), err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is over %d bytes", f.Name(), limit)
	}
	return data, nil
}

// openMetaDir opens team's meta directory beneath the homeserver tree's root descriptor;
// found is false when it, or a directory on the way, does not exist.
func openMetaDir(d Deps, team paths.TeamID) (int, bool, error) {
	rootFD, err := openStateRoot(d)
	if err != nil {
		return -1, false, err
	}
	rel := strings.TrimPrefix(paths.HomeserverPrivateBackupMeta(team), paths.HomeserverPrivateStateRoot+"/")
	fd, err := unix.Openat2(rootFD, rel, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC,
		Resolve: resolveBeneath,
	})
	closeErr := unix.Close(rootFD)
	switch {
	case errors.Is(err, unix.ENOENT):
		return -1, false, closeErr
	case err != nil:
		return -1, false, errors.Join(fmt.Errorf("cannot open %s by the trust rule: %w", paths.HomeserverPrivateBackupMeta(team), err), closeErr)
	case closeErr != nil:
		return -1, false, errors.Join(closeErr, unix.Close(fd))
	}
	return fd, true, nil
}

// openStateRoot opens the homeserver tree's root, a directory owned by StateOwnerUID reached
// through no link.
func openStateRoot(d Deps) (int, error) {
	path := d.Root + paths.HomeserverPrivateStateRoot
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("cannot open %s as a directory, not a link: %w", path, err)
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return -1, errors.Join(fmt.Errorf("cannot inspect %s: %w", path, err), unix.Close(fd))
	}
	if st.Uid != d.StateOwnerUID {
		return -1, errors.Join(fmt.Errorf("%s is owned by uid %d, not %d", path, st.Uid, d.StateOwnerUID), unix.Close(fd))
	}
	return fd, nil
}
