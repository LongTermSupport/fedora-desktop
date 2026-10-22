package restore

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// osTree is an OS FS over a temp root with ops's state directory and admin directory made.
func osTree(t *testing.T) (OS, string) {
	t.Helper()
	root := t.TempDir()
	ops := opsTeam(t)
	for _, d := range []string{paths.HomeserverPrivateState(ops), paths.AdminState(ops), paths.CtlState(ops)} {
		if err := os.MkdirAll(root+d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return OS{Root: root, StateOwnerUID: myUID(t)}, root + paths.HomeserverPrivateState(ops)
}

// myUID is the test's own uid, which owns every temp tree.
func myUID(t *testing.T) uint32 {
	t.Helper()
	uid, err := strconv.ParseUint(strconv.Itoa(os.Getuid()), 10, 31)
	if err != nil {
		t.Fatal(err)
	}
	return uint32(uid)
}

func mkdirs(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// exists is whether anything is at path, a link included.
func exists(t *testing.T, path string) bool {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return info != nil
}

// Kind names what an entry is, a link included, resolved beneath the state directory.
func TestOSKind(t *testing.T) {
	o, state := osTree(t)
	ops := opsTeam(t)
	mkdirs(t, state+"/db", state+"/backups/meta")
	writeFile(t, state+"/backups/meta/3", "1\n")
	if err := os.Symlink(state+"/db", state+"/db.previous"); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]Kind{
		"db": KindDirectory, "db.previous": KindOther, "db.restore-7": KindAbsent, "backups/meta/3": KindFile,
		"backups/meta/9": KindAbsent,
	} {
		got, err := o.Kind(ops, name)
		if err != nil || got != want {
			t.Errorf("Kind(%s) = %v, %v; want %v", name, got, err, want)
		}
	}
	for _, name := range []string{"../ops", "/etc", "db.previous/x", ""} {
		if k, err := o.Kind(ops, name); err == nil {
			t.Errorf("Kind(%q) resolved to %v", name, k)
		}
	}
}

// The trust rule: a state root of another owner, or one that is a link, is refused, and so is
// a team directory that is a link.
func TestOSRefusesAStateRootItDoesNotTrust(t *testing.T) {
	o, state := osTree(t)
	ops := opsTeam(t)
	o.StateOwnerUID = myUID(t) + 1
	if k, err := o.Kind(ops, "db"); err == nil {
		t.Errorf("a state root of another owner was trusted: %v", k)
	}
	o.StateOwnerUID = myUID(t)
	base := filepath.Dir(state)
	if err := os.Rename(base, base+".real"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(base+".real", base); err != nil {
		t.Fatal(err)
	}
	if k, err := o.Kind(ops, "db"); err == nil {
		t.Errorf("a state root that is a link was trusted: %v", k)
	}
	if err := os.Remove(base); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/", base+".real/ops2"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(base+".real", base); err != nil {
		t.Fatal(err)
	}
	if k, err := o.Kind(team(t, "ops2"), "db"); err == nil {
		t.Errorf("a team directory that is a link was followed: %v", k)
	}
}

func TestOSReadDir(t *testing.T) {
	o, state := osTree(t)
	ops := opsTeam(t)
	mkdirs(t, state+"/db", state+"/backups/meta")
	writeFile(t, state+"/backups/meta/2", "1\n")
	if err := os.Symlink("/", state+"/backups/meta/5"); err != nil {
		t.Fatal(err)
	}
	got, err := o.ReadDir(ops, "backups/meta")
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{{Name: "2", Kind: KindFile}, {Name: "5", Kind: KindOther}}
	if !slices.Equal(got, want) {
		t.Errorf("ReadDir = %v, want %v", got, want)
	}
	if top, err := o.ReadDir(ops, "."); err != nil || len(top) != 2 {
		t.Errorf("ReadDir(.) = %v, %v", top, err)
	}
	if missing, err := o.ReadDir(ops, "backups/none"); err != nil || missing != nil {
		t.Errorf("a missing directory = %v, %v; want none", missing, err)
	}
	if file, err := o.ReadDir(ops, "backups/meta/2"); err == nil {
		t.Errorf("a file listed as a directory: %v", file)
	}
}

// RS16's rename: in the state directory's fd, never over an entry that exists.
func TestOSRename(t *testing.T) {
	o, state := osTree(t)
	ops := opsTeam(t)
	mkdirs(t, state+"/db", state+"/db.restore-7")
	if err := o.Rename(ops, "db", "db.restore-7"); !errors.Is(err, fs.ErrExist) {
		t.Errorf("a rename over an entry = %v, want exists", err)
	}
	if err := o.Rename(ops, "db", "db.previous"); err != nil {
		t.Fatal(err)
	}
	if k, err := o.Kind(ops, "db.previous"); err != nil || k != KindDirectory {
		t.Errorf("after the rename: %v, %v", k, err)
	}
	for _, c := range [][2]string{{"backups/x", "y"}, {"db.previous", "../x"}, {"", "x"}, {"..", "x"}} {
		if err := o.Rename(ops, c[0], c[1]); err == nil {
			t.Errorf("Rename(%q, %q) accepted", c[0], c[1])
		}
	}
	if err := o.Fsync(ops); err != nil {
		t.Error(err)
	}
	if err := o.Syncfs(ops); err != nil {
		t.Error(err)
	}
}

// The trust rule's RemoveTree: level by level, a link removed and never followed, absent is
// done.
func TestOSRemoveTreeKeepsWhatALinkNames(t *testing.T) {
	o, state := osTree(t)
	ops := opsTeam(t)
	outside := t.TempDir()
	writeFile(t, outside+"/keep", "x")
	writeFile(t, state+"/db.previous/a/b/file", "x")
	if err := os.Symlink(outside, state+"/db.previous/a/link"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, state+"/db.restore-7"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"db.previous", "db.restore-7", "db.restore-9"} {
		if err := o.RemoveTree(ops, name); err != nil {
			t.Fatalf("RemoveTree(%s): %v", name, err)
		}
		if exists(t, state+"/"+name) {
			t.Errorf("%s is still there", name)
		}
	}
	if !exists(t, outside+"/keep") {
		t.Error("the directory a link named lost its file")
	}
	if err := o.RemoveTree(ops, "a/b"); err == nil {
		t.Error("RemoveTree of a path accepted")
	}
}

// The admin directory's writes: 0600, through the atomic writer; Remove counts absent as done.
func TestOSWriteAndRemove(t *testing.T) {
	o, state := osTree(t)
	ops := opsTeam(t)
	if !exists(t, state) {
		t.Fatal("no state directory")
	}
	if err := o.WriteFile(paths.RestoreBlock(ops), []byte("x")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(o.Root + paths.RestoreBlock(ops))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the block: %v, %v", info, err)
	}
	for range 2 {
		if err := o.Remove(paths.RestoreBlock(ops)); err != nil {
			t.Fatal(err)
		}
	}
	if err := o.Remove(paths.AdminState(ops)); err == nil {
		t.Error("removing a directory that is not empty succeeded")
	}
}
