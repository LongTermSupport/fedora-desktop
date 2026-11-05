package link

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/engine"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/hooktext"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// hookDeadline is the hooks' 300 ms (wire.Deadline).
const hookDeadline = 300 * time.Millisecond

// hookWorld is a link ready with ids added, whose keeper has told the session's pid, and whose
// process table makes this test process a hook below Claude Code (101) below the session
// (100).
func newHookWorld(t *testing.T, ids ...paths.TeamID) *linkWorld {
	t.Helper()
	w := newLinkWorld(t)
	w.proc.add(os.Getpid(), pidClaude, 2000)
	w.proc.add(pidClaude, pidSession, 1001)
	w.proc.add(pidSession, 50, 1000)
	w.ready(ids...)
	w.send(wire.Msg{Kind: wire.KindPID, PID: &state.Process{PID: pidSession, StartTicks: 1000}}, nil)
	w.sync(1)
	return w
}

func (w *linkWorld) inboxSocket() string {
	w.t.Helper()
	sock, err := paths.InboxSocket(w.dir+"/xdg", pidClaude)
	check(w.t, err)
	return sock
}

// args are a usable payload of op's hook for session sessA.
func (w *linkWorld) args(op string) wire.HookArgs {
	id, transcript, sock, token := sessA, w.dir+"/.claude/projects/-work/"+sessA+".jsonl", w.inboxSocket(), heldToken
	a := wire.HookArgs{Payload: wire.PayloadOK, SessionID: &id, TranscriptPath: &transcript, Socket: &sock, Token: &token}
	switch op {
	case wire.OpHookSessionStart:
		source := state.SourceStartup
		a.Source = &source
	case wire.OpHookStop:
		a.StopHookFalse = true
	}
	return a
}

// hookCall is one hook request and its answer line ("" when the link closed without one) and
// done line.
func (w *linkWorld) hookCall(op string, a wire.HookArgs) (string, string) {
	w.t.Helper()
	c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: w.layout.Run(w.run).LinkSock(), Net: "unix"})
	check(w.t, err)
	defer func() { check(w.t, c.Close()) }()
	raw, err := json.Marshal(a)
	check(w.t, err)
	line, err := wire.Request{V: 1, Op: op, Kit: kitHex, DeadlineMS: (w.clk.Boot() + hookDeadline).Milliseconds(), Args: raw}.Encode()
	check(w.t, err)
	_, err = c.Write(append(line, '\n'))
	check(w.t, err)
	r := bufio.NewReader(c)
	var lines []string
	for {
		l, err := r.ReadString('\n')
		if errors.Is(err, io.EOF) {
			break
		}
		check(w.t, err)
		lines = append(lines, strings.TrimSuffix(l, "\n"))
	}
	switch len(lines) {
	case 0:
		return "", ""
	case 1:
		return "", lines[0]
	}
	if len(lines) != 2 {
		w.t.Fatalf("answer lines %q", lines)
	}
	return lines[0], lines[1]
}

// hook is a usable hook op's answer, which must be followed by done exit 0.
func (w *linkWorld) hook(op string) string {
	w.t.Helper()
	return w.hookWith(op, w.args(op))
}

func (w *linkWorld) hookWith(op string, a wire.HookArgs) string {
	w.t.Helper()
	answer, done := w.hookCall(op, a)
	if done != `{"done":{"exit":0}}` {
		w.t.Fatalf("%s answered %q then %q", op, answer, done)
	}
	return answer
}

// contextOf is an answer's additional context, "" for {}.
func contextOf(t *testing.T, answer string) string {
	t.Helper()
	if answer == "{}" {
		return ""
	}
	var a struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	check(t, json.Unmarshal([]byte(answer), &a))
	return a.HookSpecificOutput.AdditionalContext
}

// blockOf is a Stop answer's reason, "" for {}.
func blockOf(t *testing.T, answer string) string {
	t.Helper()
	if answer == "{}" {
		return ""
	}
	var a struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	check(t, json.Unmarshal([]byte(answer), &a))
	if a.Decision != "block" {
		t.Fatalf("answer %q", answer)
	}
	return a.Reason
}

