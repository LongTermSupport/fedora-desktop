package tmuxccy_test

import (
	"context"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func TestCurrentSessionIsAskedOfTmuxInsideTheCcyServer(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"tmux", "-L", "ccy", "display-message", "-p", "#S"}, ok("ccy-proj\n"))
	got, err := newTmux(f).CurrentSession(context.Background(), "/tmp/tmux-1000/ccy,4242,0")
	if err != nil || got != "ccy-proj" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestCurrentSessionOutsideCcyTmuxIsNone(t *testing.T) {
	for _, env := range []string{"", "/tmp/tmux-1000/default,1,0"} {
		f := &execx.Fake{}
		got, err := newTmux(f).CurrentSession(context.Background(), env)
		if err != nil || got != "" {
			t.Fatalf("%q: got %q, %v", env, got, err)
		}
		if len(f.Calls()) != 0 {
			t.Fatalf("%q: tmux was asked: %v", env, f.Calls())
		}
	}
}

func TestCurrentSessionThatTmuxRefusesIsAnError(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"tmux"}, fail("server exited unexpectedly"))
	_, err := newTmux(f).CurrentSession(context.Background(), "/tmp/tmux-1000/ccy,4242,0")
	if err == nil || !strings.Contains(err.Error(), "server exited unexpectedly") {
		t.Fatalf("got %v", err)
	}
}
