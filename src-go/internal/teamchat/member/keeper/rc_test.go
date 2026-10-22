package keeper

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

const handleA = "repo.1+desk.host"

// agentOf is an agent's user id on team's server.
func agentOf(t *testing.T, team string) string {
	t.Helper()
	return "@" + handleA + ":" + paths.ServerName(mustTeam(t, team))
}

// reported is the link's report of alpha, waited for until its report.json is written.
func (w *world) reported(l *fakeLink, s paths.Seat) {
	w.t.Helper()
	l.state(w.t, "alpha", handleA)
	w.eventually("alpha's report", func() bool { return exists(w.t, s.Report()) })
}

// RC1: a desired file that cannot be read keeps the last good membership: keeper.json and the
// report say refused with the code, and the link is told; the seat stays held.
func TestDesiredRefused(t *testing.T) {
	cases := map[string]struct {
		change func(w *world)
		code   string
	}{
		"missing": {func(w *world) { mustDo(t, os.Remove(w.desired+"/desired.json")) }, state.CodeMissing},
		"untrusted": {func(w *world) {
			w.writeDesired(desiredJSON(2, state.InboundAccept))
			mustDo(t, os.Chmod(w.desired+"/desired.json", 0o662))
		}, state.CodeMissing},
		"undecodable": {func(w *world) { w.writeDesired(`{"v":1}`) }, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w, l, c, res := claudeWorld(t, "alpha/1")
			alpha := w.layout.Seat(mustTeam(t, "alpha"))
			w.reported(l, alpha)
			tc.change(w)
			w.changed()
			d := l.expect(t, wire.KindDesired).Desired
			if d.State != state.DesiredRefused || d.Code == nil || d.Generation != 1 ||
				(tc.code != "" && *d.Code != tc.code) || (tc.code == "" && *d.Code == state.CodeMissing) {
				t.Fatalf("desired %+v", d)
			}
			if k := w.keeperJSON(); k.Desired != state.DesiredRefused {
				t.Fatalf("keeper.json %+v", k)
			}
			if r := w.report(alpha); r.Desired.State != state.DesiredRefused || r.Desired.Code == nil {
				t.Fatalf("report %+v", r)
			}
			if !w.held(alpha) {
				t.Fatal("the last good membership's seat was let go")
			}
			c.code(0)
			ended(t, l, res, wire.EndExit, 0, 0)
		})
	}
}

// RC1: an overflow re-reads the file; an event about another name does not; a watch that
// ends is logged.
func TestDesiredEvents(t *testing.T) {
	w, l, c, res := claudeWorld(t)
	w.watch.events <- WatchEvent{Name: ".desired.tmp", Op: unix.IN_CREATE}
	w.writeDesired(desiredJSON(2, state.InboundAccept))
	w.watch.events <- WatchEvent{Op: unix.IN_Q_OVERFLOW}
	if d := l.expect(t, wire.KindDesired).Desired; d.Generation != 2 || d.State != state.DesiredApplied {
		t.Fatalf("desired %+v", d)
	}
	w.logged("generation 2 applied", 1)
	close(w.watch.events)
	w.logged("no longer watched")
	c.code(0)
	ended(t, l, res, wire.EndExit, 0, 0)
	if n := strings.Count(w.stderr.String()+w.stdout.String(), "desired"); n != 0 {
		t.Fatal("the keeper wrote to the terminal")
	}
}

// RC1, LN8: a seat taken out with farewell released owes its leave notice: the lock is held
// until the link has left; the removed user id goes with a later add of the team (n3c-D13).
func TestRemoveReleased(t *testing.T) {
	w, l, c, res := claudeWorld(t, "alpha/1")
	alpha := w.layout.Seat(mustTeam(t, "alpha"))
	w.writeDesired(desiredFarewell(2, "alpha", "1", state.FarewellReleased))
	w.changed()
	l.expect(t, wire.KindDesired)
	rm := l.expect(t, wire.KindRemove).Remove
	if rm.Team.String() != "alpha" || rm.Farewell == nil || *rm.Farewell != state.FarewellReleased ||
		!strings.HasPrefix(rm.Txn, "tc") || len(rm.Txn) != 34 {
		t.Fatalf("remove %+v", rm)
	}
	l.send(t, wire.Msg{Kind: wire.KindRemoved, Removed: &wire.Removed{Team: rm.Team, UserID: agentOf(t, "alpha")}})
	w.logged("its leave notice is owed")
	if !w.held(alpha) {
		t.Fatal("the lock was let go with the notice owed")
	}
	l.send(t, wire.Msg{Kind: wire.KindLeft, Left: &wire.Left{Team: rm.Team}})
	w.logged("seat alpha released")
	if w.held(alpha) {
		t.Fatal("the seat is held after the link left")
	}
	w.writeDesired(desiredJSON(3, state.InboundAccept, "alpha/1"))
	w.changed()
	l.expect(t, wire.KindDesired)
	if a := l.expect(t, wire.KindAdd).Add; !slices.Equal(a.RemovedUserIDs, []string{agentOf(t, "alpha")}) {
		t.Fatalf("add %+v", a)
	}
	c.code(0)
	ended(t, l, res, wire.EndExit, 0, 0)
}

