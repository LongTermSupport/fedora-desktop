package statusline

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/colour"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	runID  = "00112233445566ff"
	wall0  = int64(1_700_000_000_000)
	boot0  = 5 * time.Second
	redOn  = "\x1b[31m"
	resetC = "\x1b[0m"
)

// world is one session's run directory, its settings files and the status line's seams.
type world struct {
	t       *testing.T
	base    string
	project string
	run     paths.Run
	root    *homefs.Root
	env     map[string]string
	clk     *clock.Fake
	runner  execx.Runner
	signals chan os.Signal
	stdin   io.Reader
	out     bytes.Buffer
	stderr  syncBuffer
}

func newWorld(t *testing.T) *world {
	t.Helper()
	base := t.TempDir()
	home, xdg := filepath.Join(base, "h"), filepath.Join(base, "x")
	layout, err := paths.HomeLayout(home, xdg)
	if err != nil {
		t.Fatal(err)
	}
	id, err := paths.ParseRunID(runID)
	if err != nil {
		t.Fatal(err)
	}
	run := layout.Run(id)
	if err = os.MkdirAll(run.Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := homefs.Open(run.Dir(), homefs.Private)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cerr := root.Close(); cerr != nil {
			t.Error(cerr)
		}
	})
	project := filepath.Join(base, "project")
	if err = os.MkdirAll(filepath.Join(project, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	return &world{
		t: t, base: base, project: project, run: run, root: root,
		env: map[string]string{
			"HOME": home, "XDG_RUNTIME_DIR": xdg, paths.EnvRuntime: run.Dir(),
			"CLAUDE_CONFIG_DIR": filepath.Join(base, "config"),
		},
		clk:    clock.NewFake(boot0, wall0),
		runner: execx.OS{Grace: 200 * time.Millisecond},
		stdin:  strings.NewReader(`{"cwd":"` + project + `"}`),
	}
}

// syncBuffer is a stderr the child's copy and the test read at once.
type syncBuffer struct {
	mu   sync.Mutex
	b    bytes.Buffer
	seen chan struct{}
	want string
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.b.Write(p)
	if s.seen != nil && s.want != "" && strings.Contains(s.b.String(), s.want) {
		close(s.seen)
		s.seen = nil
	}
	return n, err
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// await is a channel closed once stderr holds text.
func (s *syncBuffer) await(text string) <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan struct{})
	s.seen, s.want = ch, text
	return ch
}

func (w *world) cliEnv() cli.Env {
	return cli.Env{Stdin: w.stdin, Stdout: &w.out, Stderr: &w.stderr, Getenv: func(k string) string { return w.env[k] }}
}

func (w *world) deps() deps {
	return deps{clock: w.clk, runner: w.runner, signals: w.signals}
}

// render runs the status line and is its exit and stdout.
func (w *world) render() (term.ExitCode, string) {
	w.t.Helper()
	code := run(w.cliEnv(), w.deps())
	return code, w.out.String()
}

// async runs the status line on its own goroutine.
func (w *world) async() <-chan term.ExitCode {
	e, d := w.cliEnv(), w.deps()
	done := make(chan term.ExitCode, 1)
	go func() { done <- run(e, d) }()
	return done
}

func (w *world) noInner() { w.env[paths.EnvStatusLineInner] = "1" }

func (w *world) write(path, content string) {
	w.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		w.t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		w.t.Fatal(err)
	}
	_, werr := f.WriteString(content)
	if err := errors.Join(werr, f.Close()); err != nil {
		w.t.Fatal(err)
	}
}

// settings writes a settings file whose statusLine runs command.
func (w *world) settings(path, command string) {
	w.t.Helper()
	w.write(path, `{"statusLine":{"type":"command","command":`+string(proto.Str(command).Compact())+`}}`)
}

func (w *world) projectSettings(command string) {
	w.settings(filepath.Join(w.project, ".claude", "settings.json"), command)
}

func (w *world) holdLink() {
	w.t.Helper()
	l, err := lock.TakeLink(w.t.Context(), lock.SystemClock(), w.root, w.run)
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() {
		if err := l.Release(); err != nil && !errors.Is(err, lock.ErrReleased) {
			w.t.Error(err)
		}
	})
}

func (w *world) holdKeeper() {
	w.t.Helper()
	k, err := lock.TakeKeeper(w.t.Context(), lock.SystemClock(), w.root, w.run)
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() {
		if err := k.Release(); err != nil && !errors.Is(err, lock.ErrReleased) {
			w.t.Error(err)
		}
	})
}

