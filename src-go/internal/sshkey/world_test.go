package sshkey

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// testPid is the pid of the run in a test; testAgent the agent ssh-agent started for it.
const (
	testPid   = 4242
	testAgent = 4243
	testSelf  = "/opt/tools/the-tool"
	testSock  = "/run/agent.sock"
)

// world is one machine for a test: a temporary home with its ~/.ssh, the programs' answers,
// the lines typed at the terminal and the streams a run writes.
type world struct {
	t      *testing.T
	home   string
	fake   *execx.Fake
	fore   *foreground
	stdout bytes.Buffer
	stderr bytes.Buffer
	tty    string
	noTTY  bool
	env    map[string]string
	// parents maps a pid to its parent's, as /proc says.
	parents map[int]int
	// probeExtra are the identity files probeSays gave.
	probeExtra []string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{
		t:       t,
		home:    t.TempDir(),
		fake:    &execx.Fake{},
		env:     map[string]string{},
		parents: map[int]int{},
	}
	w.fore = &foreground{fake: w.fake, signals: map[string]syscall.Signal{}}
	w.env["HOME"] = w.home
	if err := os.Mkdir(w.sshDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	return w
}

func (w *world) sshDir() string { return filepath.Join(w.home, ".ssh") }

// privateKeyText is a private key file's text as far as the tool reads it.
func privateKeyText(kind string) string {
	return beginMark + kind + keyMark + "\nb3BlbnNzaC1rZXktdjEAAAAA\n-----END " + kind + keyMark + "\n"
}

// key writes a private key named name in ~/.ssh and returns its path.
func (w *world) key(name string) string {
	return w.file(name, privateKeyText("OPENSSH "))
}

// file writes text to name in ~/.ssh and returns its path.
func (w *world) file(name, text string) string {
	w.t.Helper()
	path := filepath.Join(w.sshDir(), name)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		w.t.Fatal(err)
	}
	return path
}

// probeSays makes every `ssh -G` answer with these identity files after the given key.
func (w *world) probeSays(extra ...string) {
	w.probeExtra = extra
	w.fake.OnFunc([]string{"ssh", "-G"}, func(c execx.Cmd) (execx.Result, error) {
		var out strings.Builder
		out.WriteString("host host.example.com\nuser someone\nhostname host.example.com\n")
		if i := slices.Index(c.Args, "-i"); i >= 0 {
			out.WriteString("identityfile " + c.Args[i+1] + "\n")
		}
		for _, e := range extra {
			out.WriteString("identityfile " + e + "\n")
		}
		return execx.Result{Stdout: []byte(out.String())}, nil
	})
}

// probeAlsoSays adds these lines to every `ssh -G` answer, after the identity files.
func (w *world) probeAlsoSays(lines ...string) {
	w.fake.OnFunc([]string{"ssh", "-G"}, func(c execx.Cmd) (execx.Result, error) {
		var out strings.Builder
		out.WriteString("host host.example.com\nuser someone\nhostname host.example.com\n")
		if i := slices.Index(c.Args, "-i"); i >= 0 {
			out.WriteString("identityfile " + c.Args[i+1] + "\n")
		}
		for _, e := range w.probeExtra {
			out.WriteString("identityfile " + e + "\n")
		}
		for _, l := range lines {
			out.WriteString(l + "\n")
		}
		return execx.Result{Stdout: []byte(out.String())}, nil
	})
}

// insideAgent makes the run the one ssh-agent started (E14).
func (w *world) insideAgent() {
	w.env[markerVar] = strconv.Itoa(testPid)
	w.env["SSH_AUTH_SOCK"] = testSock
	w.env["SSH_AGENT_PID"] = strconv.Itoa(testAgent)
	w.parents[testAgent] = testPid
	w.fake.On([]string{"ssh-agent", "-k"}, execx.Result{})
	for _, program := range []string{"ssh-add", "ssh", "scp"} {
		w.fake.On([]string{program}, execx.Result{})
	}
}

func (w *world) environment() Env {
	return Env{
		Runner: w.fake, Execer: w.fake, Foregrounder: w.fore,
		Stdout: &w.stdout, Stderr: &w.stderr,
		Getenv: func(k string) string { return w.env[k] },
		Getpid: func() int { return testPid },
		Self:   func() (string, error) { return testSelf, nil },
		OpenTTY: func() (io.ReadCloser, error) {
			if w.noTTY {
				return nil, errors.New("no such device or address")
			}
			return io.NopCloser(strings.NewReader(w.tty)), nil
		},
		ParentOf: func(pid int) (int, error) {
			p, ok := w.parents[pid]
			if !ok {
				return 0, errors.New("no such process")
			}
			return p, nil
		},
		IsTerminal: func(any) bool { return false },
		Columns:    func() int { return 0 },
	}
}

func (w *world) run(tool Tool, args ...string) term.ExitCode {
	return w.runCtx(context.Background(), tool, args...)
}

func (w *world) runCtx(ctx context.Context, tool Tool, args ...string) term.ExitCode {
	return App{Tool: tool, Env: w.environment()}.Run(ctx, args)
}

// execd is the one command the run replaced itself with.
func (w *world) execd() execx.Cmd {
	w.t.Helper()
	execs := w.fake.Execs()
	if len(execs) != 1 {
		w.t.Fatalf("want one exec, got %d (%v); stderr:\n%s", len(execs), execs, w.stderr.String())
	}
	return execs[0]
}

// noExec fails the test if the run replaced itself with anything.
func (w *world) noExec() {
	w.t.Helper()
	if execs := w.fake.Execs(); len(execs) != 0 {
		w.t.Fatalf("want no exec, got %v", execs)
	}
}

// ran is every argv run, exec'd or foregrounded, in order of kind.
func (w *world) ran() []string {
	lines := w.lines(w.fake.Calls(), "")
	lines = append(lines, w.lines(w.fake.Foregrounds(), "fg ")...)
	return append(lines, w.lines(w.fake.Execs(), "exec ")...)
}

// agentRun is what a run inside the agent did, in order: the foreground commands, then the
// agent's end (the only command it runs to completion).
func (w *world) agentRun() []string {
	return append(w.lines(w.fake.Foregrounds(), "fg "), w.lines(w.fake.Calls(), "")...)
}

func (w *world) lines(cmds []execx.Cmd, prefix string) []string {
	lines := make([]string, 0, len(cmds))
	for _, c := range cmds {
		lines = append(lines, prefix+c.Line())
	}
	return lines
}

func (w *world) stderrHas(want string) {
	w.t.Helper()
	if !strings.Contains(w.stderr.String(), want) {
		w.t.Errorf("stderr lacks %q:\n%s", want, w.stderr.String())
	}
}

func wantCode(t *testing.T, got, want term.ExitCode) {
	t.Helper()
	if got != want {
		t.Fatalf("exit %d, want %d", got, want)
	}
}

// foreground is the fake's Foreground with a signal a test can say killed a command.
type foreground struct {
	fake *execx.Fake
	// signals maps a command's first argv word to the signal that ends it.
	signals map[string]syscall.Signal
}

func (f *foreground) Foreground(c execx.Cmd) (execx.Exit, error) {
	exit, err := f.fake.Foreground(c)
	if sig, ok := f.signals[c.Name]; ok && err == nil {
		return execx.Exit{Code: 128 + int(sig), Signal: sig, Received: []os.Signal{sig}}, nil
	}
	return exit, err
}
