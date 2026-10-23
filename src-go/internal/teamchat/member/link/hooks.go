package link

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/engine"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/hooktext"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// repeatEvery is how often a failure line, a failed team's Stop block and the no-waker block
// come back (B 2.8).
const repeatEvery = 10 * time.Minute

// The events Claude Code names the two hooks that take context.
const (
	eventSessionStart = "SessionStart"
	eventPrompt       = "UserPromptSubmit"
)

// hookMemory is what the hooks keep in memory for the link's life: one op at a time, and the
// refusals already told (n3e-D13).
type hookMemory struct {
	mu          sync.Mutex
	desiredTold bool
	addsTold    map[paths.TeamID]bool
}

// hookReply is a hook op's answer line and done; not answered, the connection closes with
// neither, and the hook gives its own recovery line.
type hookReply struct {
	line     []byte
	done     wire.Done
	answered bool
}

func isHookOp(op string) bool {
	return op == wire.OpHookSessionStart || op == wire.OpHookPrompt || op == wire.OpHookStop || op == wire.OpHookSessionEnd
}

// serveHook is a hook op (HK1-HK3, n3e-D18): the args decoded strictly, the keeper's pid
// awaited within the op's deadline, an unusable payload counted, then the event's answer, from
// fixed templates and the engine's memory alone; no hook op makes a network call.
func (l *link) serveHook(ctx context.Context, req wire.Request, peer int) hookReply {
	a, err := wire.DecodeHookArgs(req.Op, req.Args)
	if xe, ok := errors.AsType[*exits.Error](err); ok {
		return hookReply{done: wire.Done{Exit: int(xe.Exit), Error: xe}, answered: true}
	}
	if err != nil || !l.session.AwaitPID(ctx) {
		return hookReply{}
	}
	l.hooks.mu.Lock()
	defer l.hooks.mu.Unlock()
	usable := a.Payload == wire.PayloadOK
	if !usable {
		l.countUnusable(ctx, *a.Unusable)
	}
	return hookReply{line: l.answerHook(ctx, req.Op, a, usable, peer).Line, answered: true}
}

// countUnusable is B 2.8's count of a payload that was not usable: session.json, one line, and
// the engine for status and status.json.
func (l *link) countUnusable(ctx context.Context, why string) {
	u, err := l.session.CountUnusable()
	l.logf("link: a hook's input was unusable (%s); %d so far", why, u.HookStdin)
	l.logErr("count an unusable hook input", err)
	l.logErr("tell the engine the unusable counts", l.eng.SetUnusable(ctx, u))
}

func factsOf(op string, a wire.HookArgs, peer int) Facts {
	deref := func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	}
	return Facts{Op: op, Peer: peer, SessionID: deref(a.SessionID), Source: deref(a.Source), Transcript: deref(a.TranscriptPath), Socket: deref(a.Socket)}
}

// answerHook is the event's answer: the facts accepted first when the payload is usable (a
// refused op is {} and records nothing), the session's facts told the engine, then its view.
func (l *link) answerHook(ctx context.Context, op string, a wire.HookArgs, usable bool, peer int) hooktext.Answer {
	if op == wire.OpHookSessionEnd {
		return l.sessionEnd(a, usable, peer)
	}
	if usable {
		if err := l.session.AcceptFacts(factsOf(op, a, peer)); err != nil {
			l.logf("link: %s refused: %v", op, err)
			return hooktext.Empty()
		}
	}
	l.tellSession(ctx, a, usable, op != wire.OpHookPrompt)
	v, err := l.eng.HookView(ctx)
	if err != nil {
		l.logf("link: %s: the engine's view: %v", op, err)
		return hooktext.Empty()
	}
	switch op {
	case wire.OpHookSessionStart:
		return l.sessionStart(v)
	case wire.OpHookPrompt:
		return l.prompt(v)
	}
	return l.stop(v, a.StopHookFalse)
}