func (w *world) keeperJSON(link, kind string, nextMS *int64) {
	w.t.Helper()
	id, err := paths.ParseRunID(runID)
	if err != nil {
		w.t.Fatal(err)
	}
	k := state.Keeper{V: state.Version, Run: id, Link: link, NextMS: nextMS, Kind: kind, Desired: state.DesiredApplied}
	if link == state.LinkFailed {
		k.Why = new(state.WhyCrashLoop)
	}
	if kind == state.KindCCY || kind == state.KindLXC {
		s, err := paths.ParseSessionName("work-1")
		if err != nil {
			w.t.Fatal(err)
		}
		k.Session = &s
	}
	if err := state.Write(w.root, w.run.KeeperJSON(), k); err != nil {
		w.t.Fatal(err)
	}
}

// status writes status.json with the teams, written now.
func (w *world) status(desired string, unusable int64, teams ...state.StatusTeam) {
	w.t.Helper()
	w.statusAt(wall0, desired, unusable, teams...)
}

func (w *world) statusAt(writtenMS int64, desired string, unusable int64, teams ...state.StatusTeam) {
	w.t.Helper()
	id, err := paths.ParseRunID(runID)
	if err != nil {
		w.t.Fatal(err)
	}
	if teams == nil {
		teams = []state.StatusTeam{}
	}
	st := state.Status{Run: id, WrittenMS: writtenMS, Desired: desired, UnusableHookStdin: unusable, Teams: teams}
	if err := state.Write(w.root, w.run.Status(), st); err != nil {
		w.t.Fatal(err)
	}
}

func teamID(t *testing.T, s string) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// row is a running team's status row with this session's member name and role.
func row(t *testing.T, team, member, role string) state.StatusTeam {
	t.Helper()
	r := state.StatusTeam{Team: teamID(t, team), Delivery: state.DeliverySocket, Colour: colour.Auto(team)}
	if member != "" {
		r.Member = &member
	}
	if role != "" {
		r.Role = &role
	}
	return r
}

// span is text in a colour.
func span(hex, text string) string { return colour.SGR(hex) + text + colour.Reset }

func redRow(text string) string { return redOn + text + resetC }

// runnerFunc is a scripted inner command.
type runnerFunc func(ctx context.Context, c execx.Cmd) (execx.Result, error)

func (f runnerFunc) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) { return f(ctx, c) }

// --- SL1: the inner status line ---

// With no settings file anywhere there is no inner line, and an empty segment is an empty line.
func TestNoSettingsIsEmpty(t *testing.T) {
	w := newWorld(t)
	w.holdLink()
	w.status(state.DesiredApplied, 0)
	if code, out := w.render(); code != term.ExitOK || out != "\n" {
		t.Fatalf("exit %d, out %q", code, out)
	}
}

// The inner command runs as /bin/sh -c in the project, with the payload on stdin, the loop
// guard set and stderr inherited; trailing newlines are stripped.
func TestInnerRuns(t *testing.T) {
	w := newWorld(t)
	w.env[paths.EnvRuntime] = ""
	w.projectSettings(`cat; printf ' %s %s\n\n' "$TEAMCHAT_STATUS_LINE_INNER" "$(pwd)"; echo oops >&2`)
	code, out := w.render()
	want := `{"cwd":"` + w.project + `"} 1 ` + w.project + "\n"
	if code != term.ExitOK || out != want {
		t.Fatalf("exit %d, out %q, want %q", code, out, want)
	}
	if w.stderr.String() != "oops\n" {
		t.Fatalf("stderr %q", w.stderr.String())
	}
}

// The inner line comes first, then the segment, each on its own row.
func TestInnerThenSegment(t *testing.T) {
	w := newWorld(t)
	w.projectSettings(`echo inner`)
	w.holdLink()
	w.status(state.DesiredRefused, 0)
	code, out := w.render()
	if want := "inner\n" + redRow("👥 teamchat membership refused") + "\n"; code != term.ExitOK || out != want {
		t.Fatalf("exit %d, out %q, want %q", code, out, want)
	}
}

// TEAMCHAT_STATUS_LINE_INNER set: the inner command is never run (the loop guard).
func TestLoopGuard(t *testing.T) {
	w := newWorld(t)
	w.env[paths.EnvRuntime] = ""
	w.projectSettings(`echo inner`)
	w.noInner()
	w.runner = runnerFunc(func(context.Context, execx.Cmd) (execx.Result, error) {
		t.Fatal("the inner command ran under the loop guard")
		return execx.Result{}, nil
	})
	if code, out := w.render(); code != term.ExitOK || out != "\n" {
		t.Fatalf("exit %d, out %q", code, out)
	}
}

