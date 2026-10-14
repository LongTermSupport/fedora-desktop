package fsx

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestSubRootsAtAChildAndKeepsTheDisplayPath(t *testing.T) {
	e := newEnv(t, "/var/lib")
	e.mkdir("agent-bus/alpha")
	sub, err := e.fs.Sub("agent-bus")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := sub.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	if _, err := sub.EnsureDir("alpha/db", 0o700, e.self); err != nil {
		t.Fatal(err)
	}
	if got := e.changed(); got != "CHANGED\tdirectory /var/lib/agent-bus/alpha/db\n" {
		t.Fatalf("output %q", got)
	}
}

func TestDisplayIsTheAbsolutePathAsMessagesShowIt(t *testing.T) {
	e := newEnv(t, "/var/lib")
	if got := e.fs.Display("agent-bus/team.json"); got != "/var/lib/agent-bus/team.json" {
		t.Fatalf("got %q", got)
	}
	if got := e.fs.Display("."); got != "/var/lib" {
		t.Fatalf("got %q", got)
	}
}

func TestSubRefusesAMissingChildASymlinkOutAndATraversal(t *testing.T) {
	outside := t.TempDir()
	e := newEnv(t, "")
	e.symlink(outside, "escape")
	for _, name := range []string{"absent", "escape", "../x", "/abs", ""} {
		if sub, err := e.fs.Sub(name); err == nil {
			t.Fatalf("Sub(%q) accepted", name)
		} else if sub != nil {
			t.Fatalf("Sub(%q) returned a root with an error", name)
		}
	}
}

func TestSubErrorsKeepNotExistRecognisable(t *testing.T) {
	e := newEnv(t, "/x")
	_, err := e.fs.Sub("absent")
	if !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), "/x/absent") {
		t.Fatalf("error %v", err)
	}
}

func TestQuietWritesWithoutReportingAChange(t *testing.T) {
	e := newEnv(t, "")
	changed, err := e.fs.Quiet().PutBytes("record", []byte("x"), "r", 0o600, e.self)
	if err != nil || !changed {
		t.Fatalf("changed %v, err %v", changed, err)
	}
	if got := e.changed(); got != "" {
		t.Fatalf("a quiet write printed %q", got)
	}
	if e.read("r") != "x" {
		t.Fatal("the quiet write did not land")
	}
}

func TestReadFileReadsInsideTheRootOnly(t *testing.T) {
	outside := t.TempDir()
	e := newEnv(t, "")
	e.write("a", "hello", 0o600)
	e.symlink("a", "inside")
	e.symlink(outside, "escape")
	got, err := e.fs.ReadFile("inside")
	if err != nil || string(got) != "hello" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := e.fs.ReadFile("escape/x"); err == nil {
		t.Fatal("read through a symlink out of the root")
	}
	if _, err := e.fs.ReadFile("absent"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("error %v", err)
	}
}

func TestOpenReturnsAReadableFile(t *testing.T) {
	e := newEnv(t, "")
	e.write("a", "hello", 0o600)
	f, err := e.fs.Open("a")
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(f)
	if closeErr := f.Close(); closeErr != nil || readErr != nil || string(data) != "hello" {
		t.Fatalf("%q, %v, %v", data, readErr, closeErr)
	}
	if _, err := e.fs.Open("../x"); err == nil {
		t.Fatal("opened outside the root")
	}
}

func TestReadDirListsTheRootAndAChild(t *testing.T) {
	e := newEnv(t, "")
	e.mkdir("d")
	e.write("d/one", "1", 0o600)
	e.write("two", "2", 0o600)
	names := func(entries []fs.DirEntry) []string {
		all := make([]string, 0, len(entries))
		for _, entry := range entries {
			all = append(all, entry.Name())
		}
		slices.Sort(all)
		return all
	}
	top, err := e.fs.ReadDir(".")
	if err != nil || !slices.Equal(names(top), []string{"d", "two"}) {
		t.Fatalf("root: %v, %v", names(top), err)
	}
	child, err := e.fs.ReadDir("d")
	if err != nil || !slices.Equal(names(child), []string{"one"}) {
		t.Fatalf("child: %v, %v", names(child), err)
	}
	if _, err := e.fs.ReadDir("absent"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("error %v", err)
	}
}

