package seat

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func newDir(t *testing.T) (*dir, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "root")
	d, err := openDir(path, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.close(); err != nil {
			t.Error(err)
		}
	})
	return d, path
}

// SW1: a file is created 0600 by fd, never over an existing one; a replace renames a fresh copy
// over the old; every directory is 0700.
func TestCreateAndReplace(t *testing.T) {
	d, root := newDir(t)
	if mode(t, root).Perm() != 0o700 {
		t.Errorf("root mode %v", mode(t, root))
	}
	if err := d.mkdirAll(root + "/a/b"); err != nil {
		t.Fatal(err)
	}
	if mode(t, root+"/a/b").Perm() != 0o700 || mode(t, root+"/a").Perm() != 0o700 {
		t.Errorf("made directories are not 0700")
	}
	if err := d.create(root+"/a/b/f", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, root+"/a/b/f"); got != "one" || mode(t, root+"/a/b/f").Perm() != 0o600 {
		t.Errorf("created %q %v", got, mode(t, root+"/a/b/f"))
	}
	if err := d.create(root+"/a/b/f", []byte("two")); !errors.Is(err, os.ErrExist) {
		t.Errorf("create over an existing file: %v", err)
	}
	if err := d.replace(root+"/a/b/f", []byte("two")); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, root+"/a/b/f"); got != "two" || exists(t, root+"/a/b/f.new") {
		t.Errorf("replaced %q", got)
	}
	writeFile(t, root+"/a/b/g.new", "leftover")
	if err := d.replace(root+"/a/b/g", []byte("three")); err != nil {
		t.Fatalf("replace over a leftover .new: %v", err)
	}
	if got := readFile(t, root+"/a/b/g"); got != "three" {
		t.Errorf("replaced %q", got)
	}
	if err := d.create(t.TempDir()+"/x", nil); err == nil {
		t.Errorf("a path outside the root was written")
	}
}

