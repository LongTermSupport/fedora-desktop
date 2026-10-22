package homefs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// otherUID owns the files planted as another user's.
const otherUID = 4242

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func newRoot(t *testing.T) (*Root, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "root")
	check(t, os.Mkdir(path, 0o700))
	check(t, os.Chmod(path, 0o700))
	r, err := Open(path, Private)
	check(t, err)
	t.Cleanup(func() { check(t, r.Close()) })
	return r, path
}

func withUmask(t *testing.T, mask int) {
	t.Helper()
	old := syscall.Umask(mask)
	t.Cleanup(func() { syscall.Umask(old) })
}

func mode(t *testing.T, path string) fs.FileMode {
	t.Helper()
	fi, err := os.Lstat(path)
	check(t, err)
	return fi.Mode()
}

func mkdir(t *testing.T, path string, perm fs.FileMode) {
	t.Helper()
	check(t, os.Mkdir(path, perm))
	check(t, os.Chmod(path, perm))
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	check(t, os.WriteFile(path, []byte(data), 0o600))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	check(t, err)
	return string(data)
}

func giveAway(t *testing.T, path string) {
	t.Helper()
	check(t, os.Lchown(path, otherUID, -1))
}

func inode(t *testing.T, path string) uint64 {
	t.Helper()
	var st unix.Stat_t
	check(t, unix.Lstat(path, &st))
	return st.Ino
}

func TestOpenRefusesARootThatIsNotTheOwners(t *testing.T) {
	base := t.TempDir()
	for name, perm := range map[string]fs.FileMode{"private": 0o700, "open": 0o755, "group-write": 0o775, "other-write": 0o757, "others": 0o700} {
		mkdir(t, base+"/"+name, perm)
	}
	giveAway(t, base+"/others")
	writeFile(t, base+"/file", "x")
	check(t, os.Symlink(base+"/private", base+"/link"))
	for _, c := range []struct {
		path string
		need Need
		ok   bool
	}{
		{"/private", Private, true},
		{"/private", Owned, true},
		{"/open", Owned, true},
		{"/open", Private, false},
		{"/group-write", Owned, false},
		{"/other-write", Owned, false},
		{"/others", Private, false},
		{"/others", Owned, false},
		{"/file", Owned, false},
		{"/link", Private, false},
		{"/private/../private", Private, false},
	} {
		r, err := Open(base+c.path, c.need)
		switch {
		case c.ok && err != nil:
			t.Errorf("%s need %d: %v", c.path, c.need, err)
		case c.ok:
			if r.Path() != base+c.path {
				t.Errorf("path %q", r.Path())
			}
			check(t, r.Close())
		case !errors.Is(err, ErrConfig):
			t.Errorf("%s need %d: %v, want a configuration error", c.path, c.need, err)
		}
	}
	if _, err := Open("relative/path", Private); !errors.Is(err, ErrConfig) {
		t.Errorf("relative: %v", err)
	}
	if _, err := Open(base+"/missing", Private); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing: %v", err)
	}
}

// GU1: every leaf mode is named, whatever the umask.
func TestMkdirAllMakesEachDirectory0700WhateverTheUmask(t *testing.T) {
	for _, mask := range []int{0, 0o077, 0o777} {
		r, root := newRoot(t)
		withUmask(t, mask)
		check(t, r.MkdirAll(root+"/a/b/c"))
		for _, d := range []string{"/a", "/a/b", "/a/b/c"} {
			if m := mode(t, root+d); m.Perm() != 0o700 || !m.IsDir() {
				t.Errorf("umask %o: %s is %v", mask, d, m)
			}
		}
		check(t, r.MkdirAll(root+"/a/b/c"))
		check(t, r.MkdirAll(root))
	}
}

