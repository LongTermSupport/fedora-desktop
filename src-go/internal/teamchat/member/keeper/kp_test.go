package keeper

import (
	"errors"
	"syscall"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const claudePID = 5000

// claudeWorld is a running --claude launch under a desired membership of the given seats.
func claudeWorld(t *testing.T, seats ...string) (*world, *fakeLink, *child, <-chan term.ExitCode) {
	t.Helper()
	w := newWorld(t, claudeLaunch...)
	for _, s := range seats {
		w.seat(s[:len(s)-2])
	}
	w.writeDesired(desiredJSON(1, state.InboundAccept, seats...))
	l, c, res := w.running(len(seats))
	return w, l, c, res
}

// ended is the end the link was sent; the link then exits and the keeper with want.
func ended(t *testing.T, l *fakeLink, res <-chan term.ExitCode, how string, status int, want term.ExitCode) {
	t.Helper()
	end := l.expect(t, wire.KindEnd)
	if end.End.How != how || end.End.Status != status {
		t.Fatalf("end %+v, want %s %d", end.End, how, status)
	}
	l.code(0)
	exitOf(t, res, want)
}

// KP1, T21: a stop reaches Claude Code by its pidfd, once; INT is the terminal's and ignored;
// at 5 s both are sent TERM and at 7 s KILL.
func TestStopReachesClaudeByItsPidfd(t *testing.T) {
	w, l, c, res := claudeWorld(t)
	cc := w.claude(l)
	w.sigs <- syscall.SIGINT
	w.logged("signal interrupt ignored")
	w.sigs <- syscall.SIGTERM
	expectSig(t, cc.sigs, syscall.SIGTERM)
	w.sigs <- syscall.SIGHUP
	w.logged("the stop is already under way")
	c.quiet(t)
	w.advance(stopTermAfter, stopKillAfter)
	expectSig(t, cc.sigs, syscall.SIGTERM)
	expectSig(t, c.sigs, syscall.SIGTERM)
	w.advance(stopKillAfter - stopTermAfter)
	expectSig(t, cc.sigs, syscall.SIGKILL)
	expectSig(t, c.sigs, syscall.SIGKILL)
	ended(t, l, res, wire.EndStop, 137, 137)
}

// KP1: without Claude Code's pidfd the stop goes to CMD, which may end on it.
func TestStopReachesCmd(t *testing.T) {
	cases := map[string]func(w *world, l *fakeLink, c *child){
		"plain": func(*world, *fakeLink, *child) {},
		"claude is CMD": func(w *world, l *fakeLink, c *child) {
			l.send(t, wire.Msg{Kind: wire.KindClaude, Claude: &state.Process{PID: c.pid, StartTicks: int64(c.pid) * 10}})
			w.logged("Claude Code is CMD")
		},
		"another start time": func(w *world, l *fakeLink, _ *child) {
			w.proc.set(claudePID, 1)
			l.send(t, wire.Msg{Kind: wire.KindClaude, Claude: &state.Process{PID: claudePID, StartTicks: 2}})
			w.logged("is not the process the link named")
		},
		"no pidfd": func(w *world, l *fakeLink, _ *child) {
			w.pidfds.mu.Lock()
			w.pidfds.err = errors.New("no pidfd")
			w.pidfds.mu.Unlock()
			l.send(t, wire.Msg{Kind: wire.KindClaude, Claude: &state.Process{PID: claudePID, StartTicks: 1}})
			w.logged("open a pidfd on Claude Code")
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			w, l, c, res := claudeWorld(t)
			setup(w, l, c)
			w.sigs <- syscall.SIGTERM
			expectSig(t, c.sigs, syscall.SIGTERM)
			c.code(143)
			ended(t, l, res, wire.EndStop, 143, 143)
		})
	}
}

// KP1: a second report of Claude Code is ignored; the first pidfd stays the one.
func TestSecondClaudeIgnored(t *testing.T) {
	w, l, c, res := claudeWorld(t)
	cc := w.claude(l)
	l.send(t, wire.Msg{Kind: wire.KindClaude, Claude: &state.Process{PID: claudePID + 1, StartTicks: 1}})
	w.logged("named Claude Code again")
	cc.exit()
	w.logged("Claude Code exited")
	c.code(0)
	ended(t, l, res, wire.EndExit, 0, 0)
	if w.pidfds.get(claudePID+1) != nil {
		t.Fatal("a pidfd was opened on the second report")
	}
}

// KP1: Claude Code outliving CMD is sent TERM at 3 s and KILL 5 s later; the end is CMD's.
func TestClaudeOutlivingCmd(t *testing.T) {
	w, l, c, res := claudeWorld(t)
	cc := w.claude(l)
	c.code(0)
	w.advance(orphanTerm)
	expectSig(t, cc.sigs, syscall.SIGTERM)
	w.advance(orphanKill)
	expectSig(t, cc.sigs, syscall.SIGKILL)
	ended(t, l, res, wire.EndExit, 0, 0)
}

