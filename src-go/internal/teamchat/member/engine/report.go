package engine

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// reportsVerifyWait is a send's deadline (LN2), which bounds the sender's verification of the
// reports room before a queued report is posted, as it bounds the verb's.
const reportsVerifyWait = 30 * time.Second

const (
	msgNoReportText    exits.Template = "report reads its text on stdin, and none was given"
	msgReportSize      exits.Template = "a report is at most 1500 bytes; it is never cut, so shorten it"
	msgReportNoHead    exits.Template = "a report's first line, its headline, is empty; the first line is the headline, 1 to 120 characters"
	msgReportLongHead  exits.Template = "a report's headline, its first line, is over 120 characters; the first line is 1 to 120 characters"
	msgReportRate      exits.Template = "team {} allows {} reports an hour and they are made; the oldest leaves the window in {} seconds; a report that was not posted still counts"
	msgReportsRefused  exits.Template = "the homeserver forbids this member's report to the reports room of team {}; tell a human"
	msgReportsNoRoster exits.Template = "team {} has not been verified yet, so its reports room is not known; the link keeps trying"
)

// report is RP3: stdin is the report, to the reports room, in the team CL6 names. The team
// comes first (64), then the sender's role (11), the size and the headline (10 body), the secret
// check (10 secret), the room (RP4), the window and the gate (14, 15), and the charge; the
// first three are the composer's, in its order, and the rest are send's.
func (e *Engine) report(ctx context.Context, a wire.ReportArgs, stdin *string, out RecordWriter) wire.Done {
	st, err := e.reportingTeam(ctx, a.Team)
	if err != nil {
		return doneOf(err)
	}
	if stdin == nil {
		return doneOf(exits.New(exits.CodeUsage, &st.id, msgNoReportText))
	}
	text := *stdin
	return e.send(ctx, st, state.SendReport, func(c proto.Composer) (proto.Outgoing, error) {
		o, err := c.Report(text)
		return o, reportBodyError(st.id, text, err)
	}, out)
}

// reportingTeam is the team a report goes to (CL6): the flag's, else the only team held. A team
// not yet verified has no roster to compose against, and is unreachable for now.
func (e *Engine) reportingTeam(ctx context.Context, flag *paths.TeamID) (sendTeam, error) {
	cands, err := e.candidates(ctx, flag)
	if err != nil {
		return sendTeam{}, err
	}
	if len(cands) > 1 {
		active := make([]cli.ActiveTeam, 0, len(cands))
		for _, c := range cands {
			a := cli.ActiveTeam{ID: c.id}
			if r, ok := c.roster(); ok {
				a.Description = r.Name
			}
			active = append(active, a)
		}
		_, err := cli.ResolveTeam(nil, nil, active)
		return sendTeam{}, err
	}
	c := cands[0]
	r, ok := c.roster()
	if !ok {
		return sendTeam{}, exits.New(exits.CodeUnreachable, &c.id, msgReportsNoRoster, exits.Team(c.id))
	}
	return sendTeam{id: c.id, tm: c.tm, roster: r, self: c.self, anchors: c.anchors}, nil
}

// reportBodyError is a composer's refusal of a report's form as the exit it is, its message
// naming which rule the text broke (RP3 step 3); any other error passes unchanged.
func reportBodyError(t paths.TeamID, text string, err error) error {
	re, ok := errors.AsType[*proto.RefusalError](err)
	if !ok || re.Reason != proto.ReasonBody {
		return err
	}
	switch headline := strings.TrimSpace(strings.SplitN(text, "\n", 2)[0]); {
	case len(text) > proto.MaxReportBody:
		return exits.New(exits.CodeBody, &t, msgReportSize)
	case headline == "":
		return exits.New(exits.CodeBody, &t, msgReportNoHead)
	case utf8.RuneCountInString(headline) > proto.MaxHeadline:
		return exits.New(exits.CodeBody, &t, msgReportLongHead)
	}
	return err
}

// admitRefusal is Admit's refusal as the send's exit: a report refused by report_per_hour is
// 14 report-rate, naming when the oldest report leaves the window (a refusal spends nothing);
// every other refusal is the send's.
func admitRefusal(t paths.TeamID, err error, c proto.Composer, w *proto.Window, now int64) error {
	re, ok := errors.AsType[*proto.RefusalError](err)
	if !ok || re.Reason != proto.ReasonRate || re.Detail != proto.DetailReportPerHour {
		return refusal(t, err)
	}
	var leavesMs int64
	if oldest, found := w.Oldest(c.Self, proto.ReportWindowMs, now); found {
		leavesMs = max(oldest+proto.ReportWindowMs-now, 0)
	}
	return exits.New(exits.CodeReportRate, &t, msgReportRate,
		exits.Team(t), exits.Count(c.Roster.Limits.ReportPerHour), exits.Count((leavesMs+999)/1000))
}

