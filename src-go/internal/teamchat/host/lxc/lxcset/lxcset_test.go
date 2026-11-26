package lxcset

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// root is a host root with the status tree the play makes.
func root(t *testing.T) string {
	t.Helper()
	r := t.TempDir()
	if err := os.MkdirAll(r+paths.StatusRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	return r
}

func sample() Status {
	return Status{BootID: "boot-1", WrittenMS: 1_700_000_000_000, Lines: []Line{
		{Kind: Up, Container: "c1"}, {Kind: Down, Container: "c2", Why: "STOPPED"}, {Kind: Unrecorded, Container: "c4"},
	}}
}

// The status is written 0640 to the owner group and read back as it was written.
func TestWriteThenRead(t *testing.T) {
	r := root(t)
	if err := Write(r, sample(), os.Getgid()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(r + paths.LXCSetStatusFile)
	if err != nil || info.Mode().Perm() != Mode {
		t.Fatalf("mode %v, %v", info, err)
	}
	got, found, err := Read(r)
	want := sample()
	if err != nil || !found || got.BootID != want.BootID || got.WrittenMS != want.WrittenMS || got.V != 1 || !slices.Equal(got.Lines, want.Lines) {
		t.Fatalf("%+v %v %v", got, found, err)
	}
}

// No file is not found, not an empty set; a set with no line reads back as none.
func TestReadWithNoFile(t *testing.T) {
	r := root(t)
	if _, found, err := Read(r); found || err != nil {
		t.Fatalf("%v %v", found, err)
	}
	if err := Write(r, Status{BootID: "b", WrittenMS: 1}, -1); err != nil {
		t.Fatal(err)
	}
	if got, found, err := Read(r); !found || err != nil || len(got.Lines) != 0 {
		t.Fatalf("%+v %v %v", got, found, err)
	}
}

// A file that is not what Write writes is refused, never read in part: an unknown key, another
// version, no boot id, a line in no kind's form.
func TestReadRefusesAnythingWriteNeverWrites(t *testing.T) {
	for _, bad := range []string{
		`{`,
		`{"v":1,"boot_id":"b","written_ms":1,"lines":[],"extra":1}`,
		`{"v":2,"boot_id":"b","written_ms":1,"lines":[]}`,
		`{"v":1,"boot_id":"","written_ms":1,"lines":[]}`,
		`{"v":1,"boot_id":"b","written_ms":0,"lines":[]}`,
		`{"v":1,"boot_id":"b","written_ms":1,"lines":[{"kind":"WEIRD","container":"c1","why":""}]}`,
		`{"v":1,"boot_id":"b","written_ms":1,"lines":[{"kind":"DOWN","container":"c1","why":""}]}`,
		`{"v":1,"boot_id":"b","written_ms":1,"lines":[{"kind":"UP","container":"","why":""}]}`,
		`{"v":1,"boot_id":"b","written_ms":1,"lines":[{"kind":"UP","container":"c 1","why":""}]}`,
		`{"v":1,"boot_id":"b","written_ms":1,"lines":[{"kind":"NO-RECORD","container":"c1","why":"x"}]}`,
		`{"v":1,"boot_id":"b","written_ms":1,"lines":[]} {}`,
	} {
		r := root(t)
		if err := os.WriteFile(filepath.Join(r, paths.LXCSetStatusFile), []byte(bad), 0o640); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Read(r); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", bad, err)
		}
	}
}

// Write refuses a status Read would refuse, so a bad line never reaches the reader.
func TestWriteRefusesAnInvalidStatus(t *testing.T) {
	r := root(t)
	bad := sample()
	bad.Lines = append(bad.Lines, Line{Kind: Down, Container: "c9"})
	if err := Write(r, bad, -1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("%v", err)
	}
	if err := Write(r, Status{WrittenMS: 1}, -1); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "boot id") {
		t.Fatalf("no boot id: %v", err)
	}
	if _, err := os.Stat(r + paths.LXCSetStatusFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused status was written: %v", err)
	}
}
