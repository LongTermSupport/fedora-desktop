package tmuxccy_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

func startSpec() tmuxccy.StartSpec {
	return tmuxccy.StartSpec{
		Name: "ccy-proj", Dir: "/srv/my proj", Pid: 4242,
		Command:    []string{"env", "CCY_SESSION_RESTORE=1", "/var/local/claude-yolo/claude-yolo", "--supervise", "--continue"},
		RecordPath: "/state/ccy/sessions/ccy-proj", Prefix: "ccy",
	}
}

func startRig(f *execx.Fake) *tmuxccy.Tmux {
	tm := newTmux(f)
	tm.LookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	return tm
}

func TestStartDetachedExactArgv(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"systemd-escape", "--", "ccy-proj"}, ok("ccy\\x2dproj\n"))
	f.On([]string{"systemd-run"}, ok(""))
	if err := startRig(f).StartDetached(context.Background(), startSpec()); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	if len(calls) != 2 {
		t.Fatalf("calls %v", calls)
	}
	run := append([]string{calls[1].Name}, calls[1].Args...)
	want := []string{
		"systemd-run", "--user", "--scope", "--quiet", "--collect",
		"--unit", `ccy-tmux-4242-ccy\x2dproj`, "--description", "CCY tmux session ccy-proj",
		"--", "tmux", "-L", "ccy",
		"new-session", "-d", "-s", "ccy-proj", "-c", "/srv/my proj", "--",
		"bash", "-c", registry.Trampoline("/state/ccy/sessions/ccy-proj", "ccy"), "ccy-tmux",
		"env", "CCY_SESSION_RESTORE=1", "/var/local/claude-yolo/claude-yolo", "--supervise", "--continue",
		";", "set-hook", "-g", "client-attached", tmuxccy.SingleAttachHook(),
	}
	if !slices.Equal(run, want) {
		t.Fatalf("argv:\n%q\nwant:\n%q", run, want)
	}
}

func TestStartDetachedUsesTheSocketAndPidOfTheProcess(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"systemd-escape"}, ok("x\n"))
	f.On([]string{"systemd-run"}, ok(""))
	tm := startRig(f)
	tm.Socket = "other"
	spec := startSpec()
	spec.Pid = 0
	if err := tm.StartDetached(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	line := f.Calls()[1].Line()
	if !strings.Contains(line, "-- tmux -L other new-session") || !strings.Contains(line, fmt.Sprintf("--unit ccy-tmux-%d-x ", os.Getpid())) ||
		!strings.Contains(line, "tmux -L other list-clients") {
		t.Fatalf("line %q", line)
	}
}

func TestStartDetachedPreconditions(t *testing.T) {
	missing := func(which string) func(string) (string, error) {
		return func(name string) (string, error) {
			if name == which {
				return "", exec.ErrNotFound
			}
			return "/usr/bin/" + name, nil
		}
	}
	cases := []struct{ tool, want string }{
		{"tmux", "tmux is not installed, so session 'ccy-proj' cannot be started. Deploy it with playbooks/imports/play-tmux-sessions.yml (part of playbook-main.yml)."},
		{"systemd-run", "systemd-run is not available, so session 'ccy-proj' cannot be started. It ships with systemd"},
		{"systemd-escape", "systemd-escape is not available, so session 'ccy-proj' cannot be started. It ships with systemd"},
	}
	for _, c := range cases {
		f := &execx.Fake{}
		tm := startRig(f)
		tm.LookPath = missing(c.tool)
		err := tm.StartDetached(context.Background(), startSpec())
		if err == nil || !strings.HasPrefix(err.Error(), c.want) {
			t.Errorf("%s: got %v", c.tool, err)
		}
		if len(f.Calls()) != 0 {
			t.Errorf("%s: nothing is run when a tool is missing: %v", c.tool, f.Calls())
		}
	}
}

func TestStartDetachedFailures(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"systemd-escape"}, fail("bad name"))
	if err := startRig(f).StartDetached(context.Background(), startSpec()); err == nil || !strings.Contains(err.Error(), "bad name") {
		t.Fatalf("got %v", err)
	}
	f = &execx.Fake{}
	f.On([]string{"systemd-escape"}, ok("x\n"))
	f.On([]string{"systemd-run"}, fail("Failed to connect to bus"))
	err := startRig(f).StartDetached(context.Background(), startSpec())
	if err == nil || !strings.Contains(err.Error(), "could not start session 'ccy-proj'") || !strings.Contains(err.Error(), "Failed to connect to bus") {
		t.Fatalf("got %v", err)
	}
}