// LN8: an owed notice is handed to a new link, and dropped after 30 s.
func TestOwedNotice(t *testing.T) {
	w, l, c, res := claudeWorld(t, "alpha/1")
	alpha := w.layout.Seat(mustTeam(t, "alpha"))
	w.writeDesired(desiredFarewell(2, "alpha", "1", state.FarewellReleased))
	w.changed()
	l.expect(t, wire.KindDesired)
	rm := l.expect(t, wire.KindRemove).Remove
	l.send(t, wire.Msg{Kind: wire.KindRemoved, Removed: &wire.Removed{Team: rm.Team, UserID: agentOf(t, "alpha")}})
	w.logged("its leave notice is owed")
	l.code(1)
	w.advance(2, time.Second)
	l = w.link()
	if m := l.expect(t, wire.KindLaunch); m.Launch.Seats != 0 {
		t.Fatalf("launch %+v", m.Launch)
	}
	if o := l.expect(t, wire.KindOwed).Owed; o.Team != rm.Team || o.Txn != rm.Txn {
		t.Fatalf("owed %+v", o)
	}
	l.expect(t, wire.KindPID)
	w.ready(l)
	w.advance(1, owedWait-time.Second)
	w.logged("the leave notice was not posted in time")
	if w.held(alpha) {
		t.Fatal("the seat is held after the notice's 30 s")
	}
	c.code(0)
	ended(t, l, res, wire.EndExit, 0, 0)
}

// LN8: a seat taken out for no reason is let go once the link has removed it.
func TestRemoveWithoutFarewell(t *testing.T) {
	w, l, c, res := claudeWorld(t, "alpha/1")
	alpha := w.layout.Seat(mustTeam(t, "alpha"))
	w.writeDesired(desiredJSON(2, state.InboundAccept))
	w.changed()
	l.expect(t, wire.KindDesired)
	if rm := l.expect(t, wire.KindRemove).Remove; rm.Farewell != nil {
		t.Fatalf("remove %+v", rm)
	}
	l.send(t, wire.Msg{Kind: wire.KindRemoved, Removed: &wire.Removed{Team: mustTeam(t, "alpha"), UserID: agentOf(t, "alpha")}})
	w.logged("seat alpha released")
	if w.held(alpha) {
		t.Fatal("the removed seat is held")
	}
	c.code(0)
	ended(t, l, res, wire.EndExit, 0, 0)
}

// LN8: a remove unanswered in 10 s kills the link; the keeper deletes the store under the lock
// and restarts the link at once, uncounted.
func TestRemoveTimeout(t *testing.T) {
	w, l, c, res := claudeWorld(t, "alpha/1")
	alpha := w.layout.Seat(mustTeam(t, "alpha"))
	mustDo(t, os.WriteFile(alpha.Store(), []byte("store"), 0o600))
	w.writeDesired(desiredJSON(2, state.InboundAccept))
	w.changed()
	l.expect(t, wire.KindDesired)
	l.expect(t, wire.KindRemove)
	w.advance(1, removeWait)
	expectSig(t, l.sigs, 9)
	l = w.up(0)
	l.expect(t, wire.KindPID)
	if exists(t, alpha.Store()) || w.held(alpha) {
		t.Fatal("the store or the seat outlived the unanswered remove")
	}
	if k := w.keeperJSON(); k.Restarts != 0 {
		t.Fatalf("keeper.json %+v", k)
	}
	c.code(0)
	ended(t, l, res, wire.EndExit, 0, 0)
}

