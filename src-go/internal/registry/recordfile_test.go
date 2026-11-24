package registry

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fedora-desktop#398: a record's name= is not held to its file's name, so a session's record is
// found by reading the registry, and acted on through the file it was read from.

// renamedHost is a registry holding ccy-a in the file renamed-a, ccy-other in the file ccy-a, and
// an unreadable file ccy-bad.
func renamedHost(t *testing.T) (*Store, string, string, string) {
	t.Helper()
	s := newStore(t)
	for _, name := range []string{"ccy-a", "ccy-other"} {
		rec := sample()
		rec.Name = name
		if err := s.Write(rec); err != nil {
			t.Fatal(err)
		}
	}
	moved, other, bad := filepath.Join(s.Dir(), "renamed-a"), filepath.Join(s.Dir(), "ccy-a"), filepath.Join(s.Dir(), "ccy-bad")
	if err := os.Rename(other, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(s.Dir(), "ccy-other"), other); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("junk\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return s, moved, other, bad
}

func TestFilesOfIsEveryFileAnsweringToAName(t *testing.T) {
	s, moved, other, bad := renamedHost(t)
	reading, err := s.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]string{"ccy-a": {moved}, "ccy-other": {other}, "ccy-bad": {bad}, "ghost": nil} {
		if got := reading.FilesOf(name); !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", name, got, want)
		}
	}
}

func TestFindIsASessionsOneRecordAndItsFile(t *testing.T) {
	s, moved, other, bad := renamedHost(t)
	rec, file, err := s.Find("ccy-a")
	if err != nil || rec.Name != "ccy-a" || file != moved {
		t.Fatalf("ccy-a: %+v %s %v", rec, file, err)
	}
	if rec, file, err = s.Find("ccy-other"); err != nil || rec.Name != "ccy-other" || file != other {
		t.Fatalf("ccy-other: %+v %s %v", rec, file, err)
	}
	if _, _, err = s.Find("ghost"); !errors.Is(err, ErrNoRecord) || !strings.Contains(err.Error(), "session ghost has no record in "+s.Dir()) {
		t.Fatalf("ghost: %v", err)
	}
	if _, _, err = s.Find("ccy-bad"); !errors.Is(err, ErrUnreadable) || !strings.Contains(err.Error(), bad) {
		t.Fatalf("unreadable: %v", err)
	}
	twin := sample()
	twin.Name = "ccy-a"
	if err = s.Write(twin); err == nil {
		// The file ccy-a now holds a second record answering to ccy-a.
		_, _, err = s.Find("ccy-a")
	}
	if err == nil || !strings.Contains(err.Error(), "more than one record answers to ccy-a") {
		t.Fatalf("two records: %v", err)
	}
}

func TestRemoveSessionRemovesEveryFileAnsweringToTheSession(t *testing.T) {
	s, moved, other, bad := renamedHost(t)
	if err := s.RemoveSession("ccy-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(moved); err == nil {
		t.Fatal("ccy-a's record is left behind")
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("ccy-other's record, in the file named ccy-a, was removed: %v", err)
	}
	if err := s.RemoveSession("ccy-bad"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bad); err == nil {
		t.Fatal("an unreadable record answering to the session by its file's name is left behind")
	}
	if err := s.RemoveSession("ghost"); err != nil {
		t.Fatalf("a session with no record: %v", err)
	}
	if err := New(filepath.Join(t.TempDir(), "absent")).RemoveSession("ccy-a"); err != nil {
		t.Fatalf("an absent registry: %v", err)
	}
}

func TestListNamesAnUnreadableRecordByItsFile(t *testing.T) {
	s, _, _, bad := renamedHost(t)
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(list.UnreadableFiles, []string{bad}) {
		t.Fatalf("unreadable %v", list.UnreadableFiles)
	}
}
