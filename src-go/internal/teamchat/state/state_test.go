package state

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestWriteFileReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "roster.json")
	if err := WriteFile(target, []byte("one"), 0o600, -1); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := WriteFile(target, []byte("two"), 0o640, os.Getgid()); err != nil {
		t.Fatalf("second write: %v", err)
	}
	got, found, err := ReadFile(target, 16)
	if err != nil || !found || string(got) != "two" {
		t.Fatalf("ReadFile = %q, %v, %v; want two", got, found, err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode %v, want 0640", info.Mode().Perm())
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Gid) != os.Getgid() {
		t.Fatalf("gid not set")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory holds %d entries, want the target alone", len(entries))
	}
}

func TestWriteFileFailureLeavesTheOldContent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "rooms.json")
	if err := WriteFile(target, []byte("old"), 0o600, -1); err != nil {
		t.Fatal(err)
	}
	// A group the process may not give a file: chown fails, so the write must fail cleanly.
	if os.Getuid() == 0 {
		t.Skip("root may give any group")
	}
	if err := WriteFile(target, []byte("new"), 0o600, 0); err == nil {
		t.Fatal("a chown to a foreign group succeeded")
	}
	got, _, err := ReadFile(target, 16)
	if err != nil || string(got) != "old" {
		t.Fatalf("after a failed write: %q, %v", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("a failed write left %d entries", len(entries))
	}
}

func TestWriteFileNoDirectory(t *testing.T) {
	err := WriteFile(filepath.Join(t.TempDir(), "missing", "x.json"), []byte("x"), 0o600, -1)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want not exist", err)
	}
}

func TestWriteFileRenameFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "taken")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "inside"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(target, []byte("x"), 0o600, -1); err == nil {
		t.Fatal("a rename over a non-empty directory succeeded")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("a failed rename left %d entries", len(entries))
	}
}

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	if _, found, err := ReadFile(filepath.Join(dir, "none"), 8); found || err != nil {
		t.Fatalf("a missing file: found %v, err %v", found, err)
	}
	big := filepath.Join(dir, "big")
	if err := os.WriteFile(big, []byte(strings.Repeat("x", 9)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadFile(big, 8); err == nil || !strings.Contains(err.Error(), "over 8 bytes") {
		t.Fatalf("an oversized file: %v", err)
	}
	exact := filepath.Join(dir, "exact")
	if err := os.WriteFile(exact, []byte(strings.Repeat("x", 8)), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, found, err := ReadFile(exact, 8); err != nil || !found || len(got) != 8 {
		t.Fatalf("a file at the cap: %d bytes, %v, %v", len(got), found, err)
	}
}

func TestReadFileRefusesWhatIsNotARegularFile(t *testing.T) {
	dir := t.TempDir()
	exact := filepath.Join(dir, "exact")
	if err := os.WriteFile(exact, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(exact, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadFile(link, 8); err == nil {
		t.Fatal("a symlink was followed")
	}
	if _, _, err := ReadFile(dir, 8); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("a directory: %v", err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadFile(fifo, 8); err == nil {
		t.Fatal("a FIFO was read")
	}
}

func TestWriteFileChownFailureLeavesTheOldContent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "roster.json")
	if err := WriteFile(target, []byte("old"), 0o600, -1); err != nil {
		t.Fatal(err)
	}
	refused := errors.New("chown refused")
	saved := chown
	chown = func(*os.File, int) error { return refused }
	t.Cleanup(func() { chown = saved })
	if err := WriteFile(target, []byte("new"), 0o600, 1); !errors.Is(err, refused) {
		t.Fatalf("err = %v, want the chown failure", err)
	}
	got, _, err := ReadFile(target, 16)
	if err != nil || string(got) != "old" {
		t.Fatalf("after a failed write: %q, %v", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("a failed write left %d entries", len(entries))
	}
}

// ownedFile is a file of mode holding x, owned by this test's euid.
func ownedFile(t *testing.T, dir, name string, mode fs.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

// ownerOf is the uid that owns path.
func ownerOf(t *testing.T, path string) uint32 {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("no owner")
	}
	return st.Uid
}

// TestReadOwnedFile is ST3: ReadFile's rules, and the file must be owned by the uid given and
// writable by neither its group nor others.
func TestReadOwnedFile(t *testing.T) {
	dir := t.TempDir()
	good := ownedFile(t, dir, "good", 0o644)
	uid := ownerOf(t, good)
	if got, found, err := ReadOwnedFile(good, 8, uid); err != nil || !found || string(got) != "x" {
		t.Fatalf("an owned file: %q %v %v", got, found, err)
	}
	if _, found, err := ReadOwnedFile(filepath.Join(dir, "none"), 8, uid); found || err != nil {
		t.Fatalf("a missing file: found %v, err %v", found, err)
	}
	if _, _, err := ReadOwnedFile(good, 0, uid); err == nil || !strings.Contains(err.Error(), "over 0 bytes") {
		t.Fatalf("over the cap: %v", err)
	}
}

func TestReadOwnedFileRefusals(t *testing.T) {
	dir := t.TempDir()
	good := ownedFile(t, dir, "good", 0o644)
	uid := ownerOf(t, good)
	if _, _, err := ReadOwnedFile(good, 8, uid+1); err == nil || !strings.Contains(err.Error(), "owned by uid") {
		t.Fatalf("another owner: %v", err)
	}
	for name, mode := range map[string]fs.FileMode{"group": 0o664, "other": 0o646} {
		path := ownedFile(t, dir, name, mode)
		if _, _, err := ReadOwnedFile(path, 8, uid); err == nil || !strings.Contains(err.Error(), "writable by its group or by others") {
			t.Fatalf("%s writable: %v", name, err)
		}
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(good, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadOwnedFile(link, 8, uid); err == nil {
		t.Fatal("a link was followed")
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadOwnedFile(fifo, 8, uid); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("a FIFO: %v", err)
	}
}

// TestReadOwnedTarget is ST3 for a name the distribution ships as a link: the link is followed
// and the file it names is held to the same rules.
func TestReadOwnedTarget(t *testing.T) {
	dir := t.TempDir()
	good := ownedFile(t, dir, "good", 0o644)
	uid := ownerOf(t, good)
	bad := ownedFile(t, dir, "bad", 0o664)
	for target, ok := range map[string]bool{good: true, bad: false} {
		link := target + ".link"
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		got, found, err := ReadOwnedTarget(link, 8, uid)
		if ok != (err == nil) || ok && (!found || string(got) != "x") {
			t.Errorf("a link to %s: %q %v %v", filepath.Base(target), got, found, err)
		}
	}
	if _, found, err := ReadOwnedTarget(filepath.Join(dir, "none"), 8, uid); found || err != nil {
		t.Fatalf("a missing file: found %v, err %v", found, err)
	}
}

func TestReadFileOpenFailure(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, found, err := ReadFile(filepath.Join(file, "below"), 8); found || err == nil {
		t.Fatalf("a path below a file: found %v, err %v", found, err)
	}
}
