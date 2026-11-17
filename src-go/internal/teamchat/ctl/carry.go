package ctl

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/admin"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// Placer is carry.place's writer over ctl/<team>/ (SC-C2): the directory opened by OP1's rule,
// every target checked absent before anything is written, then each file written by OP4's
// create, the guest directory made 0700 for the guest tokens.
func (cfg Config) Placer() admin.Placer {
	cfg = cfg.withDefaults()
	return func(team paths.TeamID, files []admin.PlacedFile) ([]string, error) {
		return place(cfg, team, files)
	}
}

// placedName is one file's place: in ctl/<team>/ itself, or in the guest directory.
type placedName struct {
	guest bool
	name  string
}

func place(cfg Config, team paths.TeamID, files []admin.PlacedFile) (_ []string, err error) {
	guestDir := filepath.Base(paths.GuestTokenDir(team))
	names, err := placedNames(files, guestDir)
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
	guestsDone := false
	for i, n := range names {
		switch {
		case !n.guest:
			err = d.create(n.name, files[i].Data, cfg.Rand)
		case !guestsDone:
			guestsDone, err = true, placeGuests(cfg, d, guestDir, names, files)
		}
		if err != nil {
			return nil, err
		}
	}
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Name)
	}
	return out, nil
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

// placedNames reads each file's name: a plain name, or `guest/<name>`, each once.
func placedNames(files []admin.PlacedFile, guestDir string) ([]placedName, error) {
	out := make([]placedName, 0, len(files))
	for _, f := range files {
		n := placedName{name: f.Name}
		if rest, ok := strings.CutPrefix(f.Name, guestDir+"/"); ok {
			n = placedName{guest: true, name: rest}
		}
		if !plainName(n.name) || !n.guest && n.name == guestDir {
			return nil, fmt.Errorf("%q is not a file of the team's control directory", f.Name)
		}
		if slices.Contains(out, n) {
			return nil, fmt.Errorf("%s is placed twice", f.Name)
		}
		out = append(out, n)
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