// await is the next message to the keeper of kind, skipping every other.
func (w *linkWorld) awaitKind(kind string) wire.Msg {
	w.t.Helper()
	for {
		select {
		case g := <-w.in:
			if g.err != nil {
				w.t.Fatalf("waiting for %s: %v", kind, g.err)
			}
			if g.m.Kind == kind {
				return g.m
			}
		case <-w.t.Context().Done():
			w.t.Fatalf("no %s", kind)
		}
	}
}

func (w *linkWorld) persistItems(id paths.TeamID, items ...team.Item) {
	w.t.Helper()
	_, err := w.link().eng.Persist(w.t.Context(), team.Batch{Team: id, NextBatch: "s1", Items: items})
	check(w.t, err)
}

func humanAt(id paths.TeamID, n int) team.Item {
	ev := evAt(n)
	return team.Item{
		Pending: state.Pending{ID: ev, Room: proto.PurposeMain, Item: "human", SentMS: startWall, From: "@alice:" + paths.ServerName(id)},
		Verdict: proto.Verdict{Outcome: proto.Deliver, Event: proto.Event{ID: ev, OriginTs: startWall}, Human: proto.Human{Body: "hi"}},
	}
}

// evAt is an event id ending in n, a digit.
func evAt(n int) string { return "$" + strings.Repeat("0", 42) + strconv.Itoa(n) }

// HK1: SessionStart records the facts, tells the keeper the token once, and answers the frame
// with each team's block and the mode line; told and frame_told are written before the answer.
func TestSessionStartAnswersTheFrame(t *testing.T) {
	a := tid(t, "alpha")
	w := newHookWorld(t, a)
	coord := "repo.2+host.podman"
	w.link().eng.SetHealth(a, team.Health{Role: proto.RoleWorker, Coordinator: &coord})
	text := contextOf(t, w.hook(wire.OpHookSessionStart))
	for _, want := range []string{proto.FrameText, "team alpha: you are ", "the worker (coordinator: " + coord + ")", "Deliveries arrive in this conversation by themselves."} {
		if !strings.Contains(text, want) {
			t.Fatalf("frame %q lacks %q", text, want)
		}
	}
	s := w.sessionFile()
	if !s.FrameTold || s.Told.Roles[a] != proto.RoleWorker || *s.SessionID != sessA {
		t.Fatalf("session.json %+v", s)
	}
	if m := w.awaitKind(wire.KindToken); m.Token.Token != heldToken {
		t.Fatal("the keeper was not told the hook's token")
	}
	if strings.Contains(w.log.String(), heldToken) {
		t.Fatal("the token is in link.log")
	}
}

// HK1: a forged op (a peer outside the session) is answered {} and records nothing.
func TestAForgedOpIsAnsweredEmpty(t *testing.T) {
	w := newHookWorld(t)
	w.proc.add(os.Getpid(), 7, 2000)
	if got := w.hook(wire.OpHookSessionStart); got != "{}" {
		t.Fatalf("forged answer %q", got)
	}
	// Nothing was recorded: session.json was never written.
	if s := w.link().session.File(); s.Started() || s.FrameTold {
		t.Fatalf("session %+v", s)
	}
	if _, err := os.Stat(w.layout.Run(w.run).Session()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("session.json: %v", err)
	}
}

// HK1: an op before the keeper told the pid waits to its deadline and is closed unanswered.
func TestAnOpBeforeThePIDIsNotAnswered(t *testing.T) {
	w := newLinkWorld(t)
	w.ready()
	got := make(chan [2]string, 1)
	go func() {
		answer, done := w.hookCall(wire.OpHookPrompt, w.args(wire.OpHookPrompt))
		got <- [2]string{answer, done}
	}()
	// The op's deadline, not the read limit the server arms on accept, before the request is
	// written: advanced on that, the request's deadline was read after the advance and never came.
	check(t, w.clk.AwaitDue(t.Context(), hookDeadline))
	w.clk.Advance(hookDeadline)
	if g := <-got; g != [2]string{"", ""} {
		t.Fatalf("answered %q", g)
	}
}