func TestStartDetachedValidatesItsSpec(t *testing.T) {
	for name, edit := range map[string]func(*tmuxccy.StartSpec){
		"name":    func(s *tmuxccy.StartSpec) { s.Name = "" },
		"dir":     func(s *tmuxccy.StartSpec) { s.Dir = "" },
		"command": func(s *tmuxccy.StartSpec) { s.Command = nil },
		"record":  func(s *tmuxccy.StartSpec) { s.RecordPath = "" },
		"prefix":  func(s *tmuxccy.StartSpec) { s.Prefix = "" },
	} {
		spec := startSpec()
		edit(&spec)
		if err := startRig(&execx.Fake{}).StartDetached(context.Background(), spec); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestStartDetachedDefaultsLookPathToTheRealOne(t *testing.T) {
	tm := newTmux(&execx.Fake{})
	t.Setenv("PATH", t.TempDir())
	err := tm.StartDetached(context.Background(), startSpec())
	if err == nil || !strings.Contains(err.Error(), "tmux is not installed") {
		t.Fatalf("got %v", err)
	}
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("the cause is kept: %v", err)
	}
}

// scopeless stands in for systemd: systemd-escape echoes its last argument and systemd-run runs
// everything after its `--`, as the suites' doubles do; any other command runs for real.
type scopeless struct{}

func (scopeless) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) {
	switch c.Name {
	case "systemd-escape":
		if c.Stdout != nil {
			if _, err := c.Stdout.Write([]byte(c.Args[len(c.Args)-1] + "\n")); err != nil {
				return execx.Result{}, err
			}
		}
		return execx.Result{}, nil
	case "systemd-run":
		i := slices.Index(c.Args, "--")
		return execx.OS{}.Run(ctx, execx.Cmd{Name: c.Args[i+1], Args: c.Args[i+2:], Stdout: c.Stdout, Stderr: c.Stderr})
	}
	return execx.OS{}.Run(ctx, c)
}

// With real tmux and a stand-in for systemd (as the suites use), the pane runs the trampoline:
// the launcher's status holds the window and the record goes.
func TestStartDetachedWithRealTmuxRunsTheTrampoline(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	t.Setenv("TMUX_TMPDIR", t.TempDir())
	t.Setenv("TMUX", "")
	record := filepath.Join(t.TempDir(), "ccy-real")
	if err := os.WriteFile(record, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	tm := startRig(&execx.Fake{})
	tm.Runner = scopeless{}
	tm.Socket = "ccy-wave1-start"
	t.Cleanup(func() {
		if _, err := tm.Run(context.Background(), "kill-server"); err != nil && !strings.Contains(err.Error(), "no server running") {
			t.Logf("kill-server: %v", err)
		}
	})
	spec := tmuxccy.StartSpec{
		Name: "ccy-real", Dir: t.TempDir(), RecordPath: record, Prefix: "ccy",
		Command: []string{"sh", "-c", "exit 7"},
	}
	if err := tm.StartDetached(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	var screen string
	for range 100 {
		var err error
		if screen, err = tm.CapturePane(context.Background(), "ccy-real", false); err == nil && strings.Contains(screen, "Press Enter to close this session.") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(screen, "ccy exited with status 7. Press Enter to close this session.") {
		t.Fatalf("screen %q", screen)
	}
	if _, err := os.Stat(record); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the record is removed when the launcher returns: %v", err)
	}
	hooks, err := tm.Run(context.Background(), "show-hooks", "-g")
	if err != nil || !strings.Contains(string(hooks.Stdout), "client-attached") {
		t.Fatalf("the hook is installed in the same round trip: %q, %v", hooks.Stdout, err)
	}
}

func TestRegistryTrampolineAndStartAgree(t *testing.T) {
	// StartSpec builds its trampoline with the registry's, so a record path with odd characters
	// reaches the pane intact.
	f := &execx.Fake{}
	f.On([]string{"systemd-escape"}, ok("x\n"))
	f.On([]string{"systemd-run"}, ok(""))
	spec := startSpec()
	spec.RecordPath = "/state/odd name's/ccy-proj"
	if err := startRig(f).StartDetached(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(f.Calls()[1].Args, registry.Trampoline(spec.RecordPath, "ccy")) {
		t.Fatalf("args %q", f.Calls()[1].Args)
	}
}
