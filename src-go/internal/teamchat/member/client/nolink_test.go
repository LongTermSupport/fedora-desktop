package client

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// noLink runs recv with no link answering and returns its error object; nothing waits, so the
// answer is the kernel's refusal and two file reads, never a sleep (LN3: within 300 ms).
func (w *world) noLink(verb string) exits.Error {
	w.t.Helper()
	w.stderr.Reset()
	if code := w.do(verb); code != exits.ExitNoRuntime {
		w.t.Fatalf("exit %d, stderr %q", code, w.stderr.String())
	}
	if w.clk.Boot() != boot0 || w.clk.Waiters() != 0 {
		w.t.Fatalf("the client waited: boot %v, %d waiters", w.clk.Boot(), w.clk.Waiters())
	}
	return w.errorObject()
}

// LN3: a restarting link is no-link with the restart time.
func TestNoLinkRestarting(t *testing.T) {
	w := newWorld(t)
	w.keeper()
	w.keeperJSON(state.LinkRestarting, state.KindCCY, new((boot0 + 4*time.Second + 200*time.Millisecond).Milliseconds()))
	e := w.noLink("recv")
	if e.Code != exits.CodeNoLink || !strings.Contains(e.Message, "next try in 5 s") {
		t.Fatalf("error %+v", e)
	}
}

// LN3 (the brief review's fourth answer): a running link not yet answering is no-link with no
// restart time.
func TestNoLinkRunning(t *testing.T) {
	w := newWorld(t)
	w.keeper()
	w.keeperJSON(state.LinkRunning, state.KindHost, nil)
	e := w.noLink("inbox")
	if e.Code != exits.CodeNoLink || strings.Contains(e.Message, "next try") {
		t.Fatalf("error %+v", e)
	}
	// keeper.json not written yet: the keeper is starting.
	w = newWorld(t)
	w.keeper()
	if e := w.noLink("status"); e.Code != exits.CodeNoLink {
		t.Fatalf("no keeper.json: %+v", e)
	}
}

// LN3: a failed link is link-failed with its kind's recovery, the session name filled in.
func TestLinkFailed(t *testing.T) {
	for _, tc := range []struct {
		kind, want string
	}{
		{state.KindCCY, "ask the owner to run `ccy-teams restart work-1`"},
		{state.KindLXC, "ask the owner to run `ccy-teams restart work-1`"},
		{state.KindHost, "end this session and launch it again"},
		{state.KindOther, "end this session and launch it again"},
	} {
		w := newWorld(t)
		w.keeper()
		w.keeperJSON(state.LinkFailed, tc.kind, nil)
		if e := w.noLink("recv"); e.Code != exits.CodeLinkFailed || !strings.Contains(e.Message, tc.want) {
			t.Fatalf("%s: error %+v", tc.kind, e)
		}
	}
}

// LN3: keeper.lock free is the keeper gone: link-failed, whatever keeper.json says.
func TestKeeperGone(t *testing.T) {
	w := newWorld(t)
	w.keeperJSON(state.LinkRestarting, state.KindLXC, new(int64(1)))
	if e := w.noLink("recv"); e.Code != exits.CodeLinkFailed || !strings.Contains(e.Message, "ccy-teams restart work-1") {
		t.Fatalf("error %+v", e)
	}
	w = newWorld(t)
	if e := w.noLink("recv"); e.Code != exits.CodeLinkFailed || !strings.Contains(e.Message, "launch it again") {
		t.Fatalf("no keeper.json: %+v", e)
	}
}

// LN3: a link that closes before its done is no link, and its records never reach stdout: the
// link returns them, unacknowledged.
func TestACloseBeforeDone(t *testing.T) {
	w := newWorld(t)
	w.keeper()
	w.keeperJSON(state.LinkRestarting, state.KindHost, new(boot0.Milliseconds()))
	l := w.listen(record1)
	if e := w.noLink("recv"); e.Code != exits.CodeNoLink || !strings.Contains(e.Message, "next try in 0 s") {
		t.Fatalf("error %+v", e)
	}
	l.request(t)
	if w.stdout.Len() != 0 {
		t.Fatalf("stdout %q", w.stdout.String())
	}
	// a line that is not a record line's end is the link's fault: internal.
	w = newWorld(t)
	w.listen(`{"done":{"exit":"0"}}`)
	if code := w.do("recv"); code != exits.ExitInternal {
		t.Fatalf("a malformed done: exit %d", code)
	}
}

// LN3: wait rides out a link restart with the same deadline: 1 s, then doubling to 5 s.
func TestWaitAcrossARestart(t *testing.T) {
	w := newWorld(t)
	w.keeper()
	w.keeperJSON(state.LinkRestarting, state.KindCCY, new(boot0.Milliseconds()))
	exit := w.async("wait", "--timeout", "60")
	for _, d := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		if err := w.clk.BlockUntil(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
		w.clk.Advance(d)
	}
	l := w.listen(record1, doneAck)
	if err := w.clk.BlockUntil(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	w.clk.Advance(5 * time.Second)
	if code := <-exit; code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.stderr.String())
	}
	if req := l.request(t); req.Op != wire.OpWait || req.DeadlineMS != (boot0+60*time.Second).Milliseconds() {
		t.Fatalf("request %+v", req)
	}
	if l.ack(t) != "0a1b2c3d" || w.stdout.String() != record1+"\n" {
		t.Fatalf("stdout %q", w.stdout.String())
	}
}

// LN3: no link for wait's whole timeout is no-link at the deadline, never a hang.
func TestWaitWithNoLinkForItsTimeout(t *testing.T) {
	w := newWorld(t)
	w.keeper()
	w.keeperJSON(state.LinkRunning, state.KindHost, nil)
	exit := w.async("wait", "--timeout", "4")
	for _, d := range []time.Duration{time.Second, 2 * time.Second, time.Second} {
		if err := w.clk.BlockUntil(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
		w.clk.Advance(d)
	}
	if code := <-exit; code != exits.ExitNoRuntime {
		t.Fatalf("exit %d", code)
	}
	if e := w.errorObject(); e.Code != exits.CodeNoLink || w.clk.Boot() != boot0+4*time.Second {
		t.Fatalf("error %+v at %v", e, w.clk.Boot())
	}
}

// LN3: a failed link ends wait at once.
func TestWaitWithAFailedLink(t *testing.T) {
	w := newWorld(t)
	w.keeper()
	w.keeperJSON(state.LinkFailed, state.KindHost, nil)
	if e := w.noLink("wait"); e.Code != exits.CodeLinkFailed {
		t.Fatalf("error %+v", e)
	}
}

// A link that answers no done by the deadline is cut off after the grace and is no link.
func TestALinkThatNeverAnswers(t *testing.T) {
	w := newWorld(t)
	w.keeper()
	w.keeperJSON(state.LinkRunning, state.KindHost, nil)
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: w.run.LinkSock(), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cerr := ln.Close(); cerr != nil {
			t.Error(cerr)
		}
	})
	exit := w.async("recv")
	c, err := ln.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := w.clk.BlockUntil(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	w.clk.Advance(2*time.Second + grace)
	if code := <-exit; code != exits.ExitNoRuntime {
		t.Fatalf("exit %d", code)
	}
}
