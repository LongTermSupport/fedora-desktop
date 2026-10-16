package sessions

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sessprobe"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

// harness is an App wired to a Fake runner, buffers for its streams, a clock that only moves
// when the App pauses, and a scratch directory standing in for the host's files.
type harness struct {
	t      *testing.T
	app    *App
	fake   *execx.Fake
	out    bytes.Buffer
	errOut bytes.Buffer
	env    map[string]string
	root   string
	clock  time.Time
	// pauses is every wait the App asked for, in order. interruptAt, when set, makes the n-th
	// pause (1-based) come back interrupted by a signal instead of elapsing.
	pauses      []time.Duration
	interruptAt int
	tty         bool
	signals     chan os.Signal
	// lxcInstalled puts the LXC tools on PATH; without it this host has no LXC.
	lxcInstalled bool
	// p is the pair of projects a test scripted, when it did.
	p pair
}

// sendSignal queues a TERM for the App, as the system does; a second one while the first is
// still waiting is dropped, as it would be.
func (h *harness) sendSignal() {
	select {
	case h.signals <- syscall.SIGTERM:
	default:
	}
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	lib := filepath.Join(root, "lib")
	if err := os.MkdirAll(lib, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lib, "tmux-session.bash"), []byte("#"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &harness{
		t: t, fake: &execx.Fake{}, root: root, clock: time.Unix(1_000_000, 0),
		signals: make(chan os.Signal, 1),
	}
	h.env = map[string]string{
		"CCY_LIB": lib, "HOME": filepath.Join(root, "home"), "PWD": filepath.Join(root, "home", "proj"),
		"CCY_STATE_DIR": filepath.Join(root, "state"), "CCY_BOOT_ID_FILE": filepath.Join(root, "boot_id"),
		"CCY_SESSIONS_POLL_SECONDS": "5", "CCY_SESSIONS_MINUTE_SECONDS": "60",
		"CCY_SESSIONS_SUBMIT_DELAY": "0.2", "NO_COLOR": "1",
	}
	if err := os.WriteFile(h.env["CCY_BOOT_ID_FILE"], []byte("this-boot\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.app = &App{
		Runner: h.fake, Execer: h.fake, Stdout: &h.out, Stderr: &h.errOut, Stdin: strings.NewReader(""),
		Getenv:      func(k string) string { return h.env[k] },
		Unsetenv:    func(k string) error { delete(h.env, k); return nil },
		Now:         func() time.Time { return h.clock },
		Pause:       h.pause,
		Signals:     func() (<-chan os.Signal, func()) { return h.signals, func() {} },
		IsTerminal:  func(any) bool { return h.tty },
		InContainer: func() bool { return false },
		Executable:  isExecutable,
		Getuid:      os.Getuid,
		LookPath: func(name string) (string, error) {
			if h.lxcInstalled {
				return "/usr/bin/" + name, nil
			}
			return "", exec.ErrNotFound
		},
	}
	h.app.wire()
	h.app.Probe = &sessprobe.Probe{
		Tmux: &tmuxccy.Tmux{Runner: h.fake, Term: h.app.Term}, Runner: h.fake, ProcRoot: filepath.Join(root, "proc"),
		Now: h.step, Sleep: func(time.Duration) {}, ClockTicks: 100,
	}
	h.app.Tmux.Sleep = func(time.Duration) {}
	h.app.Tmux.Now = func() time.Time { return h.clock }
	return h
}

// step is a clock that moves one second every time it is read, for the CPU sample.
func (h *harness) step() time.Time {
	h.clock = h.clock.Add(time.Second)
	return h.clock
}

// pause records the wait and moves the clock on, unless it is the one told to be interrupted.
func (h *harness) pause(d time.Duration, sig <-chan os.Signal) bool {
	h.pauses = append(h.pauses, d)
	if h.interruptAt > 0 && len(h.pauses) == h.interruptAt {
		return true
	}
	if sig != nil {
		select {
		case <-sig:
			return true
		default:
		}
	}
	h.clock = h.clock.Add(d)
	return false
}

// run is the command with args, as its exit code.
func (h *harness) run(args ...string) term.ExitCode {
	h.t.Helper()
	return h.app.Run(args)
}

func (h *harness) stdout() string { return h.out.String() }
func (h *harness) stderr() string { return h.errOut.String() }

// tmuxOut answers a tmux subcommand with stdout.
func (h *harness) tmuxOut(sub, stdout string) {
	h.fake.On([]string{"tmux", "-L", "ccy", sub}, execx.Result{Stdout: []byte(stdout)})
}

// tmuxFail makes a tmux subcommand exit 1 with the message on stderr.
func (h *harness) tmuxFail(sub, message string) {
	h.fake.On([]string{"tmux", "-L", "ccy", sub}, execx.Result{ExitCode: 1, Stderr: []byte(message)})
}

// sessions sets the live sessions, each "name attached dir".
func (h *harness) sessions(lines ...string) {
	h.tmuxOut("list-sessions", strings.Join(lines, "\n")+"\n")
}

// ran is every command run so far, one argv per line.
func (h *harness) ran() []string {
	calls := h.fake.Calls()
	lines := make([]string, 0, len(calls))
	for _, c := range calls {
		lines = append(lines, c.Line())
	}
	return lines
}

// count is how many commands run so far start with prefix.
func (h *harness) count(prefix string) int {
	n := 0
	for _, line := range h.ran() {
		if strings.HasPrefix(line, prefix) {
			n++
		}
	}
	return n
}

// project makes a directory under the scratch root holding an executable hooks-daemon CLI
// (when withCLI) and returns its path.
func (h *harness) project(name string, withCLI bool) string {
	h.t.Helper()
	dir := filepath.Join(h.root, name)
	bin := filepath.Join(dir, ".claude", "hooks-daemon", "bin")
	if err := os.MkdirAll(bin, 0o750); err != nil {
		h.t.Fatal(err)
	}
	if withCLI {
		path := filepath.Join(bin, "hooks-daemon")
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o600); err != nil {
			h.t.Fatal(err)
		}
		chmod(h.t, path, 0o700)
	}
	return dir
}

// cli is the daemon CLI path of a project.
func cli(dir string) string { return dir + "/.claude/hooks-daemon/bin/hooks-daemon" }

// onCLI answers a project's host-side daemon CLI.
func (h *harness) onCLI(dir string, fn func(execx.Cmd) execx.Result) {
	h.fake.OnFunc([]string{cli(dir)}, func(c execx.Cmd) (execx.Result, error) { return fn(c), nil })
}

// okCLI makes every call to the project's daemon CLI succeed silently.
func (h *harness) okCLI(dir string) {
	h.onCLI(dir, func(execx.Cmd) execx.Result { return execx.Result{} })
}

// write puts a file under the scratch root.
func (h *harness) write(rel, content string) string {
	h.t.Helper()
	path := filepath.Join(h.root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		h.t.Fatal(err)
	}
	return path
}

// chmod sets a file's mode. The tests need executable fixtures, and loose modes to be refused.
func chmod(t *testing.T, path string, mode uint32) {
	t.Helper()
	if err := syscall.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func contains(t *testing.T, label, haystack string, needles ...string) {
	t.Helper()
	for _, n := range needles {
		if !strings.Contains(haystack, n) {
			t.Fatalf("%s: want %q in:\n%s", label, n, haystack)
		}
	}
}

func lacks(t *testing.T, label, haystack string, needles ...string) {
	t.Helper()
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			t.Fatalf("%s: did not want %q in:\n%s", label, n, haystack)
		}
	}
}

func exitCodeIs(t *testing.T, got, want term.ExitCode, h *harness) {
	t.Helper()
	if got != want {
		t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", got, want, h.stdout(), h.stderr())
	}
}

var errBoom = errors.New("boom")

func TestCmdLineForTheFakeIsSpaceJoined(t *testing.T) {
	h := newHarness(t)
	h.tmuxOut("list-sessions", "")
	if _, err := h.fake.Run(context.Background(), execx.Cmd{Name: "tmux", Args: []string{"-L", "ccy", "list-sessions"}}); err != nil {
		t.Fatal(err)
	}
	if got := h.ran(); len(got) != 1 || got[0] != "tmux -L ccy list-sessions" || h.count("tmux") != 1 {
		t.Fatalf("ran %v", got)
	}
}
