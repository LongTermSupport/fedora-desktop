package published

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// fakeGit answers `git ls-files` with canned listings.
type fakeGit struct {
	listed, deleted string
	code            int
	err             error
	argvs           [][]string
}

func (f *fakeGit) Run(_ context.Context, c proc.Cmd) (proc.Result, error) {
	f.argvs = append(f.argvs, c.Argv)
	if f.err != nil {
		return proc.Result{}, f.err
	}
	if f.code != 0 {
		return proc.Result{Code: f.code, Stderr: "fatal: not a git repository\n"}, nil
	}
	if strings.Contains(strings.Join(c.Argv, " "), "--deleted") {
		return proc.Result{Stdout: f.deleted}, nil
	}
	return proc.Result{Stdout: f.listed}, nil
}

// Y7: tracked or untracked-and-not-ignored, less the deleted, sorted and without repeats.
func TestFilesAreWhatGitWouldPublishLessTheDeleted(t *testing.T) {
	git := &fakeGit{listed: "b.py\x00a.py\x00gone.py\x00a.py\x00", deleted: "gone.py\x00"}
	got, err := Files(context.Background(), git, "/root")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.py", "b.py"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if joined := strings.Join(git.argvs[0], " "); joined != "git ls-files -z --cached --others --exclude-standard" {
		t.Fatalf("argv %q", joined)
	}
}

// Y7: git failing is an error that names the root and the reason, whether it could not start or
// exited non-zero.
func TestFilesRefuseWhenGitFails(t *testing.T) {
	for name, git := range map[string]*fakeGit{
		"status": {code: 128},
		"start":  {err: errors.New("no git")},
	} {
		_, err := Files(context.Background(), git, "/root")
		if err == nil || !strings.Contains(err.Error(), "cannot list the files git would publish under /root") {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

// Y8: the rows of one list, as whole segments anywhere in a path.
func TestTreesReadOneListAndExcludeWholeSegments(t *testing.T) {
	file := filepath.Join(t.TempDir(), "qa-trees.tsv")
	text := "# comment\n\nshell\t.git\npython\t.venv\npython\tvendor/x\n"
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	trees, err := Trees(file, "python")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{".venv", "vendor/x"}; !reflect.DeepEqual(trees, want) {
		t.Fatalf("got %v", trees)
	}
	for rel, want := range map[string]bool{
		".venv/lib/a.py": true, "a/.venv/b.py": true, "a/not.venv/b.py": false,
		"vendor/x/a.py": true, "src/vendor/x": true, "vendor/xy/a.py": false, "a.py": false,
	} {
		if got := Excluded(rel, trees); got != want {
			t.Errorf("Excluded(%q) = %v, want %v", rel, got, want)
		}
	}
}

// Y8: a reader that finds nothing proved nothing.
func TestTreesRefuseAMissingFileABadRowOrNoRowForTheList(t *testing.T) {
	dir := t.TempDir()
	if _, err := Trees(filepath.Join(dir, "none.tsv"), "python"); err == nil {
		t.Fatal("a missing file was read")
	}
	for name, text := range map[string]string{
		"short row": "python\n",
		"empty dir": "python\t\n",
		"no row":    "shell\t.git\n",
		"no rows":   "# only a comment\n",
	} {
		file := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".tsv")
		if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Trees(file, "python"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// Y9: the first line of a file, without its newline; none for an empty file.
func TestFirstLine(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{"a": "#!/usr/bin/env python3\nprint()\n", "b": "no newline", "c": "", "d": "\nrest"}
	want := map[string]string{"a": "#!/usr/bin/env python3", "b": "no newline", "c": "", "d": ""}
	for name, text := range cases {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := FirstLine(path)
		if err != nil || got != want[name] {
			t.Errorf("%s: %q %v", name, got, err)
		}
	}
	if _, err := FirstLine(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("a missing file was read")
	}
}