// tellSession gives the engine the accepted socket and transcript, the token (told the keeper
// when it is new) and whether the session is now idle (a Stop, or a SessionStart with no
// prompt since).
func (l *link) tellSession(ctx context.Context, a wire.HookArgs, usable, idle bool) {
	f := engine.SessionFacts{Idle: idle}
	if usable {
		f = l.session.Inbox()
		f.Idle = idle
		if a.Token != nil && *a.Token != "" {
			f.Token = *a.Token
			l.takeToken(*a.Token)
		}
	}
	l.logErr("tell the engine the session", l.eng.SetSession(ctx, f))
}

// takeToken keeps a hook's inbox token and gives it to the keeper when it is new, so a
// restarted link has it before any hook (B 2.4.2).
func (l *link) takeToken(token string) {
	l.mu.Lock()
	known := l.token == token
	l.token = token
	l.mu.Unlock()
	if !known {
		l.send(wire.Msg{Kind: wire.KindToken, Token: &wire.Token{Token: token}})
	}
}

// sessionStart is the frame, every team's block and the mode, then any pending, failure and
// refusal line; told is set to the teams now, and frame_told, before the answer.
func (l *link) sessionStart(v engine.HookView) hooktext.Answer {
	teams := make([]hooktext.FrameTeam, 0, len(v.Teams))
	for _, t := range v.Teams {
		teams = append(teams, hooktext.FrameTeam{Team: t.Team, Handle: t.Handle, Role: t.Role, Coordinator: t.Coordinator, Description: t.Description})
	}
	lines := []string{hooktext.Frame(teams, v.Delivery)}
	if p := pendingLine(v); p != "" {
		lines = append(lines, p)
	}
	lines = append(lines, failureLines(v, true)...)
	lines = append(lines, l.refusals(v, true)...)
	l.logErr("record the frame told", l.tellFrame(v))
	return hooktext.Context(eventSessionStart, strings.Join(lines, "\n"))
}

// prompt is UserPromptSubmit's lines: the membership notice the socket could not take, the
// pending counts when the socket will not push them, a failure line once per 10 minutes and the
// refusals not yet told.
func (l *link) prompt(v engine.HookView) hooktext.Answer {
	lines := l.owedMembership()
	if p := pendingLine(v); p != "" {
		lines = append(lines, p)
	}
	if f := failureLines(v, true); len(f) > 0 {
		now := l.cfg.Clock.WallMS()
		if due(l.session.File().StopGuard.FailureMS, now) {
			err := l.session.Update(func(n *state.Session) bool {
				n.StopGuard.FailureMS = &now
				return true
			})
			l.logErr("record the failure line", err)
			lines = append(lines, f...)
		}
	}
	lines = append(lines, l.refusals(v, false)...)
	return hooktext.Context(eventPrompt, strings.Join(lines, "\n"))
}

// stop is HK2: nothing unless stop_hook_active was the literal false; a block only when its
// guard is written to session.json.
func (l *link) stop(v engine.HookView, stopFalse bool) hooktext.Answer {
	if !stopFalse {
		return hooktext.Empty()
	}
	reasons, next := stopDecision(v, l.session.File().StopGuard, l.cfg.Clock.WallMS())
	if len(reasons) == 0 {
		return hooktext.Empty()
	}
	if err := l.session.Update(func(n *state.Session) bool {
		n.StopGuard = next
		return true
	}); err != nil {
		l.logf("link: Stop does not block, its guard could not be written: %v", err)
		return hooktext.Empty()
	}
	return hooktext.Block(strings.Join(reasons, " "))
}

// stopDecision is Stop's blocks (B 2.8, n3e-D12) and the guard that records them: once per
// distinct set of pending items the socket will not push, once per 10 minutes for a failed
// team (never a down one), once per 10 minutes for a wait session with no wait running.
func stopDecision(v engine.HookView, g state.StopGuard, nowMS int64) ([]string, state.StopGuard) {
	var reasons []string
	next := g
	if d := digestOf(v); d != "" && (g.PendingDigest == nil || *g.PendingDigest != d) {
		reasons = append(reasons, hooktext.StopPending(totals(v).counts))
		next.PendingDigest = &d
	}
	if f := failureLines(v, false); len(f) > 0 && due(g.FailureMS, nowMS) {
		reasons = append(reasons, f...)
		next.FailureMS = &nowMS
	}
	if v.Delivery == state.DeliveryWait && !v.Waiting && due(g.NoWakerMS, nowMS) {
		reasons = append(reasons, hooktext.StopNoWaker())
		next.NoWakerMS = &nowMS
	}
	return reasons, next
}

