package ccc

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
)

func prepare(t *testing.T, dir string) (*execx.Fake, error) {
	t.Helper()
	f := &execx.Fake{}
	f.On([]string{"git"}, execx.Result{})
	return f, prepareFolder(context.Background(), gitx.New(f), dir, Claude, Identity{Login: "pat", FullName: "Pat Example"})
}

func TestANewFolderIsARepositoryWithItsOwnIdentity(t *testing.T) {
	dir := filepath.Join(t.TempDir(), FolderName)
	f, err := prepare(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"init", "--quiet", "--initial-branch", "main"},
		{"config", "--local", "user.name", "Pat Example"},
		{"config", "--local", "user.email", "pat@localhost"},
		{"add", "--all"},
		{"-c", "commit.gpgsign=false", "commit", "--quiet", "--message", "ccc: start the folder"},
	}
	calls := f.Calls()
	got := make([][]string, 0, len(calls))
	for _, c := range calls {
		got = append(got, c.Args)
		if c.Dir != dir {
			t.Fatalf("git ran in %q", c.Dir)
		}
	}
	if !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("got %v", got)
	}
}

func TestAnIdentityWithNoFullNameUsesTheLogin(t *testing.T) {
	if got := (Identity{Login: "pat"}).name(); got != "pat" {
		t.Fatalf("got %q", got)
	}
}

func TestTheIgnoreFileCoversJunkAndSecrets(t *testing.T) {
	dir := filepath.Join(t.TempDir(), FolderName)
	if _, err := prepare(t, dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Clean(filepath.Join(dir, ".gitignore")))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"node_modules/", ".venv/", "__pycache__/", ".DS_Store", "*.key", ".env", "dist/"} {
		if !strings.Contains(string(data), want+"\n") {
			t.Fatalf(".gitignore lacks %s", want)
		}
	}
}

func TestAnExistingFolderKeepsItsFilesAndIsInitialised(t *testing.T) {
	dir := filepath.Join(t.TempDir(), FolderName)
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"notes.txt", "CLAUDE.md", ".gitignore"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("mine\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f, err := prepare(t, dir)
	if err != nil || !f.Called("git init") {
		t.Fatalf("%v, %v", err, f.Calls())
	}
	for _, name := range []string{"notes.txt", "CLAUDE.md", ".gitignore"} {
		if data, err := os.ReadFile(filepath.Clean(filepath.Join(dir, name))); err != nil || string(data) != "mine\n" {
			t.Fatalf("%s changed: %q %v", name, data, err)
		}
	}
}

func TestARepositoryIsLeftAsItIs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), FolderName)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := prepare(t, dir)
	if err != nil || len(f.Calls()) != 0 {
		t.Fatalf("%v, %v", err, f.Calls())
	}
}

func TestASymlinkOrAFileIsRefused(t *testing.T) {
	base := t.TempDir()
	link := filepath.Join(base, "link")
	if err := os.Symlink(base, link); err != nil {
		t.Fatal(err)
	}
	if _, err := prepare(t, link); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("symlink: %v", err)
	}
	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepare(t, file); err == nil || !strings.Contains(err.Error(), "not a folder") {
		t.Fatalf("file: %v", err)
	}
}

func TestAFailedInitStopsThePreparation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), FolderName)
	f := &execx.Fake{}
	f.On([]string{"git"}, execx.Result{ExitCode: 128, Stderr: []byte("fatal")})
	if err := prepareFolder(context.Background(), gitx.New(f), dir, Claude, Identity{Login: "pat"}); err == nil {
		t.Fatal("want an error")
	}
	if len(f.Calls()) != 1 {
		t.Fatalf("ran on: %v", f.Calls())
	}
}
