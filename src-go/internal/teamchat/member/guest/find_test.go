package guest

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
)

// findWorld is a guest whose user `agent` has bash as its login shell in passwd, and claude and
// node installed under nvm, where only an interactive login shell finds them (R77).
type findWorld struct {
	home, passwd, shell, bin string
	sys                      *fakeSystem
	run                      *execx.Fake
}

func newFindWorld(t *testing.T) *findWorld {
	t.Helper()
	w := &findWorld{home: home(t)}
	w.sys = agentSystem(w.home)
	w.run = &execx.Fake{}
	w.shell = filepath.Join(t.TempDir(), "bash")
	executable(t, w.shell)
	w.bin = filepath.Join(w.home, ".nvm", "versions", "node", "v22.1.0", "bin")
	executable(t, filepath.Join(w.bin, "claude"))
	executable(t, filepath.Join(w.bin, "node"))
	w.passwd = filepath.Join(t.TempDir(), "passwd")
	w.writePasswd(t, w.shell)
	w.answer(shellAnswer(filepath.Join(w.bin, "claude"), filepath.Join(w.bin, "node")))
	return w
}

func (w *findWorld) writePasswd(t *testing.T, shell string) {
	t.Helper()
	text := "root:x:0:0:root:/root:/bin/bash\nagent:x:1000:1000::" + w.home + ":" + shell + "\n"
	if err := os.WriteFile(w.passwd, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// shellAnswer is what the find script prints, after an rc file's own noise.
func shellAnswer(claude, node string) string {
	return "Welcome back\nnvm is loaded\n\n" + findBegin + "\nclaude=" + claude + "\nnode=" + node + "\n" + findEnd + "\n"
}

func (w *findWorld) answer(stdout string) {
	w.run.On([]string{w.shell}, execx.Result{Stdout: []byte(stdout)})
}

func (w *findWorld) finder() Shell {
	return Shell{Run: w.run, Passwd: w.passwd, SystemDirs: []string{"/usr/local/bin", "/usr/bin", "/bin"}}
}

func (w *findWorld) find(t *testing.T) (Found, error) {
	t.Helper()
	id, err := Establish(w.sys, agentUser(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := id.Close(); err != nil {
			t.Error(err)
		}
	}()
	return w.finder().Find(t.Context(), id)
}

// R77: the account's own login shell is asked, interactive and login, with a clean environment,
// no input and a session of its own; the answers are read between the markers.
func TestFindAsksTheLoginShell(t *testing.T) {
	w := newFindWorld(t)
	got, err := w.find(t)
	if err != nil {
		t.Fatal(err)
	}
	if got.Claude != filepath.Join(w.bin, "claude") || got.Node != filepath.Join(w.bin, "node") {
		t.Errorf("found %+v", got)
	}
	calls := w.run.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls %v", calls)
	}
	c := calls[0]
	if c.Name != w.shell || !slices.Equal(c.Args[:3], []string{"-l", "-i", "-c"}) || len(c.Args) != 4 || c.Args[3] != findScript {
		t.Errorf("ran %s", c.Line())
	}
	if !c.CleanEnv || !c.NewSession || c.Stdin == nil || c.Stdout == nil {
		t.Errorf("not clean, not its own session, or no stdin or stdout: %+v", c)
	}
	for _, want := range []string{"HOME=" + w.home, "USER=agent", "LOGNAME=agent", "SHELL=" + w.shell, "PATH=" + w.home + "/.local/bin:/usr/local/bin:/usr/bin:/bin"} {
		if !slices.Contains(c.Env, want) {
			t.Errorf("env %v lacks %s", c.Env, want)
		}
	}
}

// A claude with no node beside it (Claude Code's native build) is found alone.
func TestFindWithoutNode(t *testing.T) {
	w := newFindWorld(t)
	w.run = &execx.Fake{}
	w.answer(shellAnswer(filepath.Join(w.bin, "claude"), ""))
	got, err := w.find(t)
	if err != nil || got.Claude != filepath.Join(w.bin, "claude") || got.Node != "" {
		t.Fatalf("%+v %v", got, err)
	}
}

// R77: only what the lookup cannot stand behind is refused, each by 78 claude, naming it.
func TestFindRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		plant func(t *testing.T, w *findWorld)
		says  string
	}{
		"no claude found": {func(t *testing.T, w *findWorld) {
			t.Helper()
			w.answer(shellAnswer("", ""))
		}, "found no claude"},
		"an alias": {func(t *testing.T, w *findWorld) {
			t.Helper()
			w.answer(shellAnswer("alias claude='npx claude'", ""))
		}, "answered ?"},
		"a relative path": {func(t *testing.T, w *findWorld) {
			t.Helper()
			w.answer(shellAnswer("bin/claude", ""))
		}, "answered bin/claude"},
		"another name": {func(t *testing.T, w *findWorld) {
			t.Helper()
			w.answer(shellAnswer(filepath.Join(w.bin, "node"), ""))
		}, "not a clean absolute path"},
		"an unclean path": {func(t *testing.T, w *findWorld) {
			t.Helper()
			w.answer(shellAnswer(w.bin+"/../bin/claude", ""))
		}, "not a clean absolute path"},
		"a node that is not executable": {func(t *testing.T, w *findWorld) {
			t.Helper()
			if err := os.Chmod(filepath.Join(w.bin, "node"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "command -v node"},
		"a claude that is a directory": {func(t *testing.T, w *findWorld) {
			t.Helper()
			dir := filepath.Join(w.home, "claude")
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			w.answer(shellAnswer(dir, ""))
		}, "executable file"},
		"no markers": {func(t *testing.T, w *findWorld) {
			t.Helper()
			w.answer("bash: no job control\n")
		}, "between its markers"},
		"cut short": {func(t *testing.T, w *findWorld) {
			t.Helper()
			w.answer(findBegin + "\nclaude=/x/claude\n")
		}, "between its markers"},
		"the shell fails": {func(t *testing.T, w *findWorld) {
			t.Helper()
			w.run.On([]string{w.shell}, execx.Result{ExitCode: 1})
		}, "exited 1"},
		"the shell cannot run": {func(t *testing.T, w *findWorld) {
			t.Helper()
			w.run.OnError([]string{w.shell}, errors.New("exec format error"))
		}, "could not be run"},
		"no passwd entry": {func(t *testing.T, w *findWorld) {
			t.Helper()
			if err := os.WriteFile(w.passwd, []byte("root:x:0:0:root:/root:/bin/bash\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "login shell"},
		"a shell that is not executable": {func(t *testing.T, w *findWorld) {
			t.Helper()
			if err := os.Chmod(w.shell, 0o644); err != nil {
				t.Fatal(err)
			}
		}, "login shell"},
		"a relative shell": {func(t *testing.T, w *findWorld) {
			t.Helper()
			w.writePasswd(t, "bash")
		}, "login shell"},
		"no passwd": {func(t *testing.T, w *findWorld) {
			t.Helper()
			w.passwd = filepath.Join(w.home, "absent")
		}, "login shell"},
	} {
		t.Run(name, func(t *testing.T) {
			w := newFindWorld(t)
			tc.plant(t, w)
			got, err := w.find(t)
			wantCode(t, err, 78, exits.CodeClaude)
			if got != (Found{}) || !strings.Contains(err.Error(), tc.says) {
				t.Errorf("found %+v, %v; want it to say %q", got, err, tc.says)
			}
		})
	}
}

// The op prints its answers as FOUND lines of its set, and nothing else.
func TestFoundPrintsOnlyItsSet(t *testing.T) {
	var out bytes.Buffer
	o := NewOutput(&out, OpFind)
	if err := errors.Join(o.Found(ProgramClaude, "/a/claude"), o.Found(ProgramNode, "/a/node")); err != nil {
		t.Fatal(err)
	}
	if out.String() != "FOUND\tclaude\t/a/claude\nFOUND\tnode\t/a/node\n" {
		t.Errorf("stdout %q", out.String())
	}
	defer func() {
		if recover() == nil {
			t.Error("a program outside the op's set was printed")
		}
	}()
	if err := NewOutput(&out, OpCheck).Found(ProgramClaude, "/a/claude"); err != nil {
		t.Error(err)
	}
}

func TestOpFindIsRegistered(t *testing.T) {
	for _, op := range Ops() {
		if op.Verb == "guest find-claude" {
			if op.Keyed || op.Busy || len(op.Changes) != 0 || !slices.Equal(op.Finds, []string{ProgramClaude, ProgramNode}) {
				t.Errorf("%+v", op)
			}
			return
		}
	}
	t.Error("guest find-claude is not registered")
}

func TestFindsOf(t *testing.T) {
	finds, ok := FindsOf("guest find-claude")
	if !ok || !slices.Equal(finds, []string{ProgramClaude, ProgramNode}) {
		t.Errorf("%v %v", finds, ok)
	}
	if _, ok := FindsOf("guest nothing"); ok {
		t.Error("an unknown verb has a set")
	}
}

func TestNewShell(t *testing.T) {
	s := NewShell()
	if _, ok := s.Run.(execx.OS); !ok || s.Passwd != "/etc/passwd" || !slices.Equal(s.SystemDirs, SystemDirs) {
		t.Errorf("%+v", s)
	}
}