// The first usable command wins: settings.local.json, settings.json, then the user's file;
// this command itself, a blank command and a file with no statusLine are passed over.
func TestSettingsOrder(t *testing.T) {
	local := func(w *world) string { return filepath.Join(w.project, ".claude", "settings.local.json") }
	shared := func(w *world) string { return filepath.Join(w.project, ".claude", "settings.json") }
	user := func(w *world) string { return filepath.Join(w.env["CLAUDE_CONFIG_DIR"], "settings.json") }
	for _, tc := range []struct {
		name  string
		setup func(w *world)
		want  string
	}{
		{"local wins", func(w *world) {
			w.settings(local(w), "echo local")
			w.settings(shared(w), "echo shared")
			w.settings(user(w), "echo user")
		}, "local"},
		{"shared before user", func(w *world) {
			w.settings(shared(w), "echo shared")
			w.settings(user(w), "echo user")
		}, "shared"},
		{"itself skipped", func(w *world) {
			w.settings(local(w), "  teamchat status-line ")
			w.settings(shared(w), "/home/u/.local/kit/bin/teamchat status-line")
			w.settings(user(w), "echo user")
		}, "user"},
		{"blank and absent skipped", func(w *world) {
			w.settings(local(w), "   ")
			w.write(shared(w), `{"statusLine":{"type":"command","command":7},"other":1}`)
			w.write(user(w), `["not an object"]`)
		}, ""},
		{"no statusLine object", func(w *world) {
			w.write(local(w), `{"statusLine":"echo x"}`)
			w.settings(user(w), "echo user")
		}, "user"},
		{"HOME without CLAUDE_CONFIG_DIR", func(w *world) {
			delete(w.env, "CLAUDE_CONFIG_DIR")
			w.settings(filepath.Join(w.env["HOME"], ".claude", "settings.json"), "echo home")
		}, "home"},
		{"relative HOME", func(w *world) {
			delete(w.env, "CLAUDE_CONFIG_DIR")
			w.env["HOME"] = "relative"
		}, ""},
		{"a command ending in teamchat status-line but another binary runs", func(w *world) {
			w.settings(local(w), "echo teamchat-status-line")
		}, "teamchat-status-line"},
	} {
		w := newWorld(t)
		w.env[paths.EnvRuntime] = ""
		tc.setup(w)
		code, out := w.render()
		if want := tc.want + "\n"; code != term.ExitOK || out != want {
			t.Fatalf("%s: exit %d, out %q, want %q", tc.name, code, out, want)
		}
	}
}

// The project directory is the first absolute of workspace.project_dir, CLAUDE_PROJECT_DIR
// and cwd; with none, only the user's settings are read, and the command runs where it is.
func TestProjectDir(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload func(w *world) string
		envDir  bool
		want    string
	}{
		{"workspace", func(w *world) string {
			return `{"workspace":{"project_dir":"` + w.project + `"},"cwd":"/elsewhere"}`
		}, false, "project"},
		{"env before cwd", func(*world) string { return `{"workspace":{"project_dir":"rel"},"cwd":"/elsewhere"}` }, true, "project"},
		{"cwd", func(w *world) string { return `{"cwd":"` + w.project + `"}` }, false, "project"},
		{"none", func(*world) string { return `{"cwd":"rel","workspace":7}` }, false, "user"},
		{"not an object", func(*world) string { return `[1]` }, false, "user"},
		{"not JSON", func(*world) string { return `{` }, false, "user"},
		{"over 1 MiB", func(w *world) string {
			return `{"cwd":"` + w.project + `","pad":"` + strings.Repeat("x", maxPayload) + `"}`
		}, false, "user"},
	} {
		w := newWorld(t)
		w.env[paths.EnvRuntime] = ""
		w.projectSettings("echo project")
		w.settings(filepath.Join(w.env["CLAUDE_CONFIG_DIR"], "settings.json"), "echo user")
		w.stdin = strings.NewReader(tc.payload(w))
		if tc.envDir {
			w.env["CLAUDE_PROJECT_DIR"] = w.project
		}
		code, out := w.render()
		if want := tc.want + "\n"; code != term.ExitOK || out != want {
			t.Fatalf("%s: exit %d, out %q, want %q", tc.name, code, out, want)
		}
	}
	// a project path that is not a directory still names the settings, and the command runs
	// in this process's directory.
	w := newWorld(t)
	w.env[paths.EnvRuntime] = ""
	gone := filepath.Join(w.base, "gone")
	w.stdin = strings.NewReader(`{"cwd":"` + gone + `"}`)
	w.runner = runnerFunc(func(_ context.Context, c execx.Cmd) (execx.Result, error) {
		if c.Dir != "" {
			t.Errorf("dir %q", c.Dir)
		}
		return execx.Result{}, nil
	})
	if code, out := w.render(); code != term.ExitOK || out != "\n" {
		t.Fatalf("exit %d, out %q", code, out)
	}
}

