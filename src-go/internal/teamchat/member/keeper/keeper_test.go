package keeper

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// claudeLaunch is a host launch of Claude Code under the world's desired membership.
var claudeLaunch = []string{"--claude", "--kind", "host", "--desired", "DESIRED", "--", "claude"}

// A launch holds each desired seat, tells the link how many adds follow, starts CMD only once
// the link is ready, and ends with CMD's status once CMD has exited (RN1, n3c-D24).
func TestLaunchHoldsSeatsAndRunsCmd(t *testing.T) {
	w := newWorld(t, claudeLaunch...)
	alpha, beta := w.seat("alpha"), w.seat("beta")
	w.writeDesired(desiredJSON(3, state.InboundAccept, "alpha/1", "beta/2"))
	res := w.start()
	l := w.link()
	m := l.expect(t, wire.KindLaunch)
	if m.Launch.Seats != 2 || m.Launch.Generation != 3 || m.Launch.Inbound != state.InboundAccept {
		t.Fatalf("launch %+v", m.Launch)
	}
	l.expect(t, wire.KindAdd)
	l.expect(t, wire.KindAdd)
	if !w.held(alpha) || !w.held(beta) {
		t.Fatal("a desired seat is not held")
	}
	if len(w.starter.Starts()) != 1 {
		t.Fatal("CMD started before the link was ready")
	}
	l.send(t, wire.Msg{Kind: wire.KindReady, Ready: &wire.Ready{}})
	c := <-w.cmds
	l.expect(t, wire.KindPID)
	w.sessionStarted(l)
	launched(t, w, c, alpha)
	c.code(0)
	end := l.expect(t, wire.KindEnd)
	if end.End.How != wire.EndExit || end.End.Status != 0 {
		t.Fatalf("end %+v", end.End)
	}
	l.code(0)
	exitOf(t, res, term.ExitOK)
	if !w.runGone() || w.held(alpha) || w.held(beta) {
		t.Fatal("the run or a seat outlived the keeper")
	}
	if w.stdout.String() != "" || w.stderr.String() != "" {
		t.Fatalf("the keeper wrote to the terminal: %q %q", w.stdout.String(), w.stderr.String())
	}
}

// A desired seat another session holds fails the launch with 75, naming the seat; no link and
// no CMD are started (RN2).
func TestHeldSeatFailsTheLaunch(t *testing.T) {
	w := newWorld(t, claudeLaunch...)
	alpha := w.seat("alpha")
	w.writeDesired(desiredJSON(1, state.InboundAccept, "alpha/1"))
	holder := holdSeat(t, w, alpha, "")
	exitOf(t, w.start(), exits.ExitBusy)
	holder.release(t)
	if len(w.starter.Starts()) != 0 {
		t.Fatal("a link or CMD was started")
	}
	if !strings.Contains(w.stderr.String(), `"seat-held"`) || !strings.Contains(w.stderr.String(), "alpha") {
		t.Fatalf("stderr %q", w.stderr.String())
	}
	if !w.runGone() {
		t.Fatal("the run was left")
	}
}

// A desired seat held by a session whose keeper is gone (its link still ending) is waited for,
// up to the link's end bound and a margin, and taken once it is let go; one whose keeper
// lives fails at once.
func TestHeldSeatOfAnEndingSessionIsWaitedFor(t *testing.T) {
	const holderRun = "1111222233334444"
	t.Run("keeper gone", func(t *testing.T) {
		w := newWorld(t, claudeLaunch...)
		alpha := w.seat("alpha")
		w.writeDesired(desiredJSON(1, state.InboundAccept, "alpha/1"))
		holder := holdSeat(t, w, alpha, holderRun)
		res := w.start()
		w.eventually("the run is made", func() bool {
			entries, err := os.ReadDir(w.layout.RunRoot())
			return err == nil && slices.ContainsFunc(entries, func(e os.DirEntry) bool {
				id, perr := paths.ParseRunID(e.Name())
				return perr == nil && e.Name() != holderRun && exists(t, w.layout.Run(id).KeeperLog())
			})
		})
		w.logged("waiting for it to end")
		holder.release(t)
		l := w.up(1)
		c := w.cmd()
		l.expect(t, wire.KindPID)
		w.sessionStarted(l)
		if !w.held(alpha) {
			t.Fatal("the seat was not taken")
		}
		c.code(0)
		ended(t, l, res, wire.EndExit, 0, 0)
	})
	t.Run("keeper alive", func(t *testing.T) {
		w := newWorld(t, claudeLaunch...)
		alpha := w.seat("alpha")
		w.writeDesired(desiredJSON(1, state.InboundAccept, "alpha/1"))
		holder := holdSeat(t, w, alpha, holderRun)
		rr := w.layout.RunRoot()
		mustDo(t, os.MkdirAll(rr+"/"+holderRun, 0o700))
		keeper := holdFile(t, rr, rr+"/"+holderRun+"/keeper.lock")
		exitOf(t, w.start(), exits.ExitBusy)
		keeper.release(t)
		holder.release(t)
	})
}

// launched checks a running launch: CMD's argv and environment, keeper.json and the lock record.
func launched(t *testing.T, w *world, c *child, alpha paths.Seat) {
	t.Helper()
	run := w.runDir()
	if !hasArgs(c.cmd, "claude", "--plugin-dir", w.kit.PluginDir(), "--settings", run.Settings()) {
		t.Fatalf("CMD argv %q", c.cmd.Line())
	}
	if v := c.env("TEAMCHAT_RUNTIME"); v != run.Dir() {
		t.Fatalf("TEAMCHAT_RUNTIME %q", v)
	}
	if k := w.keeperJSON(); k.Link != state.LinkRunning || k.Desired != state.DesiredApplied {
		t.Fatalf("keeper.json %+v", k)
	}
	if r := w.lockRecord(alpha); r.Run.String() != run.Dir()[len(run.Dir())-16:] || r.StartedMS != wallStart {
		t.Fatalf("lock record %+v", r)
	}
}