func TestMkdirAllRefusesAnExistingDirectoryNotTheOwners0700(t *testing.T) {
	r, root := newRoot(t)
	mkdir(t, root+"/open", 0o755)
	if err := r.MkdirAll(root + "/open/x"); !errors.Is(err, ErrConfig) {
		t.Errorf("0755: %v", err)
	}
	mkdir(t, root+"/theirs", 0o700)
	giveAway(t, root+"/theirs")
	if err := r.MkdirAll(root + "/theirs"); !errors.Is(err, ErrConfig) {
		t.Errorf("another uid's: %v", err)
	}
	check(t, os.Symlink(t.TempDir(), root+"/link"))
	if err := r.MkdirAll(root + "/link/x"); !errors.Is(err, ErrConfig) {
		t.Errorf("through a link: %v", err)
	}
	writeFile(t, root+"/file", "x")
	if err := r.MkdirAll(root + "/file/x"); !errors.Is(err, ErrConfig) {
		t.Errorf("below a file: %v", err)
	}
	if err := r.MkdirAll(t.TempDir()); !errors.Is(err, ErrConfig) {
		t.Errorf("outside the root: %v", err)
	}
}

func TestMkdirMakesOneFreshDirectory(t *testing.T) {
	r, root := newRoot(t)
	withUmask(t, 0o777)
	check(t, r.Mkdir(root+"/fresh"))
	if m := mode(t, root+"/fresh"); m.Perm() != 0o700 {
		t.Errorf("mode %v", m)
	}
	if err := r.Mkdir(root + "/fresh"); !errors.Is(err, fs.ErrExist) {
		t.Errorf("again: %v", err)
	}
	if err := r.Mkdir(root); !errors.Is(err, ErrConfig) {
		t.Errorf("the root itself: %v", err)
	}
}

func TestCreateNamesTheModeAndNeverOverwrites(t *testing.T) {
	r, root := newRoot(t)
	withUmask(t, 0o777)
	check(t, r.Create(root+"/f", []byte("one"), 0o640))
	if got := readFile(t, root+"/f"); got != "one" || mode(t, root+"/f").Perm() != 0o640 {
		t.Errorf("created %q %v", got, mode(t, root+"/f"))
	}
	if err := r.Create(root+"/f", []byte("two"), 0o600); !errors.Is(err, fs.ErrExist) {
		t.Errorf("over an existing file: %v", err)
	}
	target := t.TempDir() + "/target"
	check(t, os.Symlink(target, root+"/link"))
	if err := r.Create(root+"/link", []byte("x"), 0o600); !errors.Is(err, fs.ErrExist) {
		t.Errorf("through a link: %v", err)
	}
	if _, err := os.Lstat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the link's target was written: %v", err)
	}
	if err := r.Create(t.TempDir()+"/x", nil, 0o600); !errors.Is(err, ErrConfig) {
		t.Errorf("outside the root: %v", err)
	}
	if err := r.Create(root+"/missing/x", nil, 0o600); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("in a missing directory: %v", err)
	}
}

func TestReplaceIsATempFileRenamedOverTheOld(t *testing.T) {
	r, root := newRoot(t)
	withUmask(t, 0o777)
	writeFile(t, root+"/f", "old")
	check(t, r.Replace(root+"/f", []byte("new"), 0o600))
	if got := readFile(t, root+"/f"); got != "new" || mode(t, root+"/f").Perm() != 0o600 {
		t.Errorf("replaced %q %v", got, mode(t, root+"/f"))
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Errorf("left beside it: %v %v", entries, err)
	}
	writeFile(t, root+"/.g.new", "leftover")
	check(t, r.Replace(root+"/g", []byte("fresh"), 0o644))
	if got := readFile(t, root+"/g"); got != "fresh" || mode(t, root+"/g").Perm() != 0o644 {
		t.Errorf("replaced %q %v", got, mode(t, root+"/g"))
	}
}