// HK1: a request whose args do not decode is the schema refusal.
func TestBadHookArgsAreRefused(t *testing.T) {
	w := newHookWorld(t)
	a := w.args(wire.OpHookPrompt)
	a.Payload = "maybe"
	answer, done := w.hookCall(wire.OpHookPrompt, a)
	if answer != "" || !strings.Contains(done, `"code":"`+exits.CodeSchema+`"`) {
		t.Fatalf("answered %q then %q", answer, done)
	}
}

// HK1, T13: an unusable payload is counted in session.json and status.json, logged, and
// answered as an event with no payload; the count survives a restart.
func TestAnUnusablePayloadIsCounted(t *testing.T) {
	w := newHookWorld(t)
	reason := wire.UnusableJSON
	bad := wire.HookArgs{Payload: wire.PayloadUnusable, Unusable: &reason}
	if got := w.hookWith(wire.OpHookStop, bad); got != "{}" {
		t.Fatalf("answer %q", got)
	}
	w.hookWith(wire.OpHookPrompt, bad)
	if got := w.sessionFile().Unusable.HookStdin; got != 2 {
		t.Fatalf("hook_stdin %d", got)
	}
	if !strings.Contains(w.log.String(), "unusable (json)") {
		t.Fatalf("link.log %q", w.log.String())
	}
	if s := w.status(); !strings.Contains(s, `"hook_stdin":2`) {
		t.Fatalf("status %q", s)
	}
	// A restarted link reads the count back (and gives it the engine, as
	// TestARestartedLinkDeliversOnTheToken shows).
	again, err := newSession(sessionDeps{runRoot: w.root, stateRoot: w.root, layout: w.layout, run: w.run, clock: w.clk, log: w.log})
	check(t, err)
	if got := again.Unusable().HookStdin; got != 2 {
		t.Fatalf("hook_stdin after a restart %d", got)
	}
}

// tokenless is a without the inbox token, so delivery is not live: a hook that tells the
// socket and its token makes it live, and the socket will push the items.
func tokenless(a wire.HookArgs) wire.HookArgs {
	a.Token = nil
	return a
}

// hookNotLive is op's answer with no token told.
func (w *linkWorld) hookNotLive(op string) string {
	w.t.Helper()
	return w.hookWith(op, tokenless(w.args(op)))
}

// HK1: the prompt answers the pending counts when delivery is not live (no token here).
func TestThePromptCountsPendingWhenNotLive(t *testing.T) {
	a := tid(t, "alpha")
	w := newHookWorld(t, a)
	w.persistItems(a, humanAt(a, 1))
	text := contextOf(t, w.hookNotLive(wire.OpHookPrompt))
	if !strings.Contains(text, hooktext.Pending(hooktext.Counts{Total: 1, Humans: 1})) {
		t.Fatalf("prompt %q", text)
	}
}

// HK2: Stop blocks once per distinct pending set, only on stop_hook_active false, and never
// when session.json cannot be written.
func TestStopBlocksOncePerPendingSet(t *testing.T) {
	a := tid(t, "alpha")
	w := newHookWorld(t, a)
	w.hookNotLive(wire.OpHookSessionStart)
	w.persistItems(a, humanAt(a, 1))
	active := tokenless(w.args(wire.OpHookStop))
	active.StopHookFalse = false
	if got := w.hookWith(wire.OpHookStop, active); got != "{}" {
		t.Fatalf("stop_hook_active true answered %q", got)
	}
	reason := blockOf(t, w.hookNotLive(wire.OpHookStop))
	if !strings.Contains(reason, "1 pending") || !strings.Contains(reason, "teamchat recv") {
		t.Fatalf("block %q", reason)
	}
	if got := w.hookNotLive(wire.OpHookStop); got != "{}" {
		t.Fatalf("the same set blocked again: %q", got)
	}
	w.persistItems(a, humanAt(a, 2))
	unwritable(t, w)
	if got := w.hookNotLive(wire.OpHookStop); got != "{}" {
		t.Fatalf("an unwritable session.json blocked: %q", got)
	}
}

