package testfiles

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fifotest"
)

// world is a directory the test owns, holding file, a link to it inside the directory, and a link
// to a file beside the directory.
type world struct {
	dir, outside string
}

func newWorld(t *testing.T) world {
	t.Helper()
	base := t.TempDir()
	w := world{dir: filepath.Join(base, "dir"), outside: filepath.Join(base, "outside")}
	for _, step := range []func() error{
		func() error { return os.Mkdir(w.dir, 0o700) },
		func() error { return os.WriteFile(filepath.Join(w.dir, "file"), []byte("inside"), 0o600) },
		func() error { return os.WriteFile(w.outside, []byte("outside"), 0o600) },
		func() error { return os.Symlink("file", filepath.Join(w.dir, "inlink")) },
		func() error { return os.Symlink(w.outside, filepath.Join(w.dir, "outlink")) },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

// The name is taken beneath the directory, whether it is relative or the full path the test
// built; a link that stays inside is followed.
func TestReadByNameAndByFullPath(t *testing.T) {
	w := newWorld(t)
	for _, name := range []string{"file", filepath.Join(w.dir, "file"), "inlink"} {
		if got := Read(t, w.dir, name); string(got) != "inside" {
			t.Errorf("Read %s: %q", name, got)
		}
	}
}

// Nothing that leaves the directory is read: not by .., not by a full path elsewhere, not through
// a link that leads out.
func TestWhatLeavesTheDirectoryIsRefused(t *testing.T) {
	w := newWorld(t)
	for _, name := range []string{"../outside", w.outside, "outlink", "/etc/os-release"} {
		if got, err := readIn(w.dir, name); err == nil {
			t.Errorf("readIn %s: %q", name, got)
		}
		if f, err := openIn(w.dir, name, os.O_RDONLY, 0); err == nil {
			t.Errorf("openIn %s: opened %s", name, f.Name())
		}
	}
}

func TestFindIsFalseForNoFile(t *testing.T) {
	w := newWorld(t)
	if data, found := Find(t, w.dir, "none"); found || data != nil {
		t.Errorf("a missing file: %q %v", data, found)
	}
	if data, found := Find(t, w.dir, "file"); !found || string(data) != "inside" {
		t.Errorf("a file: %q %v", data, found)
	}
	if _, err := readIn(w.dir, "none"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a missing file: %v", err)
	}
	if _, err := readIn(filepath.Join(w.dir, "none"), "file"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a missing directory: %v", err)
	}
}

// Try never ends the test: a diagnostic that reports a file hands back why it cannot be read, and
// a name that leaves the directory is such a reason.
func TestTryAnswersWithTheReason(t *testing.T) {
	w := newWorld(t)
	if data, err := Try(w.dir, "file"); err != nil || string(data) != "inside" {
		t.Errorf("a file: %q %v", data, err)
	}
	if _, err := Try(w.dir, "none"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a missing file: %v", err)
	}
	if data, err := Try(w.dir, "outlink"); err == nil {
		t.Errorf("a link that leads out: %q", data)
	}
	if data, err := Try(filepath.Join(w.dir, "none"), "file"); err == nil {
		t.Errorf("a missing directory: %q", data)
	}
}

func TestOpenAndOpenFile(t *testing.T) {
	w := newWorld(t)
	f := Open(t, w.dir, "file")
	data, err := io.ReadAll(f)
	if err = errors.Join(err, f.Close()); err != nil || string(data) != "inside" {
		t.Fatalf("Open: %q %v", data, err)
	}
	g := OpenFile(t, w.dir, filepath.Join(w.dir, "made"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	_, err = g.WriteString("made")
	if err = errors.Join(err, g.Close()); err != nil {
		t.Fatal(err)
	}
	if got := Read(t, w.dir, "made"); string(got) != "made" {
		t.Errorf("OpenFile wrote %q", got)
	}
	if _, err = openIn(w.dir, "made", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600); !errors.Is(err, os.ErrExist) {
		t.Errorf("a second exclusive create: %v", err)
	}
}

func TestExecutableIsTheRunningTestBinary(t *testing.T) {
	f := Executable(t)
	head := make([]byte, 4)
	_, err := io.ReadFull(f, head)
	if err = errors.Join(err, f.Close()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(head, []byte("\x7fELF")) {
		t.Errorf("the first bytes are %q", head)
	}
}

// A name that is not a regular file is refused, never opened: a test reading what the code under
// test left in a directory it found (a glob of run directories) must not hang on a named pipe
// (#195). A link to a regular file inside the directory is still read.
func TestWhatIsNotARegularFileIsRefused(t *testing.T) {
	w := newWorld(t)
	opened := fifotest.Plant(t, filepath.Join(w.dir, "pipe"))
	linkOpened := fifotest.Plant(t, filepath.Join(w.dir, "linked"))
	if err := os.Symlink("linked", filepath.Join(w.dir, "pipelink")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pipe", "pipelink", "."} {
		if data, err := Try(w.dir, name); err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Errorf("Try %s: %q %v, want it refused as not a regular file", name, data, err)
		}
	}
	if opened() || linkOpened() {
		t.Fatal("a named pipe was opened")
	}
	if got := Read(t, w.dir, "inlink"); string(got) != "inside" {
		t.Errorf("Read inlink: %q", got)
	}
}
