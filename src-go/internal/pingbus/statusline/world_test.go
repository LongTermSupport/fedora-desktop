package statusline

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func newTestWorld(t *testing.T) *world {
	t.Helper()
	return newWorld(t.TempDir(), time.Unix(0, 0), nil)
}

// S33: a fingerprint is taken of every path the render looked at, absent ones included, and a
// link carries what it points at.
func TestWorldNotesEveryPathOnceWithItsLinkTarget(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	w := newTestWorld(t)
	w.note(file)
	w.note(link)
	w.note(filepath.Join(dir, "missing"))
	w.note(file)
	if len(w.prints) != 3 {
		t.Fatalf("prints: %+v", w.prints)
	}
	if w.prints[0].Lstat.Errno != 0 || !w.prints[0].Lstat.isRegular() || w.prints[0].Target != nil {
		t.Errorf("file: %+v", w.prints[0])
	}
	if !w.prints[1].Lstat.isLink() || w.prints[1].Target == nil || !w.prints[1].Target.isRegular() {
		t.Errorf("link: %+v", w.prints[1])
	}
	if w.prints[2].Lstat.Errno != int(syscall.ENOENT) || w.prints[2].Lstat.exists() {
		t.Errorf("missing: %+v", w.prints[2])
	}
	for _, fp := range w.prints {
		if !fp.unchanged() {
			t.Errorf("%s: changed with nothing changed", fp.Path)
		}
	}
}

// S33: a chmod (ctime), an in-place rewrite of the same size (mtime), a replace (ino) and a
// file appearing all change the fingerprint.
func TestFingerprintSeesEachKindOfChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	write := func(data string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := func() fingerprint {
		w := newTestWorld(t)
		w.note(path)
		return w.prints[0]
	}
	absent := snapshot()
	write("aaaa")
	if absent.unchanged() {
		t.Error("a file that appeared reads as unchanged")
	}
	before := snapshot()
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if before.unchanged() {
		t.Error("a chmod reads as unchanged")
	}
	before = snapshot()
	old := time.Unix(1000, 0)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if before.unchanged() {
		t.Error("a re-dated file reads as unchanged")
	}
	before = snapshot()
	other := filepath.Join(dir, "other")
	if err := os.WriteFile(other, []byte("bbbb"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(other, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(other, path); err != nil {
		t.Fatal(err)
	}
	if before.unchanged() {
		t.Error("a file replaced by rename reads as unchanged")
	}
}

// S34's racy rule: a path changed within racyWindow of the render is not trusted to a cache.
func TestWorldIsRacyWhenAPathChangedJustBefore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := info.ModTime()
	recent := newWorld("/", changed.Add(racyWindow/2), nil)
	recent.note(path)
	if !recent.racy() {
		t.Error("a path changed half a window ago is not racy")
	}
	settled := newWorld("/", changed.Add(racyWindow+time.Second), nil)
	settled.note(path)
	settled.note(filepath.Join(path, "absent"))
	if settled.racy() {
		t.Error("a path changed long ago is racy")
	}
}

// A relative path is read against the render's own working directory.
func TestWorldResolvesRelativePathsAgainstItsDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rel"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := newWorld(dir, time.Unix(0, 0), nil)
	if st := w.note("rel"); !st.isRegular() {
		t.Fatalf("rel: %+v", st)
	}
	if w.prints[0].Path != filepath.Join(dir, "rel") {
		t.Errorf("path recorded as %q", w.prints[0].Path)
	}
	if w.abs("/x") != "/x" {
		t.Errorf("absolute path changed: %q", w.abs("/x"))
	}
}

// read is the Python's open: a symlink refused only with noFollow, a FIFO never blocks, a
// file over the limit refused.
func TestWorldReadRefusesWhatThePythonRefuses(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	link := filepath.Join(dir, "link")
	fifo := filepath.Join(dir, "fifo")
	if err := os.WriteFile(file, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	w := newTestWorld(t)
	if got, err := w.read(link, false, 5); err != nil || string(got.raw) != "12345" {
		t.Errorf("followed link: %q %v", got.raw, err)
	}
	if _, err := w.read(link, true, 5); !isErrno(err, syscall.ELOOP) {
		t.Errorf("nofollow link: %v", err)
	}
	if _, err := w.read(fifo, true, 5); err != errNotRegular {
		t.Errorf("fifo: %v", err)
	}
	if _, err := w.read(file, true, 4); err != errTooLarge {
		t.Errorf("over the limit: %v", err)
	}
	if _, err := w.read(filepath.Join(dir, "none"), true, 4); !isErrno(err, syscall.ENOENT) {
		t.Errorf("missing: %v", err)
	}
}