// A settings file that exists but cannot be read, or is not JSON, is a red line naming it,
// and stops the search; a FIFO is never waited on.
func TestSettingsErrors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(w *world, path string)
		why   string
	}{
		{"not JSON", func(w *world, p string) { w.write(p, `{"statusLine":`) }, "is not valid JSON"},
		{"trailing junk", func(w *world, p string) { w.write(p, `{} x`) }, "is not valid JSON"},
		{"not UTF-8", func(w *world, p string) { w.write(p, "{\"a\":\"\xff\"}") }, "cannot be read"},
		{"a directory", func(_ *world, p string) {
			if err := os.MkdirAll(p, 0o700); err != nil {
				t.Fatal(err)
			}
		}, "cannot be read"},
		{"a FIFO", func(_ *world, p string) {
			if err := unix.Mkfifo(p, 0o600); err != nil {
				t.Fatal(err)
			}
		}, "cannot be read"},
		{"over 1 MiB", func(w *world, p string) { w.write(p, `{"a":"`+strings.Repeat("x", maxSettings)+`"}`) }, "cannot be read"},
		{"unreadable", func(w *world, p string) {
			w.write(p, `{}`)
			if err := os.Chmod(p, 0); err != nil {
				t.Fatal(err)
			}
		}, "cannot be read"},
	} {
		if tc.name == "unreadable" && os.Geteuid() == 0 {
			continue
		}
		w := newWorld(t)
		w.env[paths.EnvRuntime] = ""
		p := filepath.Join(w.project, ".claude", "settings.local.json")
		tc.setup(w, p)
		w.settings(filepath.Join(w.env["CLAUDE_CONFIG_DIR"], "settings.json"), "echo user")
		code, out := w.render()
		if want := redRow("status line: "+p+" "+tc.why) + "\n"; code != term.ExitOK || out != want {
			t.Fatalf("%s: exit %d, out %q, want %q", tc.name, code, out, want)
		}
	}
}

// A non-zero exit keeps the text and adds the status in red; a signal is a negative status;
// stdout is decoded with U+FFFD for each invalid byte.
func TestInnerExits(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    string
	}{
		{`echo text; exit 3`, "text " + redRow("(status line exit 3)")},
		{`exit 4`, redRow("(status line exit 4)")},
		{`echo text; kill -9 $$`, "text " + redRow("(status line exit -9)")},
		{`printf 'a\377\376b\n'`, "a��b"},
		{`printf 'a\n\n\n'`, "a"},
		{`head -c 100000 /dev/zero | tr '\0' x`, strings.Repeat("x", maxInner)},
	} {
		w := newWorld(t)
		w.env[paths.EnvRuntime] = ""
		w.projectSettings(tc.command)
		code, out := w.render()
		if want := tc.want + "\n"; code != term.ExitOK || out != want {
			t.Fatalf("%q: exit %d, out %q, want %q", tc.command, code, out, want)
		}
	}
}

// A command that cannot be started is a red note.
func TestInnerNotRun(t *testing.T) {
	w := newWorld(t)
	w.env[paths.EnvRuntime] = ""
	w.projectSettings("echo x")
	w.runner = runnerFunc(func(context.Context, execx.Cmd) (execx.Result, error) {
		return execx.Result{}, errors.New("no shell")
	})
	code, out := w.render()
	if want := redRow("(status line could not be run)") + "\n"; code != term.ExitOK || out != want {
		t.Fatalf("exit %d, out %q", code, out)
	}
}

