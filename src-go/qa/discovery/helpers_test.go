package discovery

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const defaultTrees = "# trees\nshell\t.git\nshell\tuntracked\nshell\t.semgrep\npython\t.venv\npython\tuntracked\n"

// world is a repository tree on disk plus a fake git that lists what the test says git tracks,
// would publish or ignores.
type world struct {
	t         *testing.T
	root      string
	tracked   []string
	untracked []string
	deleted   []string
	gitCode   int
	argvs     [][]string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{t: t, root: t.TempDir()}
	w.write("scripts/qa-trees.tsv", defaultTrees, false)
	return w
}

// Run implements proc.Runner as git ls-files over the world's lists.
func (w *world) Run(_ context.Context, c proc.Cmd) (proc.Result, error) {
	w.argvs = append(w.argvs, c.Argv)
	if len(c.Argv) < 3 || c.Argv[0] != "git" || c.Argv[1] != "ls-files" || c.Argv[2] != "-z" {
		return proc.Result{}, errors.New("unexpected command " + strings.Join(c.Argv, " "))
	}
	if w.gitCode != 0 {
		return proc.Result{Code: w.gitCode, Stderr: "fatal: not a git repository\n"}, nil
	}
	var files []string
	switch flags := strings.Join(c.Argv[3:], " "); flags {
	case "":
		files = w.tracked
	case "--cached --others --exclude-standard":
		files = append(slices.Clone(w.tracked), w.untracked...)
	case "--deleted":
		files = w.deleted
	default:
		return proc.Result{}, errors.New("unexpected flags " + flags)
	}
	var out strings.Builder
	for _, f := range files {
		out.WriteString(f + "\x00")
	}
	return proc.Result{Stdout: out.String()}, nil
}

// write puts a file in the tree; list is whether git would list it (tracked).
func (w *world) write(rel, text string, list bool) {
	w.t.Helper()
	path := filepath.Join(w.root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		w.t.Fatal(err)
	}
	if list {
		w.tracked = append(w.tracked, rel)
	}
}

// track writes a tracked file; fresh writes an untracked, not ignored one; ignore writes one git
// would never list.
func (w *world) track(rel, text string) { w.t.Helper(); w.write(rel, text, true) }

func (w *world) fresh(rel, text string) {
	w.t.Helper()
	w.write(rel, text, false)
	w.untracked = append(w.untracked, rel)
}

func (w *world) ignore(rel, text string) { w.t.Helper(); w.write(rel, text, false) }

func (w *world) link(rel, target string) {
	w.t.Helper()
	if err := os.Symlink(target, filepath.Join(w.root, rel)); err != nil {
		w.t.Fatal(err)
	}
	w.tracked = append(w.tracked, rel)
}

// result is one run of Main.
type result struct {
	code           ExitCode
	stdout, stderr string
}

// paths is the NUL-separated payload as a list.
func (r result) paths() []string {
	var out []string
	for p := range strings.SplitSeq(r.stdout, "\x00") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (w *world) main(args ...string) result {
	w.t.Helper()
	var stdout, stderr bytes.Buffer
	code := Main(args, Env{Stdout: &stdout, Stderr: &stderr, Cwd: w.root, Runner: w})
	return result{code, stdout.String(), stderr.String()}
}

func wantExit(t *testing.T, got result, code ExitCode) {
	t.Helper()
	if got.code != code {
		t.Fatalf("exit %d, want %d\nstdout: %q\nstderr: %s", got.code, code, got.stdout, got.stderr)
	}
}

func wantStderr(t *testing.T, got result, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(got.stderr, p) {
			t.Fatalf("stderr lacks %q:\n%s", p, got.stderr)
		}
	}
}

func wantPaths(t *testing.T, got result, want ...string) {
	t.Helper()
	wantExit(t, got, ExitOK)
	if !slices.Equal(got.paths(), want) {
		t.Fatalf("paths\n got  %q\n want %q", got.paths(), want)
	}
}