// due is whether a repeat last made at last (nil for never) is due again at now; a wall clock
// stepped back makes it due.
func due(last *int64, nowMS int64) bool {
	return last == nil || nowMS-*last >= repeatEvery.Milliseconds() || nowMS < *last
}

// notPushed is the not-pushed counts over every team, and how many are held as missing.
type notPushed struct {
	counts  hooktext.Counts
	missing int64
}

func totals(v engine.HookView) notPushed {
	var n notPushed
	for _, t := range v.Teams {
		c := &n.counts
		c.Total += t.Counts.Total
		c.Humans += t.Counts.Humans
		c.Agents += t.Counts.Agents
		c.Pings += t.Counts.Pings
		c.Notices += t.Counts.Notices
		c.Timeouts += t.Counts.Timeouts
		n.missing += t.Missing
	}
	return n
}

// pendingLine is the line for the items the socket will not push, "" for none: the stall's,
// the counts when delivery is not live, else B 2.7.4's for the items held as missing.
func pendingLine(v engine.HookView) string {
	n := totals(v)
	switch {
	case n.counts.Total == 0:
		return ""
	case v.Stalled:
		return hooktext.Stalled(n.counts)
	case !v.Live:
		return hooktext.Pending(n.counts)
	}
	return hooktext.Missing(n.missing)
}

// failureLines are the failed teams' lines and, withDown, the down teams' (OF1: shown, never a
// Stop block).
func failureLines(v engine.HookView, withDown bool) []string {
	var lines []string
	for _, t := range v.Teams {
		switch {
		case t.Failed != nil:
			lines = append(lines, hooktext.TeamFailed(t.Team, *t.Failed))
		case t.Down && withDown:
			lines = append(lines, hooktext.TeamDown(t.Team))
		}
	}
	return lines
}

// digestOf is the digest of the not-pushed pending set, "" when it is empty.
func digestOf(v engine.HookView) string {
	var ids []string
	for _, t := range v.Teams {
		for _, id := range t.NotPushed {
			ids = append(ids, t.Team.String()+" "+id)
		}
	}
	if len(ids) == 0 {
		return ""
	}
	slices.Sort(ids)
	sum := sha256.Sum256([]byte(strings.Join(ids, "\n")))
	return hex.EncodeToString(sum[:])
}

// refusals are the refused desired file's and refused adds' lines (n3e-D13): every one with
// all (SessionStart), else only those not yet told this link's life.
func (l *link) refusals(v engine.HookView, all bool) []string {
	var lines []string
	h := &l.hooks
	refused := v.Desired.State == state.DesiredRefused
	if refused && (all || !h.desiredTold) {
		code := ""
		if v.Desired.Code != nil {
			code = *v.Desired.Code
		}
		lines = append(lines, hooktext.DesiredRefused(code))
	}
	h.desiredTold = refused
	told := map[paths.TeamID]bool{}
	for _, t := range v.Refused {
		if all || !h.addsTold[t] {
			lines = append(lines, hooktext.AddRefused(t))
		}
		told[t] = true
	}
	h.addsTold = told
	return lines
}

// sessionEnd is HK3: a clear or resume is {} and nothing else; any other reason is recorded,
// with the facts, in one session.json write; no network.
func (l *link) sessionEnd(a wire.HookArgs, usable bool, peer int) hooktext.Answer {
	if !usable || a.Reason == nil || *a.Reason == state.SourceClear || *a.Reason == state.SourceResume {
		return hooktext.Empty()
	}
	reason := endReason(*a.Reason)
	err := l.session.acceptWith(factsOf(wire.OpHookSessionEnd, a, peer), func(n *state.Session) { n.EndReason = &reason })
	if err != nil {
		l.logf("link: SessionEnd refused: %v", err)
	}
	return hooktext.Empty()
}

// endReason is session.json's end_reason for Claude Code's SessionEnd reason.
func endReason(r string) string {
	switch r {
	case "logout":
		return "logout"
	case "exit", "prompt_input_exit":
		return "exit"
	}
	return "other"
}