// LN8: with the link restarting, the keeper deletes a removed seat's store itself; a hot add
// is held and given to the next link.
func TestChangesWhileRestarting(t *testing.T) {
	w, l, c, res := claudeWorld(t, "alpha/1")
	alpha, beta := w.layout.Seat(mustTeam(t, "alpha")), w.seat("beta")
	mustDo(t, os.WriteFile(alpha.Store(), []byte("store"), 0o600))
	l.code(1)
	mustDo(t, w.clk.BlockUntil(t.Context(), 1))
	w.writeDesired(desiredJSON(2, state.InboundAccept, "beta/1"))
	w.changed()
	// The removal is logged before the add takes its seat: wait for both.
	w.logged("seat alpha released")
	w.logged("seat beta/1 held")
	if exists(t, alpha.Store()) || w.held(alpha) || !w.held(beta) {
		t.Fatal("the removal or the add did not happen with no link")
	}
	w.clk.Advance(time.Second)
	l = w.up(1)
	l.expect(t, wire.KindPID)
	c.code(0)
	ended(t, l, res, wire.EndExit, 0, 0)
}

// RC1: a hot add of a seat another session holds is refused to the link with the holder's run;
// one whose holder cannot be named is only logged.
func TestBusyHotAdd(t *testing.T) {
	w, l, c, res := claudeWorld(t)
	beta, gamma := w.seat("beta"), w.seat("gamma")
	hb := holdSeat(t, w, beta, "1111222233334444")
	hg := holdSeat(t, w, gamma, "")
	w.writeDesired(desiredJSON(2, state.InboundAccept, "beta/1", "gamma/1"))
	w.changed()
	l.expect(t, wire.KindDesired)
	if r := l.expect(t, wire.KindRefused).Refused; r.Team.String() != "beta" || r.HolderRun.String() != "1111222233334444" {
		t.Fatalf("refused %+v", r)
	}
	w.logged("seat gamma is held by another session, whose run cannot be read")
	c.code(0)
	ended(t, l, res, wire.EndExit, 0, 0)
	hb.release(t)
	hg.release(t)
	if exists(t, beta.Report()) {
		t.Fatal("a refused seat has a report")
	}
}

// A seat's report follows the link's state of it; a refuse launch whose membership now accepts
// needs a relaunch.
func TestReports(t *testing.T) {
	w := newWorld(t, claudeLaunch...)
	alpha := w.seat("alpha")
	w.writeDesired(desiredJSON(1, state.InboundRefuse, "alpha/1"))
	l, c, res := w.running(1)
	w.reported(l, alpha)
	r := w.report(alpha)
	if r.Handle != handleA || r.Link != state.LinkRunning || r.NeedsRelaunch || r.StartedMS != wallStart {
		t.Fatalf("report %+v", r)
	}
	w.writeDesired(desiredJSON(2, state.InboundAccept, "alpha/1"))
	w.changed()
	l.expect(t, wire.KindDesired)
	if r := w.report(alpha); !r.NeedsRelaunch || r.Desired.Generation != 2 {
		t.Fatalf("report %+v", r)
	}
	c.code(0)
	ended(t, l, res, wire.EndExit, 0, 0)
}

// The link's messages the keeper does not expect are logged and change nothing; the token is
// kept in memory, handed to a new link and never logged (B11).
func TestLinkMessages(t *testing.T) {
	w, l, c, res := claudeWorld(t)
	l.state(t, "zeta", handleA)
	w.logged("which the session does not hold")
	l.send(t, wire.Msg{Kind: wire.KindRemoved, Removed: &wire.Removed{Team: mustTeam(t, "zeta"), UserID: agentOf(t, "zeta")}})
	w.logged("which the keeper is not removing")
	l.send(t, wire.Msg{Kind: wire.KindLeft, Left: &wire.Left{Team: mustTeam(t, "zeta")}})
	w.logged("which owes no notice")
	l.send(t, wire.Msg{Kind: wire.KindReady, Ready: &wire.Ready{}})
	w.logged("the link sent ready again")
	l.send(t, wire.Msg{Kind: wire.KindEnd, End: &wire.End{How: wire.EndExit}})
	w.logged("which the keeper does not take")
	const secret = "tok-0123456789abcdef"
	l.send(t, wire.Msg{Kind: wire.KindToken, Token: &wire.Token{Token: secret}})
	cc := w.claude(l)
	l.code(1)
	w.advance(1, time.Second)
	l = w.link()
	l.expect(t, wire.KindLaunch)
	l.expect(t, wire.KindPID)
	if m := l.expect(t, wire.KindClaude); m.Claude.PID != claudePID {
		t.Fatalf("claude %+v", m.Claude)
	}
	if m := l.expect(t, wire.KindToken); m.Token.Token != secret {
		t.Fatal("the new link was not given the token")
	}
	w.ready(l)
	if strings.Contains(w.log(), secret) {
		t.Fatal("keeper.log holds the token")
	}
	cc.exit()
	c.code(0)
	ended(t, l, res, wire.EndExit, 0, 0)
}
