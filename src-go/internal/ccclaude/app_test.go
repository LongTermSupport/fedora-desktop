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

// C2: each default is set when its variable is unset or empty, all of them in one exec.
func TestUnsetVariablesGetTheirDefaults(t *testing.T) {
	for _, set := range []bool{false, true} {
		w := newWorld()
		if set {
			for _, d := range Defaults {
				w.env[d.Name] = ""
			}
		}
		w.run()
		c := w.only(t)
		want := make([]string, 0, len(Defaults))
		for _, d := range Defaults {
			want = append(want, d.Name+"="+d.Value)
		}
		if !slices.Equal(c.Env, want) {
			t.Errorf("empty set=%v: env %v, want %v", set, c.Env, want)
		}
	}
}

// C2: the two defaults ccy and cc share.
func TestTheDefaultsAreTheWindowAndMouseClicks(t *testing.T) {
	want := []Default{{WindowVar, DefaultWindow}, {MouseClicksVar, "1"}}
	if !slices.Equal(Defaults, want) {
		t.Errorf("Defaults %v, want %v", Defaults, want)
	}
}

// C3.
func TestAValueAlreadySetIsKept(t *testing.T) {
	for _, d := range Defaults {
		w := newWorld()
		w.env[d.Name] = "owner-value"
		w.run()
		for _, e := range w.only(t).Env {
			if strings.HasPrefix(e, d.Name+"=") {
				t.Errorf("%s set by the owner, but cc-claude passed %s", d.Name, e)
			}
		}
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

// C5: ccy passes the same defaults into its container, so cc and ccy sessions behave alike.
// The bash launcher holds its copies in the podman argv; this pins the two together.
func TestDefaultsAreTheCcyLaunchersDefaults(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := fs.ReadFile(os.DirFS(root), "files/var/local/claude-yolo/claude-yolo")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range Defaults {
		line := regexp.MustCompile(`-e "` + d.Name + `=\$\{` + d.Name + `:-([^}]+)\}"`)
		found := line.FindAllStringSubmatch(string(raw), -1)
		if len(found) != 1 {
			t.Errorf("the ccy launcher has %d %s defaults, want 1", len(found), d.Name)
			continue
		}
		if found[0][1] != d.Value {
			t.Errorf("ccy defaults %s to %s, cc-claude to %s", d.Name, found[0][1], d.Value)
		}
	}
}

// C6: the ccy launcher always sets the even-more-dangerous bypass variable, because the
// container is the isolation boundary; host cc has no such boundary and never sets it.
func TestYoloVariableIsCcyOnly(t *testing.T) {
	const yoloVar = "CLAUDE_CODE_EVEN_MORE_DANGEROUSLY_SKIP_PERMISSIONS"
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := fs.ReadFile(os.DirFS(root), "files/var/local/claude-yolo/claude-yolo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `-e "`+yoloVar+`=1"`) {
		t.Errorf("the ccy launcher does not pass -e %s=1 to the container", yoloVar)
	}
	w := newWorld()
	w.run("--print")
	for _, e := range w.only(t).Env {
		if strings.HasPrefix(e, yoloVar) {
			t.Errorf("cc-claude sets %s: %s", yoloVar, e)
		}
	}
}

// C7 (Task 5.1 H5): a first argument -- is the wrapper form teamchat run needs, which finds
// claude by an argument named claude: -- must be followed by claude, and the arguments after
// that claude go to claude, with C2's defaults as in C1.
func TestTheWrapperFormExecsClaudeWithTheRest(t *testing.T) {
	w := newWorld()
	if code := w.run("--", "claude", "--resume", "abc", "--", "-x"); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.stderr.String())
	}
	c := w.only(t)
	if c.Name != "claude" || !slices.Equal(c.Args, []string{"--resume", "abc", "--", "-x"}) {
		t.Errorf("exec'd %q", c.Line())
	}
	if len(c.Env) != len(Defaults) || w.stderr.Len() != 0 {
		t.Errorf("env %q, stderr %q", c.Env, w.stderr.String())
	}
}

// C7: a first -- followed by anything but claude is not the wrapper form: every argument goes to
// claude unchanged, as in C1, so `cc -- <prompt>` still reaches claude as `claude -- <prompt>`.
func TestAFirstSeparatorWithoutClaudeGoesToClaudeUnchanged(t *testing.T) {
	for _, args := range [][]string{{"--"}, {"--", "fix the build"}, {"--", "--model"}, {"--", "/usr/bin/claude"}, {"--", "claudex"}, {"--", "--", "claude"}} {
		w := newWorld()
		if code := w.run(args...); code != term.ExitOK {
			t.Errorf("%q: exit %d, stderr %q", args, code, w.stderr.String())
			continue
		}
		c := w.only(t)
		if c.Name != "claude" || !slices.Equal(c.Args, args) || w.stderr.Len() != 0 {
			t.Errorf("%q: exec'd %q, stderr %q", args, c.Line(), w.stderr.String())
		}
	}
}
