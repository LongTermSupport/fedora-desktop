package ctl

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/admin"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/carry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// Placer is carry.place's writer over ctl/<team>/ (SC-C2): the directory opened by OP1's rule,
// every target checked absent before anything is written; then each lost export streamed into
// a synced temporary beside its name and verified, and the stream's end seen, before anything
// is placed (sc-D11); then each file written by OP4's create, the guest directory made 0700 for
// the guest tokens, and each lost export linked into place. A failure removes the temporaries.
func (cfg Config) Placer() admin.Placer {
	cfg = cfg.withDefaults()
	return func(team paths.TeamID, files []admin.PlacedFile, lost *admin.LostStream) ([]string, error) {
		return place(cfg, team, files, lost)
	}
}

// placedName is one file's place: in ctl/<team>/ itself, or in the guest directory.
type placedName struct {
	guest bool
	name  string
}

func place(cfg Config, team paths.TeamID, files []admin.PlacedFile, lost *admin.LostStream) (_ []string, err error) {
	guestDir := filepath.Base(paths.GuestTokenDir(team))
	names, err := placedNames(files, lost.Files, guestDir)
	if err != nil {
		return nil, err
	}
	d, err := openDir(cfg.Root + paths.CtlState(team))
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, d.close()) }()
	if err = d.sweep(topNames(names)...); err != nil {
		return nil, err
	}
	if err = d.absent(names, guestDir); err != nil {
		return nil, err
	}
	temps, err := d.receive(lost, cfg.Rand)
	if err != nil {
		return nil, err
	}
	err = placeFiles(cfg, d, guestDir, names[:len(files)], files)
	if err == nil {
		err = d.linkLost(lost.Files, temps)
	}
	if err != nil {
		return nil, errors.Join(err, d.removeAll(temps))
	}
	out := make([]string, 0, len(names))
	for _, f := range files {
		out = append(out, f.Name)
	}
	for _, l := range lost.Files {
		out = append(out, l.Name)
	}
	return out, nil
}

// placeFiles writes each file by OP4's create, the guest tokens at the place of the first.
func placeFiles(cfg Config, d *dir, guestDir string, names []placedName, files []admin.PlacedFile) error {
	guestsDone := false
	for i, n := range names {
		var err error
		switch {
		case !n.guest:
			err = d.create(n.name, files[i].Data, cfg.Rand)
		case !guestsDone:
			guestsDone, err = true, placeGuests(cfg, d, guestDir, names, files)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// receive streams each lost export into a fresh temporary beside its name, synced, then sees
// the stream end (sc-D11): the temporaries' names when every export verified; on any failure
// none is left.
func (d *dir) receive(lost *admin.LostStream, rnd io.Reader) ([]string, error) {
	temps := make([]string, 0, len(lost.Files))
	for i, l := range lost.Files {
		temp, err := tempName(l.Name, rnd)
		if err == nil {
			temps = append(temps, temp)
			err = d.receiveOne(lost, i, temp)
		}
		if err != nil {
			return nil, errors.Join(fmt.Errorf("%s: %w", d.at(l.Name), err), d.removeAll(temps))
		}
	}
	if err := lost.End(); err != nil {
		return nil, errors.Join(err, d.removeAll(temps))
	}
	return temps, nil
}

// receiveOne is lost export i streamed into temp, made 0600 and never over a file, and synced.
func (d *dir) receiveOne(lost *admin.LostStream, i int, temp string) error {
	fd, err := unix.Openat(d.fd, temp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, fileMode)
	if err != nil {
		return fmt.Errorf("cannot create a temporary file: %w", err)
	}
	f := os.NewFile(uintptr(fd), d.at(temp))
	err = lost.Copy(f, i)
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}

// linkLost links each verified temporary to its lost export's name, never over a file (OP4),
// removes the temporaries and syncs the directory.
func (d *dir) linkLost(lost []carry.LostExport, temps []string) error {
	for i, l := range lost {
		if err := unix.Linkat(d.fd, temps[i], d.fd, l.Name, 0); err != nil {
			return fmt.Errorf("cannot put %s in place: %w", d.at(l.Name), err)
		}
	}
	return d.removeAll(temps)
}

// removeAll removes each of names that is there, then syncs the directory.
func (d *dir) removeAll(names []string) error {
	var errs []error
	for _, name := range names {
		if err := unix.Unlinkat(d.fd, name, 0); err != nil && !errors.Is(err, unix.ENOENT) {
			errs = append(errs, fmt.Errorf("cannot delete %s: %w", d.at(name), err))
		}
	}
	return errors.Join(append(errs, d.sync())...)
}

// placeGuests makes the guest directory, 0700, and writes every guest token into it, at the
// place of the first one in the list.
func placeGuests(cfg Config, d *dir, guestDir string, names []placedName, files []admin.PlacedFile) (err error) {
	g, err := d.mkdirOwn(guestDir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, g.close()) }()
	for i, n := range names {
		if !n.guest {
			continue
		}
		if err = g.create(n.name, files[i].Data, cfg.Rand); err != nil {
			return err
		}
	}
	return nil
}

// placedNames reads each file's name, a plain name or `guest/<name>`, then each lost export's,
// of its own grammar (sc-D11); each once.
func placedNames(files []admin.PlacedFile, lost []carry.LostExport, guestDir string) ([]placedName, error) {
	out := make([]placedName, 0, len(files)+len(lost))
	add := func(n placedName, given string) error {
		if slices.Contains(out, n) {
			return fmt.Errorf("%s is placed twice", given)
		}
		out = append(out, n)
		return nil
	}
	for _, f := range files {
		n := placedName{name: f.Name}
		if rest, ok := strings.CutPrefix(f.Name, guestDir+"/"); ok {
			n = placedName{guest: true, name: rest}
		}
		if !plainName(n.name) || !n.guest && n.name == guestDir {
			return nil, fmt.Errorf("%q is not a file of the team's control directory", f.Name)
		}
		if err := add(n, f.Name); err != nil {
			return nil, err
		}
	}
	for _, l := range lost {
		if err := l.Check(); err != nil {
			return nil, err
		}
		if err := add(placedName{name: l.Name}, l.Name); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// plainName is one path element that is not `.` or `..`.
func plainName(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsRune(s, '/') && !strings.ContainsRune(s, 0)
}

// topNames are the names created in ctl/<team>/ itself, whose temporaries a sweep removes.
func topNames(names []placedName) []string {
	var out []string
	for _, n := range names {
		if !n.guest {
			out = append(out, n.name)
		}
	}
	return out
}

// absent refuses when any target is already there: a top-level file, or the guest directory
// when a guest token is placed (SC-C2).
func (d *dir) absent(names []placedName, guestDir string) error {
	for _, n := range names {
		name := n.name
		if n.guest {
			name = guestDir
		}
		err := d.lstat(name)
		if err == nil {
			return fmt.Errorf("%s is already there: a carried team's files are placed only into an empty control directory", d.at(name))
		}
		if !errors.Is(err, unix.ENOENT) {
			return fmt.Errorf("%s: %w", d.at(name), err)
		}
	}
	return nil
}

// mkdirOwn makes name, 0700, which must not be there, syncs d, and opens it by OP1's rule.
func (d *dir) mkdirOwn(name string) (*dir, error) {
	if err := unix.Mkdirat(d.fd, name, dirMode); err != nil {
		return nil, fmt.Errorf("cannot make %s: %w", d.at(name), err)
	}
	if err := d.sync(); err != nil {
		return nil, err
	}
	return openDirAt(d.fd, name, d.at(name))
}
