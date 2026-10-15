package helpertests

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const testToken = "tok-1"

// world is a fake machine: a tree of files, what git tracks, and what the runner answers.
type world struct {
	t       *testing.T
	files   fstest.MapFS
	tracked []string
	// trackedErr is the discovery failure.
	trackedErr error
	// answer is called for the runner; nil answers exit 0 with counts that agree with the argv.
	answer func(w *world, c proc.Cmd) (proc.Result, error)
	// counts is written to the counts file by the default answer; "" writes the agreeing counts.
	counts string
	// code is the default answer's exit status.
	code int

	ran []proc.Cmd
	// bounded is that the runner's context had a deadline.
	bounded  bool
	out, err strings.Builder
	// countsSeen is the counts file's content when the runner started.
	countsSeen string
	countsPath string
}

// goodWorld holds two tracked tests, one in a subdirectory.
func goodWorld(t *testing.T) *world {
	t.Helper()
	w := &world{t: t, files: fstest.MapFS{}}
	w.add("tests/helpers/test_a.py", "x")
	w.add("tests/helpers/pkg/test_b.py", "x")
	w.add("tests/helpers/pkg/util.py", "not a test")
	w.tracked = []string{"tests/helpers/pkg/test_b.py", "tests/helpers/test_a.py"}
	return w
}

// Run implements proc.Runner.
func (w *world) Run(ctx context.Context, c proc.Cmd) (proc.Result, error) {
	w.ran = append(w.ran, c)
	_, w.bounded = ctx.Deadline()
	if path := argAfter(c.Argv, "--counts-file"); path != "" {
		w.countsPath = path
		if b, err := os.ReadFile(filepath.Clean(path)); err == nil {
			w.countsSeen = string(b)
		}
	}
	if w.answer != nil {
		return w.answer(w, c)
	}
	return w.defaultAnswer(c)
}

// defaultAnswer is a runner that ran every module it was given and passed (or exits w.code).
func (w *world) defaultAnswer(c proc.Cmd) (proc.Result, error) {
	body := w.counts
	if body == "" {
		body = w.agreeing(c)
	}
	if err := os.WriteFile(argAfter(c.Argv, "--counts-file"), []byte(body), 0o600); err != nil {
		w.t.Fatal(err)
	}
	return proc.Result{Code: w.code}, nil
}

func (w *world) add(path, text string) {
	w.files[path] = &fstest.MapFile{Data: []byte(text)}
}

// agreeing is the counts file a faithful runner writes for this argv.
func (w *world) agreeing(c proc.Cmd) string {
	var b strings.Builder
	if tok := argAfter(c.Argv, "--counts-token"); tok != "" {
		b.WriteString("token=" + tok + "\n")
	}
	modules := 0
	for i, a := range c.Argv {
		if strings.HasPrefix(a, "tests.helpers.") && i > 0 {
			modules++
		}
	}
	b.WriteString("tests=7\nskipped=1\nmodules=" + strconv.Itoa(modules) + "\ntracked=" + argAfter(c.Argv, "--tracked-modules") + "\n")
	return b.String()
}

func argAfter(argv []string, flag string) string {
	for i, a := range argv {
		if a == flag && i+1 < len(argv) {
			return argv[i+1]
		}
	}
	return ""
}

func (w *world) env() Env {
	return Env{
		Stdout:  &w.out,
		Stderr:  &w.err,
		Cwd:     "/repo",
		Resolve: func(p string) (string, error) { return p, nil },
		Open:    func(string) fs.FS { return w.files },
		Tracked: func(string) ([]string, error) { return w.tracked, w.trackedErr },
		Runner:  w,
	}
}

// run runs the gate with a counts file the test can read afterwards.
func (w *world) run(args ...string) ExitCode { return Main(args, w.env()) }

// runWith runs with --counts-file in a fresh temporary directory and the token.
func (w *world) runWith(extra ...string) ExitCode {
	w.t.Helper()
	counts := filepath.Join(w.t.TempDir(), "counts")
	args := append([]string{"--counts-file", counts, "--counts-token", testToken}, extra...)
	return w.run(args...)
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed pipe") }

var _ io.Writer = failWriter{}

func mustContain(t *testing.T, what, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("%s lacks %q:\n%s", what, want, got)
		}
	}
}

func mustNotContain(t *testing.T, what, got string, nots ...string) {
	t.Helper()
	for _, not := range nots {
		if strings.Contains(got, not) {
			t.Errorf("%s has %q:\n%s", what, not, got)
		}
	}
}