// KP1: CMD outliving Claude Code is ended the same way; a KILL the keeper sent is an exit.
func TestCmdOutlivingClaude(t *testing.T) {
	w, l, c, res := claudeWorld(t)
	cc := w.claude(l)
	cc.exit()
	w.advance(orphanTerm)
	expectSig(t, c.sigs, syscall.SIGTERM)
	w.advance(orphanKill)
	expectSig(t, c.sigs, syscall.SIGKILL)
	ended(t, l, res, wire.EndExit, 137, 137)
}

// A stop while the link comes up kills it, and the keeper ends with 128+n, its run removed.
func TestStopWhileLaunching(t *testing.T) {
	w := newWorld(t, plainLaunch...)
	res := w.start()
	l := w.link()
	l.expect(t, wire.KindLaunch)
	w.sigs <- syscall.SIGTERM
	expectSig(t, l.sigs, syscall.SIGKILL)
	exitOf(t, res, 128+term.ExitCode(syscall.SIGTERM))
	if !w.runGone() || len(w.starter.Starts()) != 1 {
		t.Fatal("the stopped launch left its run or started CMD")
	}
}

// A link whose start fails at launch fails the launch, its run kept for link.log.
func TestLinkThatCannotStart(t *testing.T) {
	w := newWorld(t, plainLaunch...)
	w.linkErr = errors.New("no link")
	exitOf(t, w.start(), exits.ExitConfig)
	if !contains(w.log(), "start the link: no link") {
		t.Fatalf("keeper.log %q", w.log())
	}
}

// KP2: each crash waits 1 s << (n-1) with keeper.json restarting; the sixth is a crash loop,
// keeper.json failed and the seat let go (T23); CMD runs on.
func TestLinkRestartsWithBackoff(t *testing.T) {
	w, l, c, res := claudeWorld(t, "alpha/1")
	alpha := w.layout.Seat(mustTeam(t, "alpha"))
	for n := int64(1); n < failedAfter; n++ {
		l.code(1)
		delay := time.Second << (n - 1)
		mustDo(t, w.clk.AwaitDue(t.Context(), delay))
		k := w.keeperJSON()
		if k.Link != state.LinkRestarting || k.Restarts != n || k.NextMS == nil || *k.NextMS != (w.clk.Boot()+delay).Milliseconds() {
			t.Fatalf("crash %d: keeper.json %+v", n, k)
		}
		w.clk.Advance(delay)
		l = w.up(1)
		l.expect(t, wire.KindPID)
	}
	l.code(1)
	w.logged("the link failed: crash-loop")
	if k := w.keeperJSON(); k.Link != state.LinkFailed || k.Why == nil || *k.Why != state.WhyCrashLoop || k.NextMS != nil {
		t.Fatalf("keeper.json %+v", k)
	}
	// The failure is logged before the seat locks close, on the keeper's loop.
	w.eventually("a failed link's seat let go", func() bool { return !w.held(alpha) })
	c.code(0)
	exitOf(t, res, term.ExitOK)
	if n := len(w.starter.Starts()); n != 1+failedAfter {
		t.Fatalf("%d starts", n)
	}
}

// KP2: exit 78 is a config failure at once; a link that ran 10 min starts its count afresh;
// a start that fails counts as an exit.
func TestLinkSupervision(t *testing.T) {
	t.Run("config", func(t *testing.T) {
		w, l, c, res := claudeWorld(t)
		l.code(int(exits.ExitConfig))
		w.logged("the link failed: config")
		if k := w.keeperJSON(); k.Why == nil || *k.Why != state.WhyConfig {
			t.Fatalf("keeper.json %+v\n%s", k, w.log())
		}
		c.code(0)
		exitOf(t, res, term.ExitOK)
	})
	t.Run("stable run resets", func(t *testing.T) {
		w, l, c, res := claudeWorld(t)
		l.code(1)
		w.advance(time.Second)
		l = w.up(0)
		l.expect(t, wire.KindPID)
		w.clk.Advance(stableRun)
		l.code(1)
		mustDo(t, w.clk.AwaitDue(t.Context(), time.Second))
		if k := w.keeperJSON(); k.Restarts != 1 {
			t.Fatalf("keeper.json %+v", k)
		}
		w.clk.Advance(time.Second)
		l = w.up(0)
		l.expect(t, wire.KindPID)
		c.code(0)
		ended(t, l, res, wire.EndExit, 0, 0)
	})
	t.Run("start fails", func(t *testing.T) {
		w, l, c, res := claudeWorld(t)
		w.mu.Lock()
		w.linkErr = errors.New("no link")
		w.mu.Unlock()
		l.code(1)
		w.advance(time.Second)
		w.logged("restart the link: no link")
		w.mu.Lock()
		w.linkErr = nil
		w.mu.Unlock()
		mustDo(t, w.clk.AwaitDue(t.Context(), 2*time.Second))
		if k := w.keeperJSON(); k.Restarts != 2 {
			t.Fatalf("keeper.json %+v", k)
		}
		w.clk.Advance(2 * time.Second)
		l = w.up(0)
		l.expect(t, wire.KindPID)
		c.code(0)
		ended(t, l, res, wire.EndExit, 0, 0)
	})
	t.Run("not ready while running", func(t *testing.T) {
		w, l, c, res := claudeWorld(t)
		l.code(1)
		w.advance(time.Second)
		l = w.link()
		l.expect(t, wire.KindLaunch)
		w.advance(readyWait)
		expectSig(t, l.sigs, syscall.SIGKILL)
		mustDo(t, w.clk.AwaitDue(t.Context(), 2*time.Second))
		if k := w.keeperJSON(); k.Restarts != 2 {
			t.Fatalf("keeper.json %+v", k)
		}
		c.code(0)
		l = w.link()
		l.expect(t, wire.KindLaunch)
		ended(t, l, res, wire.EndExit, 0, 0)
	})
}

