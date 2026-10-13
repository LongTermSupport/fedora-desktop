package tmuxccy_test

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

func TestPanes(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"tmux", "-L", "ccy", "list-panes", "-a", "-F", "#{session_name} #{pane_pid}"}, ok("ccy-a 100\nccy-b 200\n\nccy-a 101\n"))
	got, err := newTmux(f).Panes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []tmuxccy.Pane{{Session: "ccy-a", PID: 100}, {Session: "ccy-b", PID: 200}, {Session: "ccy-a", PID: 101}}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("got %+v", got)
	}
}

func TestPanesNoServerIsEmptyAndOtherFailureIsError(t *testing.T) {
	for _, msg := range []string{"no server running on /tmp/x", "error connecting to /tmp/x (No such file or directory)"} {
		f := &execx.Fake{}
		f.On([]string{"tmux"}, fail(msg))
		got, err := newTmux(f).Panes(context.Background())
		if err != nil || len(got) != 0 {
			t.Fatalf("%q: got %v, %v", msg, got, err)
		}
	}
	f := &execx.Fake{}
	f.On([]string{"tmux"}, fail("protocol version mismatch"))
	if _, err := newTmux(f).Panes(context.Background()); err == nil || !strings.Contains(err.Error(), "tmux list-panes failed") || !strings.Contains(err.Error(), "protocol version mismatch") {
		t.Fatalf("got %v", err)
	}
}

func TestPanesMalformedLines(t *testing.T) {
	for _, out := range []string{"onlyname\n", "name notanumber\n"} {
		f := &execx.Fake{}
		f.On([]string{"tmux"}, ok(out))
		if _, err := newTmux(f).Panes(context.Background()); err == nil {
			t.Fatalf("%q: want error", out)
		}
	}
}

func TestPanesSessionNameMayHoldASpace(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"tmux"}, ok("my session 42\n"))
	got, err := newTmux(f).Panes(context.Background())
	if err != nil || len(got) != 1 || got[0].Session != "my session" || got[0].PID != 42 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestCapturePaneArgvAndErrors(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"tmux", "-L", "ccy", "capture-pane", "-p", "-e", "-t", "=alpha:"}, ok("with escapes"))
	f.On([]string{"tmux", "-L", "ccy", "capture-pane", "-p", "-t", "=alpha:"}, ok("plain"))
	tm := newTmux(f)
	if got, err := tm.CapturePane(context.Background(), "alpha", true); err != nil || got != "with escapes" {
		t.Fatalf("got %q, %v", got, err)
	}
	if got, err := tm.CapturePane(context.Background(), "alpha", false); err != nil || got != "plain" {
		t.Fatalf("got %q, %v", got, err)
	}
	f.On([]string{"tmux", "-L", "ccy", "capture-pane", "-p", "-t", "=beta:"}, fail("can't find pane: beta"))
	_, err := tm.CapturePane(context.Background(), "beta", false)
	if err == nil || !strings.HasPrefix(err.Error(), "the screen of beta could not be read: can't find pane: beta") {
		t.Fatalf("got %v", err)
	}
}

func TestSendKeysAndEnterTargetsKeepTheColon(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"tmux", "-L", "ccy", "send-keys", "-t", "=alpha:", "-l", "/compact now"}, ok(""))
	f.On([]string{"tmux", "-L", "ccy", "send-keys", "-t", "=alpha:", "Enter"}, ok(""))
	tm := newTmux(f)
	if err := tm.SendKeys(context.Background(), "alpha", "/compact now"); err != nil {
		t.Fatal(err)
	}
	if err := tm.SendEnter(context.Background(), "alpha"); err != nil {
		t.Fatal(err)
	}
	f.On([]string{"tmux"}, fail("no such pane"))
	if err := tm.SendKeys(context.Background(), "alpha", "x"); err == nil || !strings.HasPrefix(err.Error(), "typing into alpha failed: no such pane") {
		t.Fatalf("got %v", err)
	}
	if err := tm.SendEnter(context.Background(), "alpha"); err == nil || !strings.HasPrefix(err.Error(), "pressing Enter in alpha failed: no such pane") {
		t.Fatalf("got %v", err)
	}
}