// SW2: a link anywhere on a path is refused, never followed, for every operation.
func TestLinksAreNeverFollowed(t *testing.T) {
	d, root := newDir(t)
	outside := t.TempDir()
	writeFile(t, outside+"/secret", "s")
	if err := os.Symlink(outside, root+"/link"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside+"/secret", root+"/file"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.read(root+"/link/secret", 10); err == nil {
		t.Errorf("read through a linked directory")
	}
	if _, err := d.read(root+"/file", 10); err == nil {
		t.Errorf("read a linked file")
	}
	if err := d.create(root+"/link/new", nil); err == nil || exists(t, outside+"/new") {
		t.Errorf("created through a linked directory: %v", err)
	}
	if err := d.mkdirAll(root + "/link/sub"); err == nil || exists(t, outside+"/sub") {
		t.Errorf("made a directory through a link: %v", err)
	}
	if _, err := d.entries(root + "/link"); err == nil {
		t.Errorf("listed a linked directory")
	}
	if err := d.rename(root+"/file", root+"/moved", unix.RENAME_NOREPLACE); err != nil {
		t.Errorf("a link itself is renamed, not followed: %v", err)
	}
	if !exists(t, outside+"/secret") || mode(t, root+"/moved")&os.ModeSymlink == 0 {
		t.Errorf("rename followed the link")
	}
}

// SW2: the root must be a directory the caller owns, mode 0700.
func TestRootChecks(t *testing.T) {
	base := t.TempDir()
	open := base + "/open"
	if err := os.Mkdir(open, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(open, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := openDir(open, false); !errors.Is(err, ErrConfig) {
		t.Errorf("a 0755 root: %v", err)
	}
	if err := os.Symlink(base, base+"/link"); err != nil {
		t.Fatal(err)
	}
	if _, err := openDir(base+"/link", false); !errors.Is(err, ErrConfig) {
		t.Errorf("a linked root: %v", err)
	}
	if _, err := openDir(base+"/missing", false); !errors.Is(err, ErrConfig) {
		t.Errorf("a missing root not created: %v", err)
	}
	if _, err := openDir("relative", true); !errors.Is(err, ErrConfig) {
		t.Errorf("a relative root: %v", err)
	}
}

// SW3: a read takes a regular file up to its cap.
func TestReadCapsAndRefusesNonFiles(t *testing.T) {
	d, root := newDir(t)
	writeFile(t, root+"/big", strings.Repeat("a", 11))
	if _, err := d.read(root+"/big", 10); !errors.Is(err, ErrInvalid) {
		t.Errorf("an over-cap file: %v", err)
	}
	if got, err := d.read(root+"/big", 11); err != nil || len(got) != 11 {
		t.Errorf("at the cap: %d, %v", len(got), err)
	}
	if err := syscall.Mkfifo(root+"/fifo", 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := d.read(root+"/fifo", 10); err == nil {
		t.Errorf("read a FIFO")
	}
	if _, err := d.read(root+"/missing", 10); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a missing file: %v", err)
	}
	if err := d.mkdirAll(root + "/sub"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.read(root+"/sub", 10); err == nil {
		t.Errorf("read a directory")
	}
}

func TestEntriesRemoveAndExists(t *testing.T) {
	d, root := newDir(t)
	writeFile(t, root+"/x/a", "1")
	writeFile(t, root+"/x/b", "2")
	names, err := d.entries(root + "/x")
	if err != nil || strings.Join(names, ",") != "a,b" {
		t.Errorf("entries %v, %v", names, err)
	}
	if ok, err := d.exists(root + "/x/a"); err != nil || !ok {
		t.Errorf("exists %v, %v", ok, err)
	}
	if err := d.unlink(root + "/x/a"); err != nil {
		t.Fatal(err)
	}
	if err := d.unlink(root + "/x/a"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("unlink of a missing file: %v", err)
	}
	if ok, err := d.exists(root + "/x/a"); err != nil || ok {
		t.Errorf("exists after unlink %v, %v", ok, err)
	}
	if err := d.rmdir(root + "/x"); err == nil {
		t.Errorf("rmdir of a non-empty directory")
	}
	if err := d.unlink(root + "/x/b"); err != nil {
		t.Fatal(err)
	}
	if err := d.rmdir(root + "/x"); err != nil || exists(t, root+"/x") {
		t.Errorf("rmdir: %v", err)
	}
	if _, err := d.entries(root + "/x"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("entries of a missing directory: %v", err)
	}
}

// renameat2's two modes: no replace, and the exchange a placement swaps with.
func TestRenameModes(t *testing.T) {
	d, root := newDir(t)
	writeFile(t, root+"/a/f", "a")
	writeFile(t, root+"/b/f", "b")
	if err := d.rename(root+"/a", root+"/b", unix.RENAME_NOREPLACE); !errors.Is(err, os.ErrExist) {
		t.Errorf("no-replace over an existing directory: %v", err)
	}
	if err := d.rename(root+"/a", root+"/b", unix.RENAME_EXCHANGE); err != nil {
		t.Fatal(err)
	}
	if readFile(t, root+"/a/f") != "b" || readFile(t, root+"/b/f") != "a" {
		t.Errorf("exchange did not swap")
	}
	if err := d.rename(root+"/a", root+"/new/deep", unix.RENAME_NOREPLACE); err != nil {
		t.Fatalf("rename into a directory it makes: %v", err)
	}
	if readFile(t, root+"/new/deep/f") != "b" {
		t.Errorf("moved content")
	}
}

// A new directory is made 0700 and must be empty and the caller's (C 3.7.3 take step 6).
func TestMakeFreshDirectory(t *testing.T) {
	d, root := newDir(t)
	if err := d.mkdirAll(root + "/s"); err != nil {
		t.Fatal(err)
	}
	if err := d.mkdirFresh(root + "/s/team"); err != nil {
		t.Fatal(err)
	}
	if err := d.mkdirFresh(root + "/s/team"); !errors.Is(err, os.ErrExist) {
		t.Errorf("a second fresh directory: %v", err)
	}
	if err := os.Symlink(t.TempDir(), root+"/s/link"); err != nil {
		t.Fatal(err)
	}
	if err := d.mkdirFresh(root + "/s/link"); !errors.Is(err, os.ErrExist) {
		t.Errorf("a link in place of the directory: %v", err)
	}
	if err := d.checkEmptyOwned(root + "/s/team"); err != nil {
		t.Errorf("a fresh directory: %v", err)
	}
	writeFile(t, root+"/s/team/x", "")
	if err := d.checkEmptyOwned(root + "/s/team"); err == nil {
		t.Errorf("a directory with a file in it was taken as empty")
	}
	if err := os.Mkdir(root+"/s/open", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root+"/s/open", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := d.checkEmptyOwned(root + "/s/open"); err == nil {
		t.Errorf("a 0755 directory was taken")
	}
}
