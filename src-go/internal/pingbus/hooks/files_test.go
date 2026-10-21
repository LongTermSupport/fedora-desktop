package hooks

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestPyJoinAndParentOf(t *testing.T) {
	joins := []struct{ base, rel, want string }{
		{"/a", "b", "/a/b"},
		{"/a/", "./b/", "/a/b"},
		{"/a", "/c", "/c"},
		{"/a", "../b", "/a/../b"},
		{"//x", "y", "//x/y"},
		{".", "", "."},
	}
	for _, c := range joins {
		if got := pyJoin(c.base, c.rel); got != c.want {
			t.Errorf("pyJoin(%q, %q) = %q, want %q", c.base, c.rel, got, c.want)
		}
	}
	for in, want := range map[string]string{"/a/b": "/a", "/a": "/", "a": ".", "a/b": "a"} {
		if got := parentOf(in); got != want {
			t.Errorf("parentOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

// inbox._read_regular: no symlink, no FIFO to block on, a regular file of at most the limit.
func TestReadRegular(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good")
	writeFile(t, good, "abc", 0o600)
	got, err := readRegular(good, 3)
	if err != nil || string(got.raw) != "abc" {
		t.Fatalf("good: %q, %v", got.raw, err)
	}
	if _, err := readRegular(good, 2); !errors.Is(err, errTooLarge) {
		t.Errorf("too large: %v", err)
	}
	if _, err := readRegular(filepath.Join(dir, "absent"), 3); !isErrno(err, syscall.ENOENT) {
		t.Errorf("absent: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(good, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readRegular(link, 3); !isErrno(err, syscall.ELOOP) {
		t.Errorf("symlink: %v", err)
	}
	if got, err := readFollowing(link, 3); err != nil || string(got.raw) != "abc" {
		t.Errorf("a reader that follows links: %q, %v", got.raw, err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readRegular(fifo, 3); !errors.Is(err, errNotRegular) {
		t.Errorf("fifo: %v", err)
	}
	if _, err := readRegular(dir, 3); !errors.Is(err, errNotRegular) {
		t.Errorf("directory: %v", err)
	}
}

func listTemps(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var temps []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			temps = append(temps, entry.Name())
		}
	}
	return temps
}

// H88 write_json_file: compact JSON, no newline, 0600, through a temporary file renamed over
// the target. The membership writer: sorted keys, default separators and a newline (H83).
func TestWriters(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "said-goodbye.json")
	writeFile(t, target, "old", 0o644)
	if err := writeJSONFile(target, pyobj{{"v", 1}, {"attempt_claimed_ms", int64(5)}}); err != nil {
		t.Fatal(err)
	}
	assertFile(t, target, `{"v":1,"attempt_claimed_ms":5}`, 0o600)
	told := filepath.Join(dir, "told.json")
	if err := writePrivateJSON(told, pyobj{{"v", 1}, {"generation", 2}}); err != nil {
		t.Fatal(err)
	}
	assertFile(t, told, "{\"generation\": 2, \"v\": 1}\n", 0o600)
	plain := filepath.Join(dir, "spawned")
	if err := writeAtomic(plain, []byte("12")); err != nil {
		t.Fatal(err)
	}
	assertFile(t, plain, "12", 0o600)
	if temps := listTemps(t, dir); len(temps) != 0 {
		t.Errorf("temporary files left: %v", temps)
	}
	missing := filepath.Join(dir, "absent", "x.json")
	for name, write := range map[string]func() error{
		"writeJSONFile":    func() error { return writeJSONFile(missing, pyobj{}) },
		"writePrivateJSON": func() error { return writePrivateJSON(missing, pyobj{}) },
		"writeAtomic":      func() error { return writeAtomic(missing, nil) },
	} {
		if err := write(); !isErrno(err, syscall.ENOENT) {
			t.Errorf("%s into a missing directory: %v", name, err)
		}
	}
	if err := writeJSONFile(target, pyobj{{"x", struct{}{}}}); err == nil {
		t.Error("a value that cannot be written must fail")
	}
	if err := writePrivateJSON(told, pyobj{{"x", struct{}{}}}); err == nil {
		t.Error("a value that cannot be written must fail")
	}
}

// A rename onto a directory fails after the temporary file was written: it is removed.
func TestWriterRemovesItsTemporaryFileOnFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "occupied")
	writeFile(t, filepath.Join(target, "inside"), "x", 0o600)
	if err := writeAtomic(target, []byte("1")); err == nil {
		t.Fatal("renaming over a non-empty directory must fail")
	}
	if temps := listTemps(t, dir); len(temps) != 0 {
		t.Errorf("temporary files left: %v", temps)
	}
}

func assertFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != content {
		t.Errorf("%s: %q, want %q", path, raw, content)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != mode {
		t.Errorf("%s: mode %o, want %o", path, info.Mode().Perm(), mode)
	}
}