func TestKillSession(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"tmux", "-L", "ccy", "kill-session", "-t", "=alpha"}, ok(""))
	tm := newTmux(f)
	if err := tm.KillSession(context.Background(), "alpha"); err != nil {
		t.Fatal(err)
	}
	f.On([]string{"tmux"}, fail("can't find session: alpha"))
	if err := tm.KillSession(context.Background(), "alpha"); !errors.Is(err, tmuxccy.ErrSessionGone) {
		t.Fatalf("got %v", err)
	}
	f.On([]string{"tmux"}, fail("server exited unexpectedly"))
	err := tm.KillSession(context.Background(), "alpha")
	if err == nil || errors.Is(err, tmuxccy.ErrSessionGone) || !strings.Contains(err.Error(), "could not end 'alpha'") {
		t.Fatalf("got %v", err)
	}
}

func TestSocketIsConfigurable(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"tmux", "-L", "private", "kill-session"}, ok(""))
	f.On([]string{"tmux", "-L", "private", "set-hook", "-g", "client-attached", tmuxccy.SingleAttachHookFor("private")}, ok(""))
	tm := newTmux(f)
	tm.Socket = "private"
	if err := tm.KillSession(context.Background(), "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := tm.InstallHook(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tmuxccy.SingleAttachHookFor("private") == tmuxccy.SingleAttachHook() || !strings.Contains(tmuxccy.SingleAttachHookFor("private"), "tmux -L private list-clients") {
		t.Fatal("the hook names its own socket")
	}
	if !strings.Contains(tmuxccy.SingleAttachHook(), "tmux -L ccy list-clients") {
		t.Fatal("the default hook is ccy's")
	}
}

// typeAndSee types text and Enter into a pane running cat and waits for it to be echoed back.
func typeAndSee(t *testing.T, tm *tmuxccy.Tmux, name, text string) {
	t.Helper()
	ctx := context.Background()
	if err := tm.SendKeys(ctx, name, text); err != nil {
		t.Fatal(err)
	}
	if err := tm.SendEnter(ctx, name); err != nil {
		t.Fatal(err)
	}
	var screen string
	for range 50 {
		var err error
		if screen, err = tm.CapturePane(ctx, name, true); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(screen, text) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("screen %q", screen)
}

// The suites' fakes only prove the argv; a real tmux on a private socket proves the targets
// are accepted, and that the bare =name form is refused for pane commands.
func TestRealTmuxAcceptsTheTargetsAndRefusesTheBareForm(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	t.Setenv("TMUX_TMPDIR", t.TempDir())
	t.Setenv("TMUX", "")
	ctx := context.Background()
	tm := newTmux(execx.OS{})
	tm.Socket = "ccy-wave1-test"
	t.Cleanup(func() {
		if _, err := tm.Run(context.Background(), "kill-server"); err != nil && !strings.Contains(err.Error(), "no server running") {
			t.Logf("kill-server: %v", err)
		}
	})
	dir := t.TempDir()
	if _, err := tm.Run(ctx, "new-session", "-d", "-s", "alpha", "-c", dir, "-x", "100", "-y", "20", "cat"); err != nil {
		t.Fatal(err)
	}
	panes, err := tm.Panes(ctx)
	if err != nil || len(panes) != 1 || panes[0].Session != "alpha" || panes[0].PID <= 0 {
		t.Fatalf("panes %+v, %v", panes, err)
	}
	typeAndSee(t, tm, "alpha", "hello from go")
	if _, bareErr := tm.Run(ctx, "send-keys", "-t", "=alpha", "x"); bareErr == nil {
		t.Fatal("a bare =name is refused for a pane command, and that is why the colon is there")
	}
	sessions, err := tm.List(ctx)
	if err != nil || len(sessions) != 1 || sessions[0].Path != dir {
		t.Fatalf("sessions %+v, %v", sessions, err)
	}
	if err = tm.KillSession(ctx, "alpha"); err != nil {
		t.Fatal(err)
	}
	if err = tm.KillSession(ctx, "alpha"); !errors.Is(err, tmuxccy.ErrSessionGone) {
		t.Fatalf("killing it twice: %v", err)
	}
}