// KP3: the end's kind: restart for a ccy CMD's 75, killed for a signal the keeper did not send.
func TestEndKinds(t *testing.T) {
	t.Run("restart", func(t *testing.T) {
		w := newWorld(t, "--plain", "--kind", "ccy", "--desired", "DESIRED", "--", "sleep", "60")
		w.writeDesired(desiredJSON(1, state.InboundAccept))
		p := w.startPlain(w.config())
		if k := w.keeperJSON(); k.Session == nil || k.Session.String() != "sess-a" {
			t.Fatalf("keeper.json %+v", k)
		}
		p.c.code(int(exits.ExitBusy))
		ended(t, p.l, p.res, wire.EndRestart, 75, 75)
	})
	t.Run("killed", func(t *testing.T) {
		w := newWorld(t, plainLaunch...)
		p := w.startPlain(w.config())
		p.c.exit(execx.ExitStatus{Code: 137, Signal: syscall.SIGKILL})
		ended(t, p.l, p.res, wire.EndKilled, 137, 137)
	})
}

// KP3: a link that does not exit 5 s after the end is killed; the seat locks close only once
// it has exited, and the run is removed.
func TestEndWaitsForTheLink(t *testing.T) {
	w, l, c, res := claudeWorld(t, "alpha/1")
	alpha := w.layout.Seat(mustTeam(t, "alpha"))
	c.code(0)
	l.expect(t, wire.KindEnd)
	if !w.held(alpha) {
		t.Fatal("the seat was let go before the link exited")
	}
	w.advance(endWait)
	expectSig(t, l.sigs, syscall.SIGKILL)
	exitOf(t, res, term.ExitOK)
	if w.held(alpha) || !w.runGone() {
		t.Fatal("the seat or the run outlived the keeper")
	}
}

// KP3: a link waiting to restart at the end is started end-only: launch, the adds, the end.
func TestEndOnlyStart(t *testing.T) {
	w, l, c, res := claudeWorld(t, "alpha/1")
	l.code(1)
	mustDo(t, w.clk.AwaitDue(t.Context(), time.Second))
	c.code(0)
	l = w.link()
	if m := l.expect(t, wire.KindLaunch); m.Launch.Seats != 1 {
		t.Fatalf("launch %+v", m.Launch)
	}
	l.expect(t, wire.KindAdd)
	l.send(t, wire.Msg{Kind: wire.KindReady, Ready: &wire.Ready{}})
	ended(t, l, res, wire.EndExit, 0, 0)
}

// RN3: CMD ending before Claude Code's first SessionStart is a launch failure: each seat's
// lock record has launch_failed_ms, its report why launch-failed, the end launch-failed, 78.
func TestLaunchFailed(t *testing.T) {
	w := newWorld(t, claudeLaunch...)
	alpha := w.seat("alpha")
	w.writeDesired(desiredJSON(1, state.InboundAccept, "alpha/1"))
	res := w.start()
	l := w.up(1)
	c := <-w.cmds
	l.expect(t, wire.KindPID)
	l.state(t, "alpha", "repo.1+desk.host")
	w.eventually("alpha's report", func() bool { return exists(t, alpha.Report()) })
	c.code(1)
	ended(t, l, res, wire.EndLaunchFailed, 1, exits.ExitConfig)
	if r := w.lockRecord(alpha); r.LaunchFailedMS == nil || *r.LaunchFailedMS != wallStart {
		t.Fatalf("lock record %+v", r)
	}
	if r := w.report(alpha); r.Why == nil || *r.Why != state.WhyLaunchFailed {
		t.Fatalf("report %+v", r)
	}
}

// A plain CMD's end before any SessionStart is no launch failure.
func TestPlainEndIsNoLaunchFailure(t *testing.T) {
	w := newWorld(t, plainLaunch...)
	w.startPlain(w.config()).finish(4)
}