// unwritable puts a directory where session.json is, so every write of it fails; the facts
// are still accepted from memory before it.
func unwritable(t *testing.T, w *linkWorld) {
	t.Helper()
	check(t, w.root.Unlink(w.layout.Run(w.run).Session()))
	check(t, w.root.MkdirAll(w.layout.Run(w.run).Session()+"/x"))
}

// HK2, n3e-D12, OF1: a failed team blocks once per 10 minutes; a down one never blocks, and
// the prompt says it cannot reach its server. The prompt's line and Stop's block share their
// 10 minutes (n3e-D11b), so the wall clock is stepped between them.
func TestAFailedTeamBlocksEveryTenMinutes(t *testing.T) {
	a, b := tid(t, "alpha"), tid(t, "bravo")
	w := newHookWorld(t, a, b)
	w.hook(wire.OpHookSessionStart)
	l := w.link()
	l.eng.SetHealth(b, team.Health{Down: true})
	if got := w.hook(wire.OpHookStop); got != "{}" {
		t.Fatalf("a down team blocked: %q", got)
	}
	if text := contextOf(t, w.hook(wire.OpHookPrompt)); !strings.Contains(text, hooktext.TeamDown(b)) {
		t.Fatalf("prompt %q", text)
	}
	if text := contextOf(t, w.hook(wire.OpHookPrompt)); strings.Contains(text, hooktext.TeamDown(b)) {
		t.Fatalf("the down line again within 10 minutes: %q", text)
	}
	w.clk.StepWall(repeatEvery.Milliseconds())
	l.eng.SetHealth(a, team.Health{Stopped: exits.CodeToken})
	if reason := blockOf(t, w.hook(wire.OpHookStop)); !strings.Contains(reason, hooktext.TeamFailed(a, state.FailedUnauthorized)) {
		t.Fatalf("block %q", reason)
	}
	if got := w.hook(wire.OpHookStop); got != "{}" {
		t.Fatalf("blocked again within 10 minutes: %q", got)
	}
	w.clk.StepWall(repeatEvery.Milliseconds())
	if got := blockOf(t, w.hook(wire.OpHookStop)); got == "" {
		t.Fatal("no block after 10 minutes")
	}
}

// HK2: a wait session with no wait running blocks once per 10 minutes.
func TestAWaitSessionWithNoWaiterBlocks(t *testing.T) {
	w := newLinkWorld(t)
	w.proc.add(os.Getpid(), pidClaude, 2000)
	w.proc.add(pidClaude, pidSession, 1001)
	w.proc.add(pidSession, 50, 1000)
	w.start()
	w.send(wire.Msg{Kind: wire.KindLaunch, Launch: &wire.Launch{
		Run: w.run, Kind: state.KindCCY, Launch: state.LaunchClaude, Inbound: state.InboundRefuse, Cwd: "/work",
	}}, nil)
	w.next(wire.KindReady)
	w.send(wire.Msg{Kind: wire.KindPID, PID: &state.Process{PID: pidSession, StartTicks: 1000}}, nil)
	w.sync(1)
	if reason := blockOf(t, w.hook(wire.OpHookStop)); reason != hooktext.StopNoWaker() {
		t.Fatalf("block %q", reason)
	}
	if got := w.hook(wire.OpHookStop); got != "{}" {
		t.Fatalf("blocked again: %q", got)
	}
	w.clk.StepWall(repeatEvery.Milliseconds())
	if got := blockOf(t, w.hook(wire.OpHookStop)); got != hooktext.StopNoWaker() {
		t.Fatalf("after 10 minutes %q", got)
	}
}