// At 10 s the run is cancelled and the output is only the timeout note.
func TestInnerTimeout(t *testing.T) {
	w := newWorld(t)
	w.env[paths.EnvRuntime] = ""
	w.projectSettings("echo never")
	started := make(chan execx.Cmd, 1)
	w.runner = runnerFunc(func(ctx context.Context, c execx.Cmd) (execx.Result, error) {
		started <- c
		<-ctx.Done()
		return execx.Result{}, context.Cause(ctx)
	})
	done := w.async()
	c := <-started
	if c.Name != "/bin/sh" || len(c.Args) != 2 || c.Args[0] != "-c" || c.Args[1] != "echo never" || !c.NewSession {
		t.Fatalf("cmd %+v", c)
	}
	if len(c.Env) != 1 || c.Env[0] != paths.EnvStatusLineInner+"=1" {
		t.Fatalf("env %v", c.Env)
	}
	w.clk.Advance(innerTimeout - time.Millisecond)
	select {
	case <-done:
		t.Fatal("ended before 10 s")
	default:
	}
	w.clk.Advance(time.Millisecond)
	if code := <-done; code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if want := redRow("(status line timed out after 10 s)") + "\n"; w.out.String() != want {
		t.Fatalf("out %q", w.out.String())
	}
}

// A real command whose grandchild left the group and holds stdout: the group is killed at 10 s
// and the render does not wait on the pipe past the grace.
func TestInnerTimeoutGrandchild(t *testing.T) {
	w := newWorld(t)
	w.env[paths.EnvRuntime] = ""
	w.projectSettings(`setsid sleep 5 2>/dev/null & echo partial; echo started >&2; exec sleep 30`)
	seen := w.stderr.await("started")
	done := w.async()
	<-seen
	w.clk.Advance(innerTimeout)
	if code := <-done; code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if want := redRow("(status line timed out after 10 s)") + "\n"; w.out.String() != want {
		t.Fatalf("out %q", w.out.String())
	}
}

// SIGTERM, SIGINT or SIGHUP before the command starts: 128+n and nothing on stdout.
func TestCancelBeforePid(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP} {
		w := newWorld(t)
		w.projectSettings("echo never")
		r, wr := io.Pipe()
		t.Cleanup(func() {
			if err := wr.Close(); err != nil {
				t.Error(err)
			}
		})
		w.stdin = r
		w.signals = make(chan os.Signal, 1)
		w.runner = runnerFunc(func(context.Context, execx.Cmd) (execx.Result, error) {
			t.Error("the command ran after the cancel")
			return execx.Result{}, nil
		})
		done := w.async()
		w.signals <- sig
		if code := <-done; code != term.ExitCode(128+int(sig)) || w.out.Len() != 0 {
			t.Fatalf("%v: exit %d, out %q", sig, code, w.out.String())
		}
	}
}

// A signal while the command runs kills its group: 128+n, nothing on stdout, the child gone.
func TestCancelAfterPid(t *testing.T) {
	w := newWorld(t)
	w.projectSettings(`echo partial; echo "pid $$" >&2; exec sleep 30`)
	w.signals = make(chan os.Signal, 1)
	seen := w.stderr.await("pid ")
	done := w.async()
	<-seen
	w.signals <- syscall.SIGINT
	if code := <-done; code != 130 || w.out.Len() != 0 {
		t.Fatalf("exit %d, out %q", code, w.out.String())
	}
	var pid int
	for f := range strings.FieldsSeq(w.stderr.String()) {
		if n, ok := atoi(f); ok {
			pid = n
		}
	}
	if pid == 0 {
		t.Fatalf("stderr %q", w.stderr.String())
	}
	if err := unix.Kill(pid, 0); !errors.Is(err, unix.ESRCH) {
		t.Fatalf("child %d still there: %v", pid, err)
	}
}

func atoi(s string) (int, bool) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, s != ""
}

// A signal that is not a syscall.Signal is passed over; the render goes on.
func TestOtherSignal(t *testing.T) {
	w := newWorld(t)
	w.env[paths.EnvRuntime] = ""
	w.noInner()
	w.signals = make(chan os.Signal, 1)
	w.signals <- fakeSignal{}
	if code, out := w.render(); code != term.ExitOK || out != "\n" {
		t.Fatalf("exit %d, out %q", code, out)
	}
}

type fakeSignal struct{}

func (fakeSignal) String() string { return "fake" }
func (fakeSignal) Signal()        {}

