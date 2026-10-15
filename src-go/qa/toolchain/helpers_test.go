package toolchain

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const (
	goodPins = "# pins\nRUFF=0.16.8\nSEMGREP=1.177.0\nSHELLCHECK=0.11.0\nGOLANGCI_LINT=2.14.0\nGO_QA_CI=803b122ce526\n"
	goodMod  = "module example.test/m\n\ngo 1.26\n"
	// goodQAMod is the nested module: the higher go directive, and the go-qa-ci require that
	// GO_QA_CI in goodPins pins.
	goodQAMod = "module example.test/m/qa\n\ngo 1.26.9\n\nrequire github.com/LongTermSupport/go-qa-ci v0.0.0-20261014082313-803b122ce526\n"
)

// reply is what a fake tool answers to one argv.
type reply struct {
	stdout, stderr string
	code           int
	timedOut       bool
	startErr       error
}

// world is a fake machine: files, the tools on PATH with their answers, the streams.
type world struct {
	files    map[string]string
	tools    map[string]string // name -> located path
	replies  map[string]reply  // "<name> <args...>" -> answer
	ran      []proc.Cmd
	out, err strings.Builder
}

// goodWorld is a machine that matches goodPins and goodMod exactly.
func goodWorld() *world {
	return &world{
		files: map[string]string{".qa-versions": goodPins, "src-go/go.mod": goodMod, "src-go/qa/go.mod": goodQAMod},
		tools: map[string]string{
			"ruff": "/bin/ruff", "semgrep": "/bin/semgrep", "shellcheck": "/bin/shellcheck",
			"golangci-lint": "/bin/golangci-lint", "go-qa": "/bin/go-qa", "go": "/bin/go",
		},
		replies: map[string]reply{
			"ruff --version":          {stdout: "ruff 0.16.8\n"},
			"semgrep --version":       {stdout: "1.177.0\n"},
			"shellcheck --version":    {stdout: "ShellCheck - shell script analysis tool\nversion: 0.11.0\nlicense: GNU General Public License, version 3\n"},
			"golangci-lint --version": {stdout: "golangci-lint has version 2.14.0 built with go1.26.2 from abc on 2026-01-01\n"},
			"go-qa --version":         {stdout: "v0.0.0-20261014082313-803b122ce526\n"},
			"go env GOVERSION":        {stdout: "go1.26.9\n"},
		},
	}
}

func (w *world) Run(_ context.Context, c proc.Cmd) (proc.Result, error) {
	w.ran = append(w.ran, c)
	name := c.Argv[0]
	for n, p := range w.tools {
		if p == name {
			name = n
		}
	}
	r, ok := w.replies[strings.Join(append([]string{name}, c.Argv[1:]...), " ")]
	if !ok {
		return proc.Result{}, errors.New("no such fake reply")
	}
	if r.startErr != nil {
		return proc.Result{}, r.startErr
	}
	return proc.Result{Stdout: r.stdout, Stderr: r.stderr, Code: r.code, TimedOut: r.timedOut}, nil
}

func (w *world) env() Env {
	mem := fstest.MapFS{}
	for p, text := range w.files {
		mem[p] = &fstest.MapFile{Data: []byte(text)}
	}
	return Env{
		Stdout:  &w.out,
		Stderr:  &w.err,
		Cwd:     "/repo",
		Resolve: func(p string) (string, error) { return p, nil },
		Open:    func(string) fs.FS { return mem },
		LookPath: func(name string) (string, error) {
			if p, ok := w.tools[name]; ok {
				return p, nil
			}
			return "", errors.New("not found")
		},
		Runner:  w,
		Timeout: time.Second,
	}
}

func (w *world) run(args ...string) ExitCode { return Main(args, w.env()) }

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed pipe") }

func symlink(target, link string) error { return os.Symlink(target, link) }

func mustContain(t *testing.T, what, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("%s lacks %q:\n%s", what, want, got)
		}
	}
}
