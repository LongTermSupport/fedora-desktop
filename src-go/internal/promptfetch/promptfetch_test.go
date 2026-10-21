package promptfetch

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

var configGet = []string{"git", "config", "--get", "core.sshCommand"}

var fetchArgv = []string{"git", "-c", "core.askPass=", "fetch", "--quiet"}

// notSet is what envValue gives for a variable the command does not set.
const notSet = "<not set>"

// world is a fake machine: the environment the command sees and the runner it calls.
type world struct {
	env  map[string]string
	fake *execx.Fake
}

func newWorld(env map[string]string) *world {
	w := &world{env: env, fake: &execx.Fake{}}
	w.fake.On(configGet, execx.Result{ExitCode: 1})
	w.fake.On(fetchArgv, execx.Result{})
	return w
}

func (w *world) run(t *testing.T, args []string) (term.ExitCode, string) {
	t.Helper()
	var errOut bytes.Buffer
	app := App{
		Dir:    "/work/repo",
		Getenv: func(k string) string { return w.env[k] },
		Runner: w.fake,
		Stderr: &errOut,
	}
	return app.Run(context.Background(), args), errOut.String()
}

func (w *world) fetch(t *testing.T) execx.Cmd {
	t.Helper()
	for _, c := range w.fake.Calls() {
		if slices.Equal(append([]string{c.Name}, c.Args...), fetchArgv) {
			return c
		}
	}
	t.Fatalf("no fetch run; calls: %v", w.fake.Calls())
	return execx.Cmd{}
}

func envValue(c execx.Cmd, key string) string {
	for _, kv := range c.Env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			return v
		}
	}
	return notSet
}

// F1: the fetch is git fetch --quiet in the checkout, with no askpass program and stdin from
// /dev/null (a nil Stdin), so nothing can be typed into it.
func TestFetchRunsInTheCheckoutWithNothingToReadFrom(t *testing.T) {
	w := newWorld(nil)
	if code, errOut := w.run(t, nil); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	c := w.fetch(t)
	if c.Dir != "/work/repo" || c.Stdin != nil {
		t.Fatalf("dir %q stdin %v", c.Dir, c.Stdin)
	}
}

// F2: no prompt of any kind: no terminal prompt, no askpass window, no ssh passphrase or
// host-key question.
func TestFetchCanAskForNothing(t *testing.T) {
	w := newWorld(nil)
	if code, errOut := w.run(t, nil); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	c := w.fetch(t)
	for key, want := range map[string]string{
		"GIT_TERMINAL_PROMPT": "0",
		"SSH_ASKPASS_REQUIRE": "never",
		"GIT_SSH_COMMAND":     "ssh -o BatchMode=yes",
	} {
		if got := envValue(c, key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	for _, name := range []string{"GIT_ASKPASS", "SSH_ASKPASS"} {
		if !slices.Contains(c.Unset, name) {
			t.Errorf("%s not unset: %v", name, c.Unset)
		}
	}
}

// F3: BatchMode is added to the ssh command git would have used, never in its place: the
// GIT_SSH_COMMAND of the environment, else the repository's core.sshCommand (a per-account
// key), else GIT_SSH as one quoted word, else ssh.
func TestBatchModeIsAddedToTheSshCommandGitWouldUse(t *testing.T) {
	cases := []struct {
		name   string
		env    map[string]string
		config string
		want   string
	}{
		{"environment", map[string]string{"GIT_SSH_COMMAND": "ssh -i ~/.ssh/k", "GIT_SSH": "/x"}, "ssh -F cfg", "ssh -i ~/.ssh/k -o BatchMode=yes"},
		{"repository config", nil, "ssh -i '/home/u/.ssh/a key' -F /dev/null", "ssh -i '/home/u/.ssh/a key' -F /dev/null -o BatchMode=yes"},
		{"GIT_SSH program", map[string]string{"GIT_SSH": "/opt/my ssh"}, "", "'/opt/my ssh' -o BatchMode=yes"},
		{"plain ssh", nil, "", "ssh -o BatchMode=yes"},
	}
	for _, c := range cases {
		w := newWorld(c.env)
		if c.config != "" {
			w.fake.On(configGet, execx.Result{Stdout: []byte(c.config + "\n")})
		}
		if code, errOut := w.run(t, nil); code != term.ExitOK {
			t.Fatalf("%s: exit %d, stderr %q", c.name, code, errOut)
		}
		if got := envValue(w.fetch(t), "GIT_SSH_COMMAND"); got != c.want {
			t.Errorf("%s: GIT_SSH_COMMAND %q, want %q", c.name, got, c.want)
		}
	}
}

// F4: a core.sshCommand lookup that fails other than "not set" (exit 1) is an error, and
// nothing is fetched with a guessed ssh command.
func TestConfigLookupFailureFetchesNothing(t *testing.T) {
	w := newWorld(nil)
	w.fake.On(configGet, execx.Result{ExitCode: 128, Stderr: []byte("fatal: bad config\n")})
	code, errOut := w.run(t, nil)
	if code != term.ExitFailure || !strings.HasPrefix(errOut, "git-prompt-fetch: ") {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
	if w.fake.Called("git -c") {
		t.Fatal("fetched after a failed lookup")
	}
	w = newWorld(nil)
	w.fake.OnFunc(configGet, func(execx.Cmd) (execx.Result, error) { return execx.Result{}, errors.New("no git") })
	if code, errOut := w.run(t, nil); code != term.ExitFailure || errOut == "" {
		t.Fatalf("git not startable: exit %d stderr %q", code, errOut)
	}
}

// F5: a fetch that fails is exit 1 with git's words on stderr.
func TestFailedFetchIsExitOne(t *testing.T) {
	w := newWorld(nil)
	w.fake.On(fetchArgv, execx.Result{ExitCode: 128, Stderr: []byte("Permission denied (publickey).\n")})
	code, errOut := w.run(t, nil)
	if code != term.ExitFailure || !strings.Contains(errOut, "Permission denied") {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
}

// F6: the command takes no arguments.
func TestArgumentsAreAUsageError(t *testing.T) {
	w := newWorld(nil)
	if code, errOut := w.run(t, []string{"origin"}); code != term.ExitUsage || errOut == "" {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
	if len(w.fake.Calls()) != 0 {
		t.Fatalf("ran %v", w.fake.Calls())
	}
}
