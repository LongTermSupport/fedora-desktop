package ccyenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func lib(t *testing.T, withFile bool) string {
	t.Helper()
	dir := t.TempDir()
	if withFile {
		if err := os.WriteFile(filepath.Join(dir, SessionLib), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestMissingIsEmptyWhenTheLibraryIsInstalled(t *testing.T) {
	if got := Missing("ccy-teams", lib(t, true), func() bool { return true }); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestMissingInsideAContainerSaysTheToolIsInTheWrongPlace(t *testing.T) {
	want := "ccy-teams runs on the host, not inside a CCY container.\n"
	if got := Missing("ccy-teams", lib(t, false), func() bool { return true }); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestMissingOnTheHostNamesTheFileAndThePlay(t *testing.T) {
	dir := lib(t, false)
	got := Missing("ccy-sessions", dir, func() bool { return false })
	want := "CCY is not installed here (" + filepath.Join(dir, SessionLib) + " missing). Run playbooks/imports/play-claude-yolo.yml.\n"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestADirectoryIsNotTheLibrary(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, SessionLib), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := Missing("ccy-teams", dir, func() bool { return false }); !strings.Contains(got, "CCY is not installed here") {
		t.Fatalf("got %q", got)
	}
}

func TestInContainerIsTrueOnlyWhereTheWorkspaceIsMounted(t *testing.T) {
	root := t.TempDir()
	if InContainerAt(filepath.Join(root, "absent")) {
		t.Fatal("no workspace directory, no container")
	}
	if !InContainerAt(root) {
		t.Fatal("a workspace directory is a container")
	}
	file := filepath.Join(root, "f")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if InContainerAt(file) {
		t.Fatal("a file is not the workspace")
	}
	if InContainer() != InContainerAt(Workspace) {
		t.Fatal("InContainer asks about the workspace")
	}
}
