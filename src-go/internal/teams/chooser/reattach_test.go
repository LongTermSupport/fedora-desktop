package chooser

import (
	"context"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

// Issue 423: the re-attach a held seat offers. Outside tmux it attaches the running session to
// this terminal; inside ccy's tmux (cc chooses there) it switches this client to it; a session
// not running in ccy's tmux, or open in another terminal, is an error naming the way on.
func TestTmuxReattachAttachesOrSwitchesToTheRunningSession(t *testing.T) {
	ctx := context.Background()
	w := tmuxWorld(t)
	w.On("tmux -L ccy list-clients", "", 0)
	w.On("tmux -L ccy set-hook", "", 0)
	w.On("tmux -L ccy attach-session", "", 0)
	if err := TmuxReattach(w.Host.Tmux, w.Host.Getenv)(ctx, "ccy-proj"); err != nil {
		t.Fatal(err)
	}
	if !w.Fake.Called("tmux -L ccy attach-session -t =ccy-proj") {
		t.Fatalf("not attached: %v", w.Fake.Calls())
	}

	w = tmuxWorld(t)
	w.Env["TMUX"] = "/tmp/tmux-1000/ccy,1,0"
	w.On("tmux -L ccy switch-client", "", 0)
	if err := TmuxReattach(w.Host.Tmux, w.Host.Getenv)(ctx, "ccy-proj"); err != nil {
		t.Fatal(err)
	}
	if !w.Fake.Called("tmux -L ccy switch-client -t =ccy-proj") || w.Fake.Called("tmux -L ccy attach-session") {
		t.Fatalf("not switched: %v", w.Fake.Calls())
	}

	w = tmuxWorld(t)
	err := TmuxReattach(w.Host.Tmux, w.Host.Getenv)(ctx, "ccy-gone")
	if err == nil || !strings.Contains(err.Error(), "ccy-sessions restore ccy-gone") {
		t.Fatalf("a session not running: %v", err)
	}

	w = tmuxWorld(t)
	w.On("tmux -L ccy list-clients", "/dev/pts/9 100\n", 0)
	err = TmuxReattach(w.Host.Tmux, w.Host.Getenv)(ctx, "ccy-proj")
	if err == nil || !strings.Contains(err.Error(), "ccy-sessions takes it over") {
		t.Fatalf("a session open elsewhere: %v", err)
	}

	w = teamtest.New(t)
	w.OnErr("tmux -L ccy list-sessions", "boom", 1)
	if err = TmuxReattach(w.Host.Tmux, w.Host.Getenv)(ctx, "ccy-proj"); err == nil {
		t.Fatal("a tmux that cannot list its sessions re-attached")
	}
}

// tmuxWorld is a world whose ccy tmux runs one session, ccy-proj.
func tmuxWorld(t *testing.T) *teamtest.World {
	t.Helper()
	w := teamtest.New(t)
	w.On("tmux -L ccy list-sessions", "ccy-proj 0 "+w.Checkout()+"\n", 0)
	return w
}
