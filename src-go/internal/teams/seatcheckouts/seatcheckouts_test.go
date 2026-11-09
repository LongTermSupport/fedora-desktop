package seatcheckouts

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
)

func listIn(t *testing.T) *List {
	t.Helper()
	return Of(registry.New(filepath.Join(t.TempDir(), "ccy", "sessions")))
}

func writeList(t *testing.T, l *List, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(l.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.Path(), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTheListIsBesideTheSessionRecords(t *testing.T) {
	base := filepath.Join(t.TempDir(), "ccy")
	l := Of(registry.New(filepath.Join(base, "sessions")))
	if l.Path() != filepath.Join(base, "seat-checkouts") {
		t.Fatalf("path %q", l.Path())
	}
}

func TestNoFileIsNoCheckouts(t *testing.T) {
	got, err := listIn(t).Read()
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestAddKeepsEachCheckoutOnceInTheOrderAdded(t *testing.T) {
	l := listIn(t)
	one, two := t.TempDir(), t.TempDir()
	for _, dir := range []string{one, two, one, two + "/"} {
		if err := l.Add(dir); err != nil {
			t.Fatal(err)
		}
	}
	got, err := l.Read()
	if err != nil || !slices.Equal(got, []string{one, two}) {
		t.Fatalf("got %v, %v", got, err)
	}
	info, err := os.Stat(l.Path())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, %v", info, err)
	}
}

func TestAddDropsCheckoutsThatNoLongerExist(t *testing.T) {
	l := listIn(t)
	kept := t.TempDir()
	added := t.TempDir()
	gone := filepath.Join(t.TempDir(), "gone")
	writeList(t, l, gone+"\n"+kept+"\n"+gone+"\n")
	if err := l.Add(added); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(l.Path())
	if err != nil || string(data) != kept+"\n"+added+"\n" {
		t.Fatalf("file %q, %v", data, err)
	}
	info, err := os.Stat(l.Path())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, %v", info, err)
	}
}

func TestTwoWritersAppendingTheSameCheckoutReadAsOne(t *testing.T) {
	l := listIn(t)
	writeList(t, l, "/home/u/one\n\n/home/u/one\n/home/u/two\n")
	got, err := l.Read()
	if err != nil || !slices.Equal(got, []string{"/home/u/one", "/home/u/two"}) {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestAddRefusesWhatIsNotAnAbsolutePathOnOneLine(t *testing.T) {
	for _, dir := range []string{"", "rel/dir", "/home/u/a\nb"} {
		l := listIn(t)
		if err := l.Add(dir); err == nil {
			t.Fatalf("%q was added", dir)
		}
		if _, err := os.Stat(l.Path()); !os.IsNotExist(err) {
			t.Fatalf("%q: the file was written", dir)
		}
	}
}

func TestALineThatIsNotACheckoutIsAnErrorNamingTheFile(t *testing.T) {
	l := listIn(t)
	writeList(t, l, "/home/u/one\nrelative\n")
	_, err := l.Read()
	if err == nil || !strings.Contains(err.Error(), l.Path()) || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("err %v", err)
	}
	if err := l.Add("/home/u/three"); err == nil {
		t.Fatal("an add to an unreadable list succeeded")
	}
}

func TestAnUnreadableFileIsAnError(t *testing.T) {
	l := listIn(t)
	if err := os.MkdirAll(l.Path(), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Read(); err == nil {
		t.Fatal("a directory read as the list")
	}
	if err := l.Add("/home/u/one"); err == nil {
		t.Fatal("an add over a directory succeeded")
	}
}

func TestAnUnwritableListIsAnError(t *testing.T) {
	l := listIn(t)
	writeList(t, l, "")
	if err := os.Chmod(l.Path(), 0o400); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root writes a read-only file")
	}
	if err := l.Add("/home/u/one"); err == nil {
		t.Fatal("an add to a read-only list succeeded")
	}
}