// HK3: SessionEnd records the reason in one write, answers {} under the deadline with no
// clock moved, and a clear or resume records nothing.
func TestSessionEndRecordsTheReasonOnly(t *testing.T) {
	w := newHookWorld(t)
	w.hook(wire.OpHookSessionStart)
	end := func(reason string) string {
		a := w.args(wire.OpHookSessionEnd)
		a.Reason = &reason
		return w.hookWith(wire.OpHookSessionEnd, a)
	}
	if got := end("clear"); got != "{}" || w.sessionFile().EndReason != nil {
		t.Fatalf("clear: %q, %v", got, w.sessionFile().EndReason)
	}
	if got := end("prompt_input_exit"); got != "{}" || *w.sessionFile().EndReason != "exit" {
		t.Fatalf("exit: %q", got)
	}
	if end("logout"); *w.sessionFile().EndReason != "logout" {
		t.Fatal("logout not recorded")
	}
	if end("bypass_permissions_disabled"); *w.sessionFile().EndReason != "other" {
		t.Fatal("other not recorded")
	}
	if len(w.ends.endCalls()) != 0 {
		t.Fatal("SessionEnd posted an end")
	}
}

// n3c-D23, n3e-D13: a refused add and a refused desired file are each said once on the
// prompt, and on every SessionStart while they hold.
func TestRefusalsAreToldOnce(t *testing.T) {
	b := tid(t, "bravo")
	w := newHookWorld(t)
	code := "schema"
	w.send(wire.Msg{Kind: wire.KindDesired, Desired: &state.ReportDesired{Generation: 2, State: state.DesiredRefused, Code: &code}}, nil)
	w.send(wire.Msg{Kind: wire.KindRefused, Refused: &wire.Refused{Team: b, HolderRun: w.run}}, nil)
	w.sync(2)
	text := contextOf(t, w.hook(wire.OpHookPrompt))
	if !strings.Contains(text, hooktext.DesiredRefused(code)) || !strings.Contains(text, hooktext.AddRefused(b)) {
		t.Fatalf("prompt %q", text)
	}
	if text := contextOf(t, w.hook(wire.OpHookPrompt)); text != "" {
		t.Fatalf("told twice: %q", text)
	}
	if text := contextOf(t, w.hook(wire.OpHookSessionStart)); !strings.Contains(text, hooktext.AddRefused(b)) || !strings.Contains(text, hooktext.DesiredRefused(code)) {
		t.Fatalf("session start %q", text)
	}
}

// HK2: live delivery with nothing held does not block, so items the socket will still push
// never block; a stall's not-pushed items do, and the guard records their digest.
func TestStopDecision(t *testing.T) {
	a := tid(t, "alpha")
	counts := hooktext.Counts{Total: 1, Humans: 1}
	live := engine.HookView{Delivery: state.DeliverySocket, Live: true, Teams: []engine.HookTeam{{Team: a, Counts: counts}}}
	if reasons, next := stopDecision(live, state.StopGuard{}, startWall); len(reasons) != 0 || next != (state.StopGuard{}) {
		t.Fatalf("live: %q, %+v", reasons, next)
	}
	stalled := engine.HookView{Delivery: state.DeliverySocket, Stalled: true, Teams: []engine.HookTeam{{Team: a, Counts: counts, NotPushed: []string{evAt(1)}}}}
	reasons, next := stopDecision(stalled, state.StopGuard{}, startWall)
	if len(reasons) != 1 || reasons[0] != hooktext.StopPending(counts) || next.PendingDigest == nil || *next.PendingDigest != digestOf(stalled) {
		t.Fatalf("stalled: %q, %+v", reasons, next)
	}
	if again, same := stopDecision(stalled, next, startWall); len(again) != 0 || same != next {
		t.Fatalf("the same set again: %q, %+v", again, same)
	}
	if line := pendingLine(stalled); line != hooktext.Stalled(counts) {
		t.Fatalf("pending line %q", line)
	}
}

// due: never made, ten minutes on, and a wall clock stepped back are due; within ten minutes
// is not.
func TestDue(t *testing.T) {
	last := startWall
	for _, c := range []struct {
		last *int64
		now  int64
		due  bool
	}{
		{nil, startWall, true},
		{&last, startWall + repeatEvery.Milliseconds() - 1, false},
		{&last, startWall + repeatEvery.Milliseconds(), true},
		{&last, startWall - 1, true},
	} {
		if got := due(c.last, c.now); got != c.due {
			t.Fatalf("due(%v, %d) = %t", c.last, c.now, got)
		}
	}
}