// postRoom is the room a queued send is posted to, after the re-check against roster r
// (n3d-D27): main, or for a report the reports room the roster names once it verifies; stop is
// true with the hold the sender keeps when it must not post now.
func (e *Engine) postRoom(ctx context.Context, h *held, p nextPost, token string, r proto.Roster) (string, *sendHold, bool) {
	if err := e.recheck(p, token, r); err != nil {
		return "", e.unposted(ctx, h, p.entry, err), true
	}
	switch p.entry.Room {
	case proto.PurposeMain:
		return p.main, nil, false
	case proto.PurposeReports:
		return e.reportsTarget(ctx, h, p, r)
	}
	// recheck refuses every other room, so none reaches here; a room added there and not here
	// is refused all the same.
	return "", e.unposted(ctx, h, p.entry, unknownRoom(h.id, p.entry.Room)), true
}

// reportsTarget is the sender's half of RP4 (n3f-D16): the reports room verified again before a
// queued report is posted, inside a send's 30 s on the engine's clock so that a room that does
// not answer cannot hold the team's sends. It gives the room to post to, or stop with the hold
// the sender keeps: a refusal removes the report (logged by its transaction id, counted
// unsent, its token and window slot spent) and answers its verb with it; a failure that is
// not a refusal is transient, and the report waits.
func (e *Engine) reportsTarget(ctx context.Context, h *held, p nextPost, r proto.Roster) (string, *sendHold, bool) {
	if r.Rooms.Reports == "" {
		e.logf("team %s: the roster names no reports room, so report %s waits", p.id, p.entry.Txn)
		return "", e.hold(ctx, h), true
	}
	vctx, cancel := context.WithCancel(ctx)
	stop := e.cfg.Clock.AfterFunc(reportsVerifyWait, cancel)
	err := p.tm.VerifyReports(vctx)
	stop()
	cancel()
	if err == nil {
		return r.Rooms.Reports, nil, false
	}
	if ctx.Err() != nil {
		return "", nil, true
	}
	xe, refused := errors.AsType[*exits.Error](err)
	if !refused {
		e.logf("team %s: the reports room was not verified, so report %s waits: %v", p.id, p.entry.Txn, err)
		return "", e.hold(ctx, h), true
	}
	var held *sendHold
	if doErr := e.do(ctx, func() {
		held = e.dropSend(h, p.entry, sendResult{err: xe}, "queued report "+p.entry.Txn+" was not posted: "+xe.Code)
	}); doErr != nil {
		return "", nil, true
	}
	return "", held, true
}

// droppedForbidden is a report whose PUT the homeserver answered 403 (RP4): removed alone and
// answered 22 forbidden if its verb still waits, otherwise logged by its transaction id.
func (e *Engine) droppedForbidden(ctx context.Context, h *held, entry state.Sending) *sendHold {
	var held *sendHold
	if err := e.do(ctx, func() {
		xe := exits.New(exits.CodeForbidden, &h.id, msgReportsRefused, exits.Team(h.id))
		held = e.dropSend(h, entry, sendResult{err: xe}, "queued report "+entry.Txn+" was not posted: forbidden")
	}); err != nil {
		return nil
	}
	return held
}

// recheckReport composes a queued report again with roster r (n3d-D27), so one the team no
// longer allows its sender is refused at its post.
func (e *Engine) recheckReport(p nextPost, token string, r proto.Roster, v proto.Value) error {
	dc, reason := proto.DecodeContent(v, proto.ReportKeys)
	if reason != "" {
		return &proto.RefusalError{Reason: reason, Detail: "the queued content does not decode"}
	}
	c := proto.Composer{Server: p.server, Self: p.self, Token: token, Roster: r, Lookup: p.lookup.with(r), NowMs: e.cfg.Clock.WallMS()}
	_, err := c.Report(dc.Body)
	return err
}

// What the verb's check of the reports room found: the room verifies, it fails (the report is
// refused with the check's own answer), or the homeserver did not say.
const (
	reportsVerified = iota
	reportsRefused
	reportsUnknown
)

// reportsRoomOK is RP4's check at the verb, after the report composed and before anything is
// charged: the reports room verified now, off the state goroutine, inside the request's
// deadline. An *exits.Error (forbidden, untrusted) refuses the report, which charges nothing
// and leaves main as it was; the request's own deadline ended is 4 timed-out; any other failure
// is the transient case, which does not refuse: the report is charged and queued at once, and
// the sender verifies the room again before its PUT (n3d-D2, n3f-D16).
func (e *Engine) reportsRoomOK(ctx context.Context, st sendTeam) (wire.Done, int) {
	err := st.tm.VerifyReports(ctx)
	if err == nil {
		return wire.Done{}, reportsVerified
	}
	if ctx.Err() != nil {
		return doneOf(exits.New(exits.CodeTimedOut, &st.id, msgTimedOut, exits.Team(st.id))), reportsRefused
	}
	if xe, ok := errors.AsType[*exits.Error](err); ok {
		return doneOf(xe), reportsRefused
	}
	e.logf("team %s: the reports room was not verified, so the report is queued: %v", st.id, err)
	return wire.Done{}, reportsUnknown
}
