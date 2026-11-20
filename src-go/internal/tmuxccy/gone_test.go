package tmuxccy_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

// What a runner that starts tmux in an LXC container through lxc-attach, runuser and env says
// when tmux was never reached: none of it is tmux's own no-server or no-session text, so none is
// read as an empty server or a session gone (SPEC-team-close TC12, the B.13 review's finding 4).
var notTmux = []execx.Result{
	{ExitCode: 1, Stderr: []byte("lxc-attach: box: attach.c: lxc_attach: 1100 No such file or directory - Failed to get init pid")},
	{ExitCode: 127, Stderr: []byte("runuser: failed to execute tmux: No such file or directory")},
	{ExitCode: 127, Stderr: []byte("env: 'tmux': No such file or directory")},
	{ExitCode: 1, Stderr: []byte("runuser: user agent does not exist or the user entry does not contain all the required fields")},
	{ExitCode: 127, Stderr: []byte("error connecting to /run/x/tmux.sock (No such file or directory)")},
	{ExitCode: 1, Stderr: []byte("error connecting to /run/x/tmux.sock (No such file or directory)\nlxc-attach: box: exec failed")},
}

func TestListReadsOnlyTmuxsOwnNoServerAsEmpty(t *testing.T) {
	for _, res := range notTmux {
		f := &execx.Fake{}
		f.On([]string{"tmux"}, res)
		tm := newTmux(f)
		tm.SocketPath = "/run/x/tmux.sock"
		if got, err := tm.List(context.Background()); err == nil {
			t.Errorf("%q (exit %d): read as an empty server %v", res.Stderr, res.ExitCode, got)
		}
		if _, err := tm.Panes(context.Background()); err == nil {
			t.Errorf("%q (exit %d): panes read as an empty server", res.Stderr, res.ExitCode)
		}
		if _, err := tm.Clients(context.Background(), "alpha"); err == nil || errors.Is(err, tmuxccy.ErrSessionGone) {
			t.Errorf("%q (exit %d): clients read as the session gone: %v", res.Stderr, res.ExitCode, err)
		}
		if err := tm.KillSession(context.Background(), "alpha"); err == nil || errors.Is(err, tmuxccy.ErrSessionGone) {
			t.Errorf("%q (exit %d): kill read as the session gone: %v", res.Stderr, res.ExitCode, err)
		}
	}
	for _, msg := range []string{
		"no server running on /run/x/tmux.sock",
		"error connecting to /run/x/tmux.sock (No such file or directory)",
	} {
		f := &execx.Fake{}
		f.On([]string{"tmux"}, fail(msg))
		tm := newTmux(f)
		tm.SocketPath = "/run/x/tmux.sock"
		if got, err := tm.List(context.Background()); err != nil || len(got) != 0 {
			t.Errorf("%q: got %v, %v", msg, got, err)
		}
	}
}

// A pane command's answer is the session gone only in tmux's own words with exit 1; the same
// words from lxc-attach, runuser or env are a failure, never the session gone.
func TestCapturePaneIsGoneOnlyOnTmuxsOwnAnswer(t *testing.T) {
	for _, msg := range []string{"can't find pane: alpha", "can't find session: alpha", "can't find window: 0", "no server running on /run/x/tmux.sock"} {
		f := &execx.Fake{}
		f.On([]string{"tmux"}, fail(msg))
		if _, err := newTmux(f).CapturePane(context.Background(), "alpha", true); !errors.Is(err, tmuxccy.ErrSessionGone) {
			t.Errorf("%q: got %v, want the session gone", msg, err)
		}
	}
	for _, res := range notTmux {
		f := &execx.Fake{}
		f.On([]string{"tmux"}, res)
		_, err := newTmux(f).CapturePane(context.Background(), "alpha", true)
		if err == nil || errors.Is(err, tmuxccy.ErrSessionGone) {
			t.Errorf("%q (exit %d): got %v, want a failure that is not the session gone", res.Stderr, res.ExitCode, err)
		}
	}
}

// Submit takes a line as entered when the pane is gone after its Enter; an attach failure
// that only mentions a missing file is not that, and is said.
func TestSubmitDoesNotTakeAnAttachFailureAsTheSessionGone(t *testing.T) {
	var slept []time.Duration
	for _, res := range notTmux {
		f := &execx.Fake{}
		f.On([]string{"tmux", "-L", "ccy", "capture-pane"}, res)
		f.On([]string{"tmux", "-L", "ccy", "send-keys"}, ok(""))
		if err := newTmux(f).Submit(context.Background(), "alpha", "/exit", pace(&slept)); err == nil {
			t.Errorf("%q (exit %d): taken as submitted", res.Stderr, res.ExitCode)
		}
	}
}