func TestReplaceNeverFollowsALinkOrLandsOnADirectory(t *testing.T) {
	r, root := newRoot(t)
	target := t.TempDir() + "/target"
	writeFile(t, target, "theirs")
	check(t, os.Symlink(target, root+"/link"))
	check(t, r.Replace(root+"/link", []byte("mine"), 0o600))
	if got := readFile(t, target); got != "theirs" || !mode(t, root+"/link").IsRegular() {
		t.Errorf("the link was followed: target %q, link %v", got, mode(t, root+"/link"))
	}
	mkdir(t, root+"/dir", 0o700)
	if err := r.Replace(root+"/dir", []byte("x"), 0o600); err == nil || !mode(t, root+"/dir").IsDir() {
		t.Errorf("replaced a directory: %v", err)
	}
	if _, err := os.Lstat(root + "/.dir.new"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the temp was left: %v", err)
	}
	mkdir(t, root+"/.d.new", 0o700)
	if err := r.Replace(root+"/d", []byte("x"), 0o600); err == nil {
		t.Error("replaced with a directory in the temp's place")
	}
}

func TestReadRefusesAnythingButTheOwnersRegularFileWithinItsCap(t *testing.T) {
	r, root := newRoot(t)
	writeFile(t, root+"/f", "hello")
	got, err := r.Read(root+"/f", 5)
	if err != nil || string(got.Data) != "hello" || got.Ino != inode(t, root+"/f") || got.Perm != 0o600 {
		t.Fatalf("read %+v, %v", got, err)
	}
	if _, err = r.Read(root+"/f", 4); !errors.Is(err, ErrConfig) || !strings.Contains(err.Error(), "cap") {
		t.Errorf("over its cap: %v", err)
	}
	if _, err = r.Read(root+"/missing", 4); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing: %v", err)
	}
	check(t, unix.Mkfifo(root+"/fifo", 0o600))
	if _, err = r.Read(root+"/fifo", 4); !errors.Is(err, ErrConfig) {
		t.Errorf("a FIFO: %v", err)
	}
	check(t, os.Symlink(root+"/f", root+"/link"))
	if _, err = r.Read(root+"/link", 5); !errors.Is(err, ErrConfig) {
		t.Errorf("through a link: %v", err)
	}
	writeFile(t, root+"/theirs", "x")
	giveAway(t, root+"/theirs")
	if _, err = r.Read(root+"/theirs", 5); !errors.Is(err, ErrConfig) {
		t.Errorf("another uid's: %v", err)
	}
	if _, err = r.Read(root+"/sub/../f", 5); !errors.Is(err, ErrConfig) {
		t.Errorf("an unclean path: %v", err)
	}
	mkdir(t, root+"/dir", 0o700)
	if _, err = r.Read(root+"/dir", 5); !errors.Is(err, ErrConfig) {
		t.Errorf("a directory: %v", err)
	}
}

func TestOpenLockMakesOrOpensTheOwners0600File(t *testing.T) {
	r, root := newRoot(t)
	withUmask(t, 0o777)
	f, err := r.OpenLock(root + "/seat.lock")
	check(t, err)
	check(t, f.Close())
	if m := mode(t, root+"/seat.lock"); m.Perm() != 0o600 || !m.IsRegular() {
		t.Errorf("mode %v", m)
	}
	before := inode(t, root+"/seat.lock")
	f, err = r.OpenLock(root + "/seat.lock")
	check(t, err)
	check(t, f.Close())
	if inode(t, root+"/seat.lock") != before {
		t.Error("reopened as a new file")
	}
}

