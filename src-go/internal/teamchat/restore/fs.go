package restore

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// Kind is what an entry of the state directory is.
type Kind int

// The kinds: absent, a directory, a regular file, and anything else (a link included).
const (
	KindAbsent Kind = iota
	KindDirectory
	KindFile
	KindOther
)

func (k Kind) String() string {
	return [...]string{"absent", "directory", "file", "other"}[k]
}

// Entry is one entry of a listed directory.
type Entry struct {
	Name string
	Kind Kind
}

// FS is every file effect of the restore (SM4): in a team's homeserver state directory, by
// names relative to it, each resolved by the trust rule; and in root/<team>/, by the paths of
// paths. Every write or removal is a kill point of SM5.
type FS interface {
	Kind(team paths.TeamID, name string) (Kind, error)
	// ReadDir lists a directory; a missing one is none.
	ReadDir(team paths.TeamID, name string) ([]Entry, error)
	// Rename renames one entry of the state directory to another name in it, never over an
	// entry that exists (renameat2 RENAME_NOREPLACE).
	Rename(team paths.TeamID, from, to string) error
	// RemoveTree removes one entry of the state directory, level by level, never following a
	// link; an absent one is done.
	RemoveTree(team paths.TeamID, name string) error
	// Fsync syncs the state directory; Syncfs its filesystem.
	Fsync(team paths.TeamID) error
	Syncfs(team paths.TeamID) error
	// WriteFile replaces a root file (0600, state.WriteFile); Remove removes one, absent is
	// done.
	WriteFile(path string, data []byte) error
	Remove(path string) error
}

// OS is the FS on the host under the prefix Root ("" on a host). StateOwnerUID owns
// /var/lib/private/teamchat (0 on a host).
type OS struct {
	Root          string
	StateOwnerUID uint32
}

func kindOf(mode uint32) Kind {
	switch mode & unix.S_IFMT {
	case unix.S_IFDIR:
		return KindDirectory
	case unix.S_IFREG:
		return KindFile
	}
	return KindOther
}

// Kind implements FS.
func (o OS) Kind(team paths.TeamID, name string) (Kind, error) {
	k := KindAbsent
	err := o.inTeam(team, func(dir *os.File) error {
		f, err := openBeneath(dir, name, unix.O_PATH)
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		if err != nil {
			return err
		}
		var st unix.Stat_t
		err = unix.Fstat(int(f.Fd()), &st)
		k = kindOf(st.Mode)
		return errors.Join(err, f.Close())
	})
	return k, err
}

// ReadDir implements FS.
func (o OS) ReadDir(team paths.TeamID, name string) ([]Entry, error) {
	var out []Entry
	err := o.inTeam(team, func(dir *os.File) error {
		d, err := openBeneath(dir, name, unix.O_RDONLY|unix.O_DIRECTORY)
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		if err != nil {
			return err
		}
		entries, err := d.ReadDir(-1)
		for _, e := range entries {
			k := KindOther
			switch {
			case e.Type().IsDir():
				k = KindDirectory
			case e.Type().IsRegular():
				k = KindFile
			}
			out = append(out, Entry{Name: e.Name(), Kind: k})
		}
		return errors.Join(err, d.Close())
	})
	return out, err
}

// oneName refuses a name that is not one entry of a directory.
func oneName(name string) error {
	if name == "" || name == "." || name == ".." || strings.Contains(name, "/") {
		return fmt.Errorf("%q is not one entry's name", name)
	}
	return nil
}

// Rename implements FS.
func (o OS) Rename(team paths.TeamID, from, to string) error {
	if err := errors.Join(oneName(from), oneName(to)); err != nil {
		return err
	}
	return o.inTeam(team, func(dir *os.File) error {
		fd := int(dir.Fd())
		if err := unix.Renameat2(fd, from, fd, to, unix.RENAME_NOREPLACE); err != nil {
			return &fs.PathError{Op: "rename " + from + " to", Path: dir.Name() + "/" + to, Err: err}
		}
		return nil
	})
}

