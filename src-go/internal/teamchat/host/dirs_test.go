package host

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// strictUmask runs the test under umask 077, the strictest a root shell commonly has, so a
// mode left to the umask shows. Tests of this package never run in parallel.
func strictUmask(t *testing.T) {
	t.Helper()
	old := unix.Umask(0o077)
	t.Cleanup(func() { unix.Umask(old) })
}

// HS6: the directories root makes have their modes whatever the umask.
func TestDirectoryModesIgnoreTheUmask(t *testing.T) {
	strictUmask(t)
	root := t.TempDir()
	shared, owned := filepath.Join(root, "shared"), filepath.Join(root, "owned")
	if err := MkdirShared(shared); err != nil {
		t.Fatal(err)
	}
	if err := mkdirOwned(owned, -1, -1); err != nil {
		t.Fatal(err)
	}
	if got := mode(t, shared); got != 0o755 {
		t.Errorf("shared mode %v", got)
	}
	if got := mode(t, owned); got != 0o700 {
		t.Errorf("owned mode %v", got)
	}
	if err := MkdirShared(shared); err != nil {
		t.Errorf("an existing directory: %v", err)
	}
	if err := mkdirOwned(owned, -1, -1); err != nil {
		t.Errorf("an existing directory: %v", err)
	}
}

func TestDirectoryThatIsAFileIsRefused(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MkdirShared(file); err == nil {
		t.Error("MkdirShared accepted a file")
	}
	if err := mkdirOwned(file, -1, -1); err == nil {
		t.Error("mkdirOwned accepted a file")
	}
	if err := MkdirShared(filepath.Join(file, "below")); err == nil {
		t.Error("MkdirShared made a directory below a file")
	}
}

func TestCreateOnlyNeverReplaces(t *testing.T) {
	strictUmask(t)
	file := filepath.Join(t.TempDir(), "name")
	if err := createOnly(file, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := createOnly(file, []byte("two"), 0o644); err == nil {
		t.Error("createOnly replaced a file")
	}
	if got := string(readFile(t, file)); got != "one" {
		t.Errorf("content %q", got)
	}
	if got := mode(t, file); got != 0o644 {
		t.Errorf("mode %v", got)
	}
	entries, err := os.ReadDir(filepath.Dir(file))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("a temporary was left: %v", entries)
	}
}