func TestOpenLockRefusesAnythingButTheOwners0600RegularFile(t *testing.T) {
	r, root := newRoot(t)
	check(t, unix.Mkfifo(root+"/fifo.lock", 0o600))
	writeFile(t, root+"/seat.lock", "")
	check(t, os.Symlink(root+"/seat.lock", root+"/link.lock"))
	writeFile(t, root+"/theirs.lock", "")
	giveAway(t, root+"/theirs.lock")
	writeFile(t, root+"/open.lock", "")
	check(t, os.Chmod(root+"/open.lock", 0o644))
	for _, name := range []string{"fifo.lock", "link.lock", "theirs.lock", "open.lock"} {
		if _, err := r.OpenLock(root + "/" + name); !errors.Is(err, ErrConfig) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := r.OpenLock(root + "/missing/seat.lock"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("in a missing directory: %v", err)
	}
}

func TestSubOpensADirectoryBelowAsARootOfItsOwn(t *testing.T) {
	r, root := newRoot(t)
	check(t, r.MkdirAll(root+"/a/b"))
	sub, err := r.Sub(root+"/a/b", Private)
	check(t, err)
	t.Cleanup(func() { check(t, sub.Close()) })
	check(t, sub.Create(root+"/a/b/f", []byte("x"), 0o600))
	check(t, os.Rename(root+"/a/b", root+"/a/moved"))
	// What is done through the sub root follows the directory, not its old name.
	check(t, sub.Create(root+"/a/b/g", []byte("y"), 0o600))
	if got := readFile(t, root+"/a/moved/g"); got != "y" {
		t.Errorf("got %q", got)
	}
	if _, err = r.Sub(root+"/missing", Private); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing: %v", err)
	}
	mkdir(t, root+"/open", 0o755)
	if _, err = r.Sub(root+"/open", Private); !errors.Is(err, ErrConfig) {
		t.Errorf("0755 as private: %v", err)
	}
	open, err := r.Sub(root+"/open", Owned)
	check(t, err)
	check(t, open.Close())
	self, err := r.Sub(root, Private)
	check(t, err)
	check(t, self.Close())
}

func TestEntriesUnlinkRenameAndRemoveTree(t *testing.T) {
	r, root := newRoot(t)
	check(t, r.MkdirAll(root+"/d/inner/deeper"))
	writeFile(t, root+"/d/b", "b")
	writeFile(t, root+"/d/a", "a")
	writeFile(t, root+"/d/inner/x", "x")
	check(t, os.Symlink("/", root+"/d/link"))
	got, err := r.Entries(root + "/d")
	check(t, err)
	names := make([]string, 0, len(got))
	for _, e := range got {
		names = append(names, e.Name+":"+e.Type.String())
	}
	if strings.Join(names, " ") != "a:---------- b:---------- inner:d--------- link:L---------" {
		t.Errorf("entries %q", names)
	}
	if _, err = r.Entries(root + "/missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing: %v", err)
	}
	check(t, r.Unlink(root+"/d/link"))
	if err = r.Rename(root+"/d/a", root+"/d/b"); !errors.Is(err, fs.ErrExist) {
		t.Errorf("rename over an existing name: %v", err)
	}
	check(t, r.Rename(root+"/d/a", root+"/d/c"))
	if got := readFile(t, root+"/d/c"); got != "a" {
		t.Errorf("renamed %q", got)
	}
	check(t, r.RemoveTree(root+"/d"))
	if _, err = os.Lstat(root + "/d"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("tree left: %v", err)
	}
	if err = r.RemoveTree(root + "/d"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("again: %v", err)
	}
	if err = r.RemoveTree(root); !errors.Is(err, ErrConfig) {
		t.Errorf("the root itself: %v", err)
	}
}

func TestExists(t *testing.T) {
	r, root := newRoot(t)
	check(t, os.Symlink("/nowhere", root+"/link"))
	writeFile(t, root+"/file", "x")
	for path, want := range map[string]bool{root + "/link": true, root + "/none": false, root + "/none/deeper": false} {
		got, err := r.Exists(path)
		if err != nil || got != want {
			t.Errorf("%s: %v %v", path, got, err)
		}
	}
	if _, err := r.Exists(root + "/file/x"); !errors.Is(err, ErrConfig) {
		t.Errorf("below a file: %v", err)
	}
}