// A stdin that fails to read is no payload; a nil stdin is none.
func TestStdinFails(t *testing.T) {
	w := newWorld(t)
	w.env[paths.EnvRuntime] = ""
	w.settings(filepath.Join(w.env["CLAUDE_CONFIG_DIR"], "settings.json"), "cat; echo user")
	w.stdin = failingReader{}
	if code, out := w.render(); code != term.ExitOK || out != "user\n" {
		t.Fatalf("exit %d, out %q", code, out)
	}
	w.out.Reset()
	w.stdin = nil
	if code, out := w.render(); code != term.ExitOK || out != "user\n" {
		t.Fatalf("nil: exit %d, out %q", code, out)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read fails") }

// A stdout that cannot be written still exits 0.
func TestStdoutFails(t *testing.T) {
	w := newWorld(t)
	w.noInner()
	e := w.cliEnv()
	e.Stdout = failingWriter{}
	if code := run(e, w.deps()); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write fails") }

// Main is the verb with the production seams.
func TestMainVerb(t *testing.T) {
	w := newWorld(t)
	w.noInner()
	w.env[paths.EnvRuntime] = ""
	inv, err := cli.Parse([]string{"status-line"})
	if err != nil {
		t.Fatal(err)
	}
	if code := Main(w.cliEnv(), inv); code != term.ExitOK || w.out.String() != "\n" {
		t.Fatalf("exit %d, out %q", code, w.out.String())
	}
}

// --- B27: the team segment ---

// Two teams, each in its own colour (the automatic one and an override), with this session's
// short member name and role letter, byte for byte.
func TestTwoTeams(t *testing.T) {
	w := newWorld(t)
	w.noInner()
	w.holdLink()
	beta := row(t, "beta", "fedora-desktop.1", proto.RoleWorker)
	beta.Colour = "#fb7185"
	w.status(state.DesiredApplied, 0, row(t, "alpha", "fedora-desktop.2", proto.RoleCoordinator), beta)
	code, out := w.render()
	want := span(colour.Auto("alpha"), "👥 alpha: fedora-desktop.2 (c),") + " " + span("#fb7185", "beta: fedora-desktop.1 (w)") + "\n"
	if code != term.ExitOK || out != want {
		t.Fatalf("exit %d, out %q, want %q", code, out, want)
	}
}

// No handle yet is member?, no role is role?, a probe is p; the marks follow in order.
func TestPieceForms(t *testing.T) {
	w := newWorld(t)
	w.noInner()
	w.holdLink()
	failed := state.FailedTrust
	all := row(t, "gamma", "fedora-desktop.3", proto.RoleProbe)
	all.Down, all.Stalled, all.Missing, all.Failed = true, true, 2, &failed
	w.status(state.DesiredApplied, 0, row(t, "alpha", "", ""), all)
	code, out := w.render()
	want := span(colour.Auto("alpha"), "👥 alpha: member? (role?),") + " " +
		span(colour.Auto("gamma"), "gamma: fedora-desktop.3 (p) (down) (stalled) (missing) (failed)") + "\n"
	if code != term.ExitOK || out != want {
		t.Fatalf("exit %d, out %q, want %q", code, out, want)
	}
}

// OF1: one team down beside a running one is drawn, and the status line exits 0.
func TestDownBesideRunning(t *testing.T) {
	w := newWorld(t)
	w.noInner()
	w.holdLink()
	down := row(t, "beta", "fedora-desktop.1", proto.RoleWorker)
	down.Down, down.Delivery = true, state.DeliveryNone
	w.status(state.DesiredApplied, 0, row(t, "alpha", "fedora-desktop.2", proto.RoleCoordinator), down)
	code, out := w.render()
	want := span(colour.Auto("alpha"), "👥 alpha: fedora-desktop.2 (c),") + " " + span(colour.Auto("beta"), "beta: fedora-desktop.1 (w) (down)") + "\n"
	if code != term.ExitOK || out != want {
		t.Fatalf("exit %d, out %q, want %q", code, out, want)
	}
}

// A free link.lock replaces the rows with one red form from keeper.json and keeper.lock.
func TestLinkDownForms(t *testing.T) {
	next := (boot0 + 3*time.Second).Milliseconds()
	for _, tc := range []struct {
		name   string
		keeper bool
		link   string
		kind   string
		next   *int64
		want   string
	}{
		{"restarting", true, state.LinkRestarting, state.KindHost, &next, "teamchat restarting"},
		{"starting", true, state.LinkRunning, state.KindCCY, nil, "teamchat starting"},
		{"no keeper.json", true, "", "", nil, "teamchat starting"},
		{"failed ccy", true, state.LinkFailed, state.KindCCY, nil, "teamchat stopped: ccy-teams restart work-1"},
		{"failed lxc", true, state.LinkFailed, state.KindLXC, nil, "teamchat stopped: ccy-teams restart work-1"},
		{"failed host", true, state.LinkFailed, state.KindHost, nil, "teamchat stopped: relaunch this session"},
		{"keeper gone", false, state.LinkRunning, state.KindCCY, nil, "teamchat stopped: ccy-teams restart work-1"},
		{"keeper gone, nothing known", false, "", "", nil, "teamchat stopped: relaunch this session"},
	} {
		w := newWorld(t)
		w.noInner()
		if tc.keeper {
			w.holdKeeper()
		}
		if tc.link != "" {
			w.keeperJSON(tc.link, tc.kind, tc.next)
		}
		w.status(state.DesiredApplied, 0, row(t, "alpha", "fedora-desktop.2", proto.RoleCoordinator))
		code, out := w.render()
		if want := redRow("👥 "+tc.want) + "\n"; code != term.ExitOK || out != want {
			t.Fatalf("%s: exit %d, out %q, want %q", tc.name, code, out, want)
		}
	}
}

// A held link.lock with status.json more than 90 s old while a team is held: not answering.
func TestNotAnswering(t *testing.T) {
	piece := span(colour.Auto("alpha"), "👥 alpha: fedora-desktop.2 (c)")
	for _, tc := range []struct {
		name    string
		written int64
		teams   bool
		want    string
	}{
		{"90 s is fresh", wall0 - 90_000, true, piece},
		{"over 90 s", wall0 - 90_001, true, piece + " " + redRow("teamchat not answering")},
		{"no team", wall0 - 900_000, false, ""},
		{"ahead of the clock", wall0 + 5_000, true, piece},
	} {
		w := newWorld(t)
		w.noInner()
		w.holdLink()
		var teams []state.StatusTeam
		if tc.teams {
			teams = append(teams, row(t, "alpha", "fedora-desktop.2", proto.RoleCoordinator))
		}
		w.statusAt(tc.written, state.DesiredApplied, 0, teams...)
		code, out := w.render()
		if want := tc.want + "\n"; code != term.ExitOK || out != want {
			t.Fatalf("%s: exit %d, out %q, want %q", tc.name, code, out, want)
		}
	}
}

// Membership refused and unusable hook input are red with or without teams, the link up or not.
func TestRedNotes(t *testing.T) {
	refused := redRow("teamchat membership refused")
	unusable := redRow("teamchat hook input unusable: 3")
	piece := span(colour.Auto("alpha"), "👥 alpha: fedora-desktop.2 (c)")
	for _, tc := range []struct {
		name  string
		link  bool
		teams bool
		want  string
	}{
		{"with a team", true, true, piece + " " + refused + " " + unusable},
		{"no team", true, false, redRow("👥 teamchat membership refused") + " " + unusable},
		{"link down", false, true, redRow("👥 teamchat stopped: relaunch this session") + " " + refused + " " + unusable},
	} {
		w := newWorld(t)
		w.noInner()
		if tc.link {
			w.holdLink()
		}
		var teams []state.StatusTeam
		if tc.teams {
			teams = append(teams, row(t, "alpha", "fedora-desktop.2", proto.RoleCoordinator))
		}
		w.status(state.DesiredRefused, 3, teams...)
		code, out := w.render()
		if want := tc.want + "\n"; code != term.ExitOK || out != want {
			t.Fatalf("%s: exit %d, out %q, want %q", tc.name, code, out, want)
		}
	}
}

// Rows wrap greedily at COLUMNS between pieces, continuation indented 3 spaces, escapes taking
// no columns; a red note's words wrap the same way, one red span per row.
func TestWrap(t *testing.T) {
	w := newWorld(t)
	w.noInner()
	w.holdLink()
	w.status(state.DesiredRefused, 0,
		row(t, "alpha", "fedora-desktop.2", proto.RoleCoordinator),
		row(t, "beta", "fedora-desktop.1", proto.RoleWorker))
	// "👥 alpha: fedora-desktop.2 (c)," is 31 columns; with " beta: fedora-desktop.1 (w)" it
	// is 58.
	w.env["COLUMNS"] = "40"
	code, out := w.render()
	want := span(colour.Auto("alpha"), "👥 alpha: fedora-desktop.2 (c),") + "\n" +
		span(colour.Auto("beta"), "   beta: fedora-desktop.1 (w)") + " " + redRow("teamchat") + "\n" +
		redRow("   membership refused") + "\n"
	if code != term.ExitOK || out != want {
		t.Fatalf("exit %d, out %q, want %q", code, out, want)
	}
	// exactly the width fits.
	w.out.Reset()
	w.env["COLUMNS"] = "58"
	_, out = w.render()
	if !strings.HasPrefix(out, span(colour.Auto("alpha"), "👥 alpha: fedora-desktop.2 (c),")+" "+span(colour.Auto("beta"), "beta: fedora-desktop.1 (w)")+"\n") {
		t.Fatalf("58: out %q", out)
	}
}

// A red note alone wraps by words, each row one red span with the label or the indent in it.
func TestWrapRedNote(t *testing.T) {
	w := newWorld(t)
	w.noInner()
	w.keeperJSON(state.LinkFailed, state.KindCCY, nil)
	w.env["COLUMNS"] = "20"
	code, out := w.render()
	want := redRow("👥 teamchat stopped:") + "\n" + redRow("   ccy-teams restart") + "\n" + redRow("   work-1") + "\n"
	if code != term.ExitOK || out != want {
		t.Fatalf("exit %d, out %q, want %q", code, out, want)
	}
	// a word wider than the row is a row of its own.
	w.out.Reset()
	w.env["COLUMNS"] = "3"
	_, out = w.render()
	want = redRow("👥 teamchat") + "\n" + redRow("   stopped:") + "\n" + redRow("   ccy-teams") + "\n" + redRow("   restart") + "\n" + redRow("   work-1") + "\n"
	if out != want {
		t.Fatalf("3: out %q, want %q", out, want)
	}
}

// COLUMNS that is not 1 to 6 digits above zero is one row.
func TestColumnsInvalid(t *testing.T) {
	for _, cols := range []string{"", "0", "000", "abc", "-5", "1234567", "4x"} {
		w := newWorld(t)
		w.noInner()
		w.keeperJSON(state.LinkFailed, state.KindCCY, nil)
		w.env["COLUMNS"] = cols
		code, out := w.render()
		if want := redRow("👥 teamchat stopped: ccy-teams restart work-1") + "\n"; code != term.ExitOK || out != want {
			t.Fatalf("%q: exit %d, out %q", cols, code, out)
		}
	}
	if columns("999999") != 999999 || columns("12") != 12 {
		t.Fatal("columns")
	}
}

// OF1: a missing, oversized, undecodable or foreign status.json is the red stopped form while
// the link is down and no segment while it runs; never a non-zero exit.
func TestStatusUnreadable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(w *world)
	}{
		{"missing", func(*world) {}},
		{"oversized", func(w *world) { w.write(w.run.Status(), strings.Repeat(" ", state.MaxStatus+1)) }},
		{"undecodable", func(w *world) { w.write(w.run.Status(), `{"v":1}`) }},
		{"another run", func(w *world) {
			w.status(state.DesiredRefused, 0, row(t, "alpha", "fedora-desktop.2", proto.RoleCoordinator))
			data, err := os.ReadFile(w.run.Status())
			if err != nil {
				t.Fatal(err)
			}
			w.write(w.run.Status(), strings.Replace(string(data), runID, "ffeeddccbbaa0011", 1))
		}},
	} {
		for _, link := range []bool{true, false} {
			w := newWorld(t)
			w.noInner()
			if link {
				w.holdLink()
			}
			tc.setup(w)
			code, out := w.render()
			want := "\n"
			if !link {
				want = redRow("👥 teamchat stopped: relaunch this session") + "\n"
			}
			if code != term.ExitOK || out != want {
				t.Fatalf("%s, link %v: exit %d, out %q", tc.name, link, code, out)
			}
		}
	}
}

// No TEAMCHAT_RUNTIME, or one naming no session: no segment at all.
func TestNoRuntime(t *testing.T) {
	for _, dir := range []string{"", "/elsewhere/" + runID} {
		w := newWorld(t)
		w.noInner()
		w.status(state.DesiredRefused, 1)
		w.env[paths.EnvRuntime] = dir
		if code, out := w.render(); code != term.ExitOK || out != "\n" {
			t.Fatalf("%q: exit %d, out %q", dir, code, out)
		}
	}
}

// A run directory that cannot be opened is the stopped form: the keeper has gone.
func TestRunDirGone(t *testing.T) {
	w := newWorld(t)
	w.noInner()
	if err := os.Chmod(w.run.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(w.run.Dir(), 0o700); err != nil {
			t.Error(err)
		}
	})
	code, out := w.render()
	if want := redRow("👥 teamchat stopped: relaunch this session") + "\n"; code != term.ExitOK || out != want {
		t.Fatalf("exit %d, out %q", code, out)
	}
}
