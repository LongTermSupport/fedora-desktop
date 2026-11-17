package carry

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
)

// Stage is the import's own file effects (SI8-SI10), each a kill point of SM5: the staging
// names made and filled, the placed roots renamed into place and removed again, ctl/<team>/ and
// the status directory made, and the approval saved. Every path is a host path under the
// prefix, resolved with no link followed anywhere in it.
type Stage interface {
	// MakeHomeserverRoot makes the homeserver's state root as systemd makes it (its parent 0700,
	// itself 0755) where it is absent, and refuses one that is not the state owner's directory.
	MakeHomeserverRoot() error
	// Mkdir makes a new directory 0700, never over an entry.
	Mkdir(path string) error
	// Create makes a new file of exactly mode, never over an entry.
	Create(path string, mode fs.FileMode) (Part, error)
	// Rename moves an entry to a name no entry holds (renameat2 RENAME_NOREPLACE).
	Rename(from, to string) error
	// RemoveTree removes an entry level by level, never following a link; absent is done.
	RemoveTree(path string) error
	// SyncDir fsyncs a directory.
	SyncDir(dir string) error
	// MkdirCtl makes ctl/<team>/ 0700, the ctl account's, as a first apply does.
	MkdirCtl(team paths.TeamID) error
	// MkdirStatus makes the team's status directory as team approve does.
	MkdirStatus(team paths.TeamID) error
	// SaveApproved writes approved.json as team approve does, with the owner group's gid.
	SaveApproved(team paths.TeamID, g declare.Gated) error
}

// StageOS is Stage on the host under the prefix Root ("" on a host). StateOwnerUID owns the
// homeserver's state root (0 on a host); CtlUID and CtlGID are the ctl account's; OwnerGroup is
// the owner group's gid, read when the approval is saved.
type StageOS struct {
	Root           string
	StateOwnerUID  uint32
	CtlUID, CtlGID int
	OwnerGroup     func() (int, error)
}

var _ Stage = StageOS{}

// MakeHomeserverRoot implements Stage.
func (o StageOS) MakeHomeserverRoot() error {
	base := paths.HomeserverPrivateStateRoot
	for _, dir := range []struct {
		path string
		mode fs.FileMode
	}{{filepath.Dir(base), 0o700}, {base, 0o755}} {
		info, err := OS{Root: o.Root}.Stat(dir.path)
		switch {
		case err != nil:
			return err
		case info.Kind == restore.KindAbsent:
			err = o.mkdirAt(dir.path, dir.mode)
		case info.Kind != restore.KindDirectory || info.UID != o.StateOwnerUID:
			err = fmt.Errorf("%s is not a directory of uid %d", o.Root+dir.path, o.StateOwnerUID)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// Mkdir implements Stage.
func (o StageOS) Mkdir(path string) error { return o.mkdirAt(path, dirMode) }

// Create implements Stage.
func (o StageOS) Create(path string, mode fs.FileMode) (Part, error) {
	d, name, err := o.parent(path)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Openat(int(d.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err == nil {
		if err = unix.Fchmod(fd, uint32(mode)); err != nil {
			err = errors.Join(err, unix.Close(fd))
		}
	}
	if err = errors.Join(err, d.Close()); err != nil {
		return nil, &fs.PathError{Op: "create", Path: o.Root + path, Err: err}
	}
	return os.NewFile(uintptr(fd), o.Root+path), nil
}

// Rename implements Stage.
func (o StageOS) Rename(from, to string) error {
	fd, fname, err := o.parent(from)
	if err != nil {
		return err
	}
	td, tname, err := o.parent(to)
	if err != nil {
		return errors.Join(err, fd.Close())
	}
	err = unix.Renameat2(int(fd.Fd()), fname, int(td.Fd()), tname, unix.RENAME_NOREPLACE)
	if err != nil {
		err = &fs.PathError{Op: "rename " + o.Root + from + " to", Path: o.Root + to, Err: err}
	}
	return errors.Join(err, fd.Close(), td.Close())
}

// RemoveTree implements Stage.
func (o StageOS) RemoveTree(path string) error {
	d, name, err := o.parent(path)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.Join(restore.RemoveAt(d, name), d.Close())
}

// SyncDir implements Stage.
func (o StageOS) SyncDir(dir string) error { return OS{Root: o.Root}.SyncDir(dir) }

// MkdirCtl implements Stage.
func (o StageOS) MkdirCtl(team paths.TeamID) error {
	return host.MkdirOwned(o.Root+paths.CtlState(team), o.CtlUID, o.CtlGID)
}

// MkdirStatus implements Stage.
func (o StageOS) MkdirStatus(team paths.TeamID) error { return host.MkdirStatus(o.Root, team) }

// SaveApproved implements Stage.
func (o StageOS) SaveApproved(team paths.TeamID, g declare.Gated) error {
	if o.OwnerGroup == nil {
		return errors.New("this wiring has no owner group")
	}
	gid, err := o.OwnerGroup()
	if err != nil {
		return fmt.Errorf("the owner group: %w", err)
	}
	return declare.FileApprovals{Path: func(t paths.TeamID) string { return o.Root + paths.ApprovedFile(t) }, GID: gid}.Save(team, g)
}

// parent opens path's directory with no link followed, and answers it with path's last name.
func (o StageOS) parent(path string) (*os.File, string, error) {
	dir, name := filepath.Split(filepath.Clean(path))
	if name == "" || name == "." || name == ".." {
		return nil, "", fmt.Errorf("%q names no entry", path)
	}
	d, err := OS{Root: o.Root}.openNoLinks(filepath.Clean(dir), unix.O_RDONLY|unix.O_DIRECTORY)
	return d, name, err
}

// mkdirAt makes path, a new directory of exactly mode.
func (o StageOS) mkdirAt(path string, mode fs.FileMode) error {
	d, name, err := o.parent(path)
	if err != nil {
		return err
	}
	err = unix.Mkdirat(int(d.Fd()), name, uint32(mode))
	if err == nil {
		err = unix.Fchmodat(int(d.Fd()), name, uint32(mode), 0)
	}
	if err != nil {
		err = &fs.PathError{Op: "mkdir", Path: o.Root + path, Err: err}
	}
	return errors.Join(err, d.Close())
}