// RemoveTree implements FS.
func (o OS) RemoveTree(team paths.TeamID, name string) error {
	if err := oneName(name); err != nil {
		return err
	}
	return o.inTeam(team, func(dir *os.File) error { return removeAt(dir, name) })
}

// removeAt removes name in dir: a directory by its entries first, opened no-follow beneath dir;
// anything else, a link included, by unlinkat.
func removeAt(dir *os.File, name string) error {
	var st unix.Stat_t
	err := unix.Fstatat(int(dir.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW)
	switch {
	case errors.Is(err, unix.ENOENT):
		return nil
	case err != nil:
		return fmt.Errorf("cannot stat %s/%s: %w", dir.Name(), name, err)
	case kindOf(st.Mode) != KindDirectory:
		return unlinkAt(dir, name, 0)
	}
	sub, err := openBeneath(dir, name, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return err
	}
	names, err := sub.Readdirnames(-1)
	for _, n := range names {
		if err == nil {
			err = removeAt(sub, n)
		}
	}
	if err = errors.Join(err, sub.Close()); err != nil {
		return err
	}
	return unlinkAt(dir, name, unix.AT_REMOVEDIR)
}

func unlinkAt(dir *os.File, name string, flags int) error {
	if err := unix.Unlinkat(int(dir.Fd()), name, flags); err != nil && !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("cannot remove %s/%s: %w", dir.Name(), name, err)
	}
	return nil
}

// Fsync implements FS.
func (o OS) Fsync(team paths.TeamID) error {
	return o.inTeam(team, func(dir *os.File) error { return dir.Sync() })
}

// Syncfs implements FS.
func (o OS) Syncfs(team paths.TeamID) error {
	return o.inTeam(team, func(dir *os.File) error { return unix.Syncfs(int(dir.Fd())) })
}

// WriteFile implements FS.
func (o OS) WriteFile(path string, data []byte) error {
	return state.WriteFile(o.Root+path, data, 0o600, -1)
}

// Remove implements FS: it unlinks a file and never removes a directory.
func (o OS) Remove(path string) error {
	if err := unix.Unlink(o.Root + path); err != nil && !errors.Is(err, unix.ENOENT) {
		return &fs.PathError{Op: "remove", Path: o.Root + path, Err: err}
	}
	return nil
}

// beneath is the trust rule's resolution in the homeserver's tree.
const beneath = unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_BENEATH | unix.RESOLVE_NO_XDEV

// teamDir is the team's state directory, opened from a fstat-checked fd of the state root.
func (o OS) teamDir(team paths.TeamID) (*os.File, error) {
	base := o.Root + paths.HomeserverPrivateStateRoot
	fd, err := unix.Open(base, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", base, err)
	}
	root := os.NewFile(uintptr(fd), base)
	var st unix.Stat_t
	if serr := unix.Fstat(fd, &st); serr != nil || st.Uid != o.StateOwnerUID {
		return nil, errors.Join(fmt.Errorf("%s is not owned by uid %d", base, o.StateOwnerUID), serr, root.Close())
	}
	dir, err := openBeneath(root, team.String(), unix.O_RDONLY|unix.O_DIRECTORY)
	return dir, errors.Join(err, root.Close())
}

func openBeneath(dir *os.File, name string, flags uint64) (*os.File, error) {
	if name == "" || strings.HasPrefix(name, "/") || filepath.Clean(name) != name {
		return nil, fmt.Errorf("%q is not a clean relative name", name)
	}
	fd, err := unix.Openat2(int(dir.Fd()), name, &unix.OpenHow{Flags: flags | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: beneath})
	if err != nil {
		return nil, fmt.Errorf("cannot open %s beneath %s: %w", name, dir.Name(), err)
	}
	return os.NewFile(uintptr(fd), dir.Name()+"/"+name), nil
}

// inTeam runs fn on the team's state directory.
func (o OS) inTeam(team paths.TeamID, fn func(dir *os.File) error) error {
	dir, err := o.teamDir(team)
	if err != nil {
		return err
	}
	return errors.Join(fn(dir), dir.Close())
}