func TestLstatSeesALinkAndStatFollowsIt(t *testing.T) {
	e := newEnv(t, "")
	e.mkdir("real")
	e.symlink("real", "link")
	l, err := e.fs.Lstat("link")
	if err != nil || l.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("lstat %v, %v", l, err)
	}
	s, err := e.fs.Stat("link")
	if err != nil || !s.IsDir() {
		t.Fatalf("stat %v, %v", s, err)
	}
	if _, err := e.fs.Lstat("absent"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("error %v", err)
	}
	if _, err := e.fs.Stat("absent"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("error %v", err)
	}
}

func TestRemoveIfPresent(t *testing.T) {
	e := newEnv(t, "")
	e.write("a", "x", 0o600)
	gone, err := e.fs.RemoveIfPresent("a")
	if err != nil || !gone || exists(e.path("a")) {
		t.Fatalf("gone %v, err %v", gone, err)
	}
	gone, err = e.fs.RemoveIfPresent("a")
	if err != nil || gone {
		t.Fatalf("a second removal: gone %v, err %v", gone, err)
	}
	if _, err := e.fs.RemoveIfPresent("../x"); err == nil {
		t.Fatal("removed outside the root")
	}
}

func TestRemoveAllRemovesATreeAndNotAFollowedLink(t *testing.T) {
	outside := t.TempDir()
	writeFile(t, outside+"/keep", "x", 0o600)
	e := newEnv(t, "")
	e.mkdir("t/sub")
	e.write("t/sub/f", "x", 0o600)
	e.symlink(outside, "t/link")
	if err := e.fs.RemoveAll("t"); err != nil {
		t.Fatal(err)
	}
	if exists(e.path("t")) {
		t.Fatal("the tree is still there")
	}
	if !exists(outside + "/keep") {
		t.Fatal("RemoveAll followed a link out of the root")
	}
	if err := e.fs.RemoveAll("t"); err != nil {
		t.Fatalf("removing what is not there: %v", err)
	}
	if err := e.fs.RemoveAll("../x"); err == nil {
		t.Fatal("removed outside the root")
	}
}

func TestEnsureFileModeRepairsModeAndKeepsContent(t *testing.T) {
	e := newEnv(t, "/s")
	e.write("secret", "abc", 0o644)
	changed, err := e.fs.EnsureFileMode("secret mode", "secret", 0o600, e.self)
	if err != nil || !changed {
		t.Fatalf("changed %v, err %v", changed, err)
	}
	if modeOf(t, e.path("secret")).Perm() != 0o600 || e.read("secret") != "abc" {
		t.Fatal("mode not repaired or content touched")
	}
	if got := e.changed(); got != "CHANGED\tsecret mode\n" {
		t.Fatalf("output %q", got)
	}
	changed, err = e.fs.EnsureFileMode("secret mode", "secret", 0o600, e.self)
	if err != nil || changed || e.changed() != "" {
		t.Fatalf("a correct file changed: %v, %v", changed, err)
	}
}

func TestEnsureFileModeFixesOwner(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("changing the owner needs root")
	}
	e := newEnv(t, "")
	e.write("secret", "abc", 0o600)
	changed, err := e.fs.EnsureFileMode("x", "secret", 0o600, Owner{UID: 4321, GID: 4322})
	if err != nil || !changed {
		t.Fatalf("changed %v, err %v", changed, err)
	}
	if o, ok := ownerOf(infoOf(t, e.path("secret"))); !ok || o != (Owner{UID: 4321, GID: 4322}) {
		t.Fatalf("owner %v", o)
	}
}

func TestEnsureFileModeRefusesWhatIsNotAPlainFile(t *testing.T) {
	e := newEnv(t, "")
	e.write("real", "x", 0o600)
	e.symlink("real", "link")
	e.mkdir("dir")
	for _, name := range []string{"link", "dir"} {
		if _, err := e.fs.EnsureFileMode("x", name, 0o600, e.self); err == nil {
			t.Fatalf("EnsureFileMode(%q) accepted", name)
		}
	}
	if _, err := e.fs.EnsureFileMode("x", "absent", 0o600, e.self); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("error %v", err)
	}
	if _, err := e.fs.EnsureFileMode("x", "../x", 0o600, e.self); err == nil {
		t.Fatal("accepted a path outside the root")
	}
}
