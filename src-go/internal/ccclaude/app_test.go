package ccclaude

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

type world struct {
	fake   *execx.Fake
	stderr bytes.Buffer
	env    map[string]string
}

func newWorld() *world {
	return &world{fake: &execx.Fake{}, env: map[string]string{}}
}

func (w *world) run(args ...string) term.ExitCode {
	app := App{
		Execer: w.fake,
		Stderr: &w.stderr,
		Getenv: func(k string) string { return w.env[k] },
	}
	return app.Run(args)
}

func (w *world) only(t *testing.T) execx.Cmd {
	t.Helper()
	execs := w.fake.Execs()
	if len(execs) != 1 {
		t.Fatalf("want one exec, got %d: %v", len(execs), execs)
	}
	return execs[0]
}

// C1.
func TestExecsClaudeWithEveryArgument(t *testing.T) {
	w := newWorld()
	if code := w.run("--resume", "abc", "--", "-x"); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.stderr.String())
	}
	c := w.only(t)
	if c.Name != "claude" || !slices.Equal(c.Args, []string{"--resume", "abc", "--", "-x"}) {
		t.Errorf("exec'd %q", c.Line())
	}
	if w.stderr.Len() != 0 {
		t.Errorf("stderr %q", w.stderr.String())
	}
}

// C2.
func TestUnsetWindowGetsTheDefault(t *testing.T) {
	for _, set := range []bool{false, true} {
		w := newWorld()
		if set {
			w.env[WindowVar] = ""
		}
		w.run()
		c := w.only(t)
		want := WindowVar + "=" + DefaultWindow
		if !slices.Equal(c.Env, []string{want}) {
			t.Errorf("empty set=%v: env %v, want [%s]", set, c.Env, want)
		}
	}
}

// C3.
func TestAWindowAlreadySetIsKept(t *testing.T) {
	w := newWorld()
	w.env[WindowVar] = "250000"
	w.run()
	if c := w.only(t); len(c.Env) != 0 {
		t.Errorf("env %v, want the inherited value untouched", c.Env)
	}
}

// C4.
func TestClaudeMissingFails(t *testing.T) {
	w := newWorld()
	w.fake.ExecError(&execx.NotInstalledError{Name: "claude"})
	if code := w.run(); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.stderr.String(), "cc-claude: claude is not installed") {
		t.Errorf("stderr %q", w.stderr.String())
	}
}

// C4.
func TestExecFailingOtherwiseFails(t *testing.T) {
	w := newWorld()
	w.fake.ExecError(errors.New("exec claude: permission denied"))
	if code := w.run(); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.stderr.String(), "cc-claude: cannot start claude: exec claude: permission denied") {
		t.Errorf("stderr %q", w.stderr.String())
	}
}

// C5: ccy passes the same default into its container, so cc and ccy sessions compact at the
// same size. The bash launcher holds its copy in the podman argv; this pins the two together.
func TestDefaultIsTheCcyLaunchersDefault(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := fs.ReadFile(os.DirFS(root), "files/var/local/claude-yolo/claude-yolo")
	if err != nil {
		t.Fatal(err)
	}
	line := regexp.MustCompile(`-e "` + WindowVar + `=\$\{` + WindowVar + `:-([0-9]+)\}"`)
	found := line.FindAllStringSubmatch(string(raw), -1)
	if len(found) != 1 {
		t.Fatalf("the ccy launcher has %d %s defaults, want 1", len(found), WindowVar)
	}
	if found[0][1] != DefaultWindow {
		t.Errorf("ccy defaults %s to %s, cc-claude to %s", WindowVar, found[0][1], DefaultWindow)
	}
}
