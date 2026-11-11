package engine

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The reports room's `report` verb (SPEC-n3f RP3 to RP6, n3f-D6, n3f-D16): the order of its
// refusals, the window kept in the store, the verification at the verb and at the sender, and
// the post to the reports room alone.

// reportText is a report that composes: a headline and a body.
const reportText = "Tests pass\nAll 12 of them."

// reportsRoomOf is the reports room sendRoster names for team id.
func reportsRoomOf(id paths.TeamID) string { return sendRoster(id).Rooms.Reports }

func (w *world) report(tm *paths.TeamID, text string) answer {
	w.t.Helper()
	return w.request(w.t.Context(), wire.OpReport, wire.ReportArgs{Team: tm}, &text)
}

func (n *fakeNotices) sentRooms() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.rooms)
}

// reportPosted is a report answered posted, in the reports room: its event id.
func reportPosted(t *testing.T, a answer) string {
	t.Helper()
	s := sentOf(t, a)
	if s.Queued || s.ID == nil || s.Room != proto.PurposeReports {
		t.Fatalf("the report was not posted to the reports room: %+v", s)
	}
	return *s.ID
}

// reportQueued is a report answered queued, exit 0, in the reports room.
func reportQueued(t *testing.T, a answer) {
	t.Helper()
	if s := sentOf(t, a); !s.Queued || s.ID != nil || s.Room != proto.PurposeReports {
		t.Fatalf("the report was not queued for the reports room: %+v", s)
	}
}

// withReportTimes is a new store of team id whose report window holds times.
func (w *world) withReportTimes(id paths.TeamID, times ...int64) state.Store {
	s := w.newStore(id)
	s.Outbox.Reports = times
	return s
}

func untrustedReports(t *testing.T, id paths.TeamID) error {
	t.Helper()
	return exits.New(exits.CodeUntrusted, &id, "the reports room of team {} fails its {} check; tell a human", exits.Team(id), exits.Words("roster"))
}

func forbiddenReports(id paths.TeamID) error {
	return exits.New(exits.CodeForbidden, &id, "the homeserver forbids this member the reports room of team {}; tell a human", exits.Team(id))
}

// RP3, RP5: a report is posted to the reports room and no other, as A 10.1's content exactly, and
// answers the sent record with room reports. The window is written (the report's time), main's
// recovery anchor and the resolver's own sends are not touched, and no ping is made of it.
func TestRP3AReportIsPostedToTheReportsRoom(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sending(ops)
	now := w.clk.WallMS()
	id := reportPosted(t, w.report(&ops, reportText))
	if got := f.notices.sentRooms(); !slices.Equal(got, []string{reportsRoomOf(ops)}) {
		t.Fatalf("the report went to %v, want the reports room alone", got)
	}
	want := `{"msgtype":"m.notice","body":"Tests pass\nAll 12 of them.","format":"org.matrix.custom.html","formatted_body":"<p>Tests pass<br>All 12 of them.</p>","m.mentions":{},"teamchat.report":{"v":1}}`
	if got := string(lastContent(t, f)); got != want {
		t.Errorf("the PUT's content\n got %s\nwant %s", got, want)
	}
	s := w.store(ops)
	if len(s.Outbox.Sending) != 0 || !slices.Equal(s.Outbox.Reports, []int64{now}) {
		t.Errorf("after the post: sending %d, reports %v, want none and [%d]", len(s.Outbox.Sending), s.Outbox.Reports, now)
	}
	if s.Outbox.LastSent != nil || len(s.Outbox.Pings) != 0 || len(f.notices.owned()) != 0 {
		t.Errorf("a report touched main's bookkeeping: last_sent %v, pings %d, own sends %v (event %s)", s.Outbox.LastSent, len(s.Outbox.Pings), f.notices.owned(), id)
	}
}

// RP3 step 1: with several teams held and no --team the verb is 64 ambiguous-team; --team names
// one, and nothing is charged by the refusal.
func TestRP3TheTeamRule(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops, dev := tid(t, "ops"), tid(t, "dev")
	w.sending(ops)
	w.sending(dev)
	before := w.store(ops).Outbox
	refusedCode(t, "a report with two teams and no --team", firstOf(w.report(nil, reportText)), exits.CodeAmbiguousTeam)
	if got := w.store(ops).Outbox; !reflect.DeepEqual(got, before) {
		t.Errorf("a refused report changed the store: %+v, was %+v", got, before)
	}
	reportPosted(t, w.report(&dev, reportText))
	refusedCode(t, "a team not held", firstOf(w.report(new(tid(t, "nope")), reportText)), exits.CodeUsage)
}

// RP3, one team: --team may be left out, as the team is the only one held.
func TestRP3TheOnlyTeamNeedsNoFlag(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.sending(ops)
	reportPosted(t, w.report(nil, reportText))
}

// RP3 step 2 before step 3: the role check comes before the size check, so a probe sending a
// report over the cap, an empty one or a secret is 11 role, where a worker's are 10; the order
// the client leaves to the link (the client refuses no size).
func TestRP3TheRoleCheckComesBeforeTheSizeCheck(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sending(ops)
	probe := sendRoster(ops)
	probe.Members[agent(ops, 1)] = proto.RosterMember{Role: proto.RoleProbe}
	f.notices.set(probe)
	before := w.store(ops).Outbox
	for name, text := range map[string]string{
		"over the cap": "H\n" + strings.Repeat("x", 1600), "empty": "", "a secret": "Done\n" + memberAccess,
	} {
		refusedCode(t, "a probe's report, "+name, firstOf(w.report(&ops, text)), exits.CodeRole)
	}
	if got := w.store(ops).Outbox; !reflect.DeepEqual(got, before) {
		t.Errorf("a refused report changed the store: %+v", got)
	}
	f.notices.set(sendRoster(ops))
	refusedCode(t, "a worker's report over the cap", firstOf(w.report(&ops, "H\n"+strings.Repeat("x", 1600))), exits.CodeBody)
}

// RP3 step 3: the size, then the headline; each is 10 body, its message naming which, and
// nothing is charged. The cap is 1500 bytes, never cut: 1500 posts, 1501 does not.
func TestRP3TheSizeAndTheHeadline(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.sending(ops)
	before := w.store(ops).Outbox
	for name, c := range map[string]struct{ text, says string }{
		"1501 bytes":              {"H\n" + strings.Repeat("x", 1499), "1500 bytes"},
		"an empty first line":     {"\nbody", "empty"},
		"a blank first line":      {"  \t\nbody", "empty"},
		"an empty report":         {"", "empty"},
		"a headline of 121":       {strings.Repeat("h", 121) + "\nbody", "over 120"},
		"a headline of 121 runes": {strings.Repeat("é", 121), "over 120"},
	} {
		d := firstOf(w.report(&ops, c.text))
		refusedCode(t, name, d, exits.CodeBody)
		if !strings.Contains(d.Error.Message, c.says) {
			t.Errorf("%s: the message %q does not say %q", name, d.Error.Message, c.says)
		}
	}
	if got := w.store(ops).Outbox; !reflect.DeepEqual(got, before) {
		t.Errorf("a refused report changed the store: %+v", got)
	}
	reportPosted(t, w.report(&ops, "H\n"+strings.Repeat("x", 1498)))
	reportPosted(t, w.report(&ops, strings.Repeat("é", 120)))
}

// RP3 step 4: the secret check, after the form and before the room: 10 secret, nothing charged,
// and the room is not asked.
func TestRP3TheSecretCheck(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sending(ops)
	before := w.store(ops).Outbox
	refusedCode(t, "a report holding the token", firstOf(w.report(&ops, "Deployed\nkey "+memberAccess)), exits.CodeSecret)
	if got := w.store(ops).Outbox; !reflect.DeepEqual(got, before) || f.reportsVerifies != 0 {
		t.Errorf("a refused report changed the store or asked the room: %+v, %d verifications", got, f.reportsVerifies)
	}
}

// RP3 step 6, n3f-D6: report_per_hour reports in the last 3600 s is 14 report-rate, the message
// naming when the oldest leaves the window and that an unposted report still counts; the
// refusal writes nothing; the oldest leaving is at 3600 s exactly.
func TestRP3TheReportWindow(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	now := w.clk.WallMS()
	times := []int64{now - 3_000_000, now - 2_000_000, now - 1_000_000, now - 900_000, now - 800_000, now - 700_000}
	w.sendingWith(ops, w.withReportTimes(ops, times...))
	before := w.store(ops).Outbox
	d := firstOf(w.report(&ops, reportText))
	refusedCode(t, "the seventh report in an hour", d, exits.CodeReportRate)
	for _, say := range []string{"600 seconds", "not posted still counts"} {
		if !strings.Contains(d.Error.Message, say) {
			t.Errorf("the message %q does not say %q", d.Error.Message, say)
		}
	}
	if got := w.store(ops).Outbox; !reflect.DeepEqual(got, before) {
		t.Errorf("a refused report wrote the store: %+v, was %+v", got, before)
	}
	w.advanceTo(now + 599_999)
	d = firstOf(w.report(&ops, reportText))
	refusedCode(t, "1 ms before the oldest leaves", d, exits.CodeReportRate)
	if !strings.Contains(d.Error.Message, "1 seconds") {
		t.Errorf("the message %q does not round up to a second", d.Error.Message)
	}
	w.advanceTo(now + 600_000)
	reportPosted(t, w.report(&ops, reportText))
	want := append(slices.Clone(times[1:]), now+600_000)
	if got := w.store(ops).Outbox.Reports; !slices.Equal(got, want) {
		t.Errorf("the window after the post = %v, want the oldest dropped and this report added: %v", got, want)
	}
}

// RP3 step 6: the send bucket is 14 rate and the same text within 60 s 15 duplicate, each
// spending nothing of the report window.
func TestRP3TheGateStillJudgesAReport(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.sending(ops)
	now := w.clk.WallMS()
	reportPosted(t, w.report(&ops, reportText))
	refusedCode(t, "the same report twice", firstOf(w.report(&ops, reportText)), exits.CodeDuplicate)
	if got := w.store(ops).Outbox.Reports; !slices.Equal(got, []int64{now}) {
		t.Errorf("a duplicate spent the window: %v", got)
	}
	w.local(func() {
		g := &w.e.teams[ops].store.Outbox.Gate
		g.Credit, g.AtMS = 0, now
	})
	d := firstOf(w.report(&ops, "Another\nreport"))
	refusedCode(t, "a report with the bucket empty", d, exits.CodeRate)
	if got := w.store(ops).Outbox.Reports; !slices.Equal(got, []int64{now}) {
		t.Errorf("a refused report spent the window: %v", got)
	}
}

// SD3-1 for a report: the window, the gate and the entry are one store write, so a store that
// cannot be written charges none of them.
func TestRP3TheChargeIsOneStoreWrite(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sending(ops)
	before := w.store(ops).Outbox
	w.local(func() { check(t, w.e.teams[ops].writer.Close()) })
	if d := firstOf(w.report(&ops, reportText)); d.Error == nil || d.Error.Code != exits.CodeStore {
		t.Fatalf("a report whose write failed = %+v", d)
	}
	if got := w.store(ops).Outbox; !reflect.DeepEqual(got, before) {
		t.Errorf("the failed charge wrote %+v, was %+v", got, before)
	}
	var mem state.Outbox
	w.local(func() { mem = w.e.teams[ops].store.Outbox })
	if !reflect.DeepEqual(mem, before) {
		t.Errorf("memory ran ahead of the disk: %+v", mem)
	}
	if len(f.notices.sent()) != 0 {
		t.Errorf("a failed charge was sent: %v", f.notices.sent())
	}
	w.local(func() {
		h := w.e.teams[ops]
		var err error
		h.writer, err = state.NewStoreWriter(w.root, h.lock)
		check(t, err)
	})
}

// RP4: a room that fails its check is 22 untrusted, one the homeserver forbids 22 forbidden;
// nothing is charged, the window and the gate stay, and main goes on: its sends post and its
// items are kept.
func TestRP4AFailedCheckChargesNothingAndLeavesMain(t *testing.T) {
	for name, fail := range map[string]struct {
		err  func(paths.TeamID) error
		code string
	}{
		"untrusted": {func(id paths.TeamID) error { return untrustedReports(t, id) }, exits.CodeUntrusted},
		"forbidden": {forbiddenReports, exits.CodeForbidden},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, state.DeliverySocket)
			ops := tid(t, "ops")
			f := w.sending(ops)
			f.mu.Lock()
			f.reportsAnswers = []error{fail.err(ops)}
			f.mu.Unlock()
			before := w.store(ops).Outbox
			refusedCode(t, "a report whose room fails", firstOf(w.report(&ops, reportText)), fail.code)
			if got := w.store(ops).Outbox; !reflect.DeepEqual(got, before) {
				t.Errorf("the refusal charged: %+v, was %+v", got, before)
			}
			if len(f.notices.sent()) != 0 {
				t.Errorf("a refused report was sent: %v", f.notices.sent())
			}
			posted(t, w.say(&ops, "main goes on", "repo.2"))
			w.persist(ops, humanItem(ops, 1, w.clk.WallMS(), "still delivered"))
			if n := len(w.store(ops).Pending); n != 1 {
				t.Errorf("main holds %d pending items after the reports failure, want 1", n)
			}
		})
	}
}

// RP3's order: the secret check (4) comes before the room (5), and the room before Admit (6),
// so a report that would be report-rate is 22 when its room fails, and the room is asked once.
func TestRP3TheRoomIsCheckedBeforeTheWindow(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	now := w.clk.WallMS()
	times := []int64{now - 3_000_000, now - 2_000_000, now - 1_000_000, now - 900_000, now - 800_000, now - 700_000}
	f := w.sendingWith(ops, w.withReportTimes(ops, times...))
	f.mu.Lock()
	f.reportsAnswers = []error{untrustedReports(t, ops)}
	f.mu.Unlock()
	refusedCode(t, "a report over the window with a failing room", firstOf(w.report(&ops, reportText)), exits.CodeUntrusted)
	if f.reportsVerifies != 1 {
		t.Errorf("the room was asked %d times, want once", f.reportsVerifies)
	}
}

// RP4, n3d-D2: a verification that fails transiently (the homeserver did not answer) does not
// refuse: the report is charged and queued at once, exit 0, and the sender verifies it again
// and posts it.
func TestRP4ATransientCheckQueuesTheReport(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sending(ops)
	now := w.clk.WallMS()
	f.mu.Lock()
	f.reportsAnswers = []error{errors.New("the homeserver did not answer"), nil}
	f.mu.Unlock()
	reportQueued(t, w.report(&ops, reportText))
	w.until(w.sendsLeft(ops))
	if got := f.notices.sentRooms(); !slices.Equal(got, []string{reportsRoomOf(ops)}) {
		t.Errorf("the queued report went to %v", got)
	}
	if got := w.store(ops).Outbox.Reports; !slices.Equal(got, []int64{now}) {
		t.Errorf("the queued report's slot = %v, want [%d]", got, now)
	}
	if f.reportsVerifies != 2 {
		t.Errorf("%d verifications, want the verb's and the sender's", f.reportsVerifies)
	}
}

// RP4: a homeserver down during `report`: queued, exit 0, listed by status as a queued report,
// and posted after verification when the server returns.
func TestRP4AReportWhileTheServerIsDownIsQueuedThenPosted(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sending(ops)
	w.e.SetHealth(ops, team.Health{Down: true})
	f.mu.Lock()
	f.reportsAnswers = []error{errors.New("the homeserver is down")}
	f.mu.Unlock()
	reportQueued(t, w.report(&ops, reportText))
	q := w.queuedRecords()
	if len(q) != 1 || q[0].Room != proto.PurposeReports || q[0].Send != state.SendReport || len(q[0].To) != 0 {
		t.Fatalf("status lists %+v, want one queued report with no addressees", q)
	}
	if len(f.notices.sent()) != 0 {
		t.Fatalf("a report was posted while the team was down: %v", f.notices.sent())
	}
	f.mu.Lock()
	f.reportsAnswers = nil
	f.mu.Unlock()
	w.e.SetHealth(ops, up(startWall))
	w.until(w.sendsLeft(ops))
	if got := f.notices.sentRooms(); !slices.Equal(got, []string{reportsRoomOf(ops)}) {
		t.Errorf("the report went to %v", got)
	}
}

// RP4, n3f-D16: a queued report whose room fails verification at the sender is removed, logged
// by its transaction id and counted unsent, nothing posted; its slot stays spent (n3f-D6).
func TestRP4AQueuedReportThatFailsAtTheSenderIsRemoved(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sending(ops)
	now := w.clk.WallMS()
	w.e.SetHealth(ops, team.Health{Down: true})
	f.mu.Lock()
	f.reportsAnswers = []error{nil, untrustedReports(t, ops)}
	f.mu.Unlock()
	reportQueued(t, w.report(&ops, reportText))
	txn := w.store(ops).Outbox.Sending[0].Txn
	w.e.SetHealth(ops, up(startWall))
	w.until(w.sendsLeft(ops))
	if len(f.notices.sent()) != 0 {
		t.Errorf("a report whose room failed was posted: %v", f.notices.sent())
	}
	if !strings.Contains(w.log.String(), txn) {
		t.Errorf("the removal is not logged by its transaction id %s: %s", txn, w.log.String())
	}
	if tr := w.teamRecord(ops); tr.Counts.Unsent != 1 {
		t.Errorf("status counts %d unsent, want 1", tr.Counts.Unsent)
	}
	if got := w.store(ops).Outbox.Reports; !slices.Equal(got, []int64{now}) {
		t.Errorf("the removed report's slot = %v, want it spent: [%d]", got, now)
	}
}

// RP4: a report whose verb still waits is answered with the sender's refusal, 22, when its room
// fails at the sender after the verb's own check passed.
func TestRP4AWaitingVerbIsAnsweredTheSendersRefusal(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sending(ops)
	f.mu.Lock()
	f.reportsAnswers = []error{nil, untrustedReports(t, ops)}
	f.mu.Unlock()
	refusedCode(t, "a report whose room changed between the verb and the post", firstOf(w.report(&ops, reportText)), exits.CodeUntrusted)
	if len(f.notices.sent()) != 0 || len(w.store(ops).Outbox.Sending) != 0 {
		t.Errorf("posted %v, in flight %d", f.notices.sent(), len(w.store(ops).Outbox.Sending))
	}
}

// RP4, SD3-8c: a PUT to the reports room answered 403 removes that report alone and answers it
// 22 forbidden; unlike a main send it stops nothing, so main's next send posts.
func TestRP4AReportForbiddenAtThePutStopsNothing(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sending(ops)
	f.notices.setSendErr(matrix.StatusError("PUT", "/send", 403, "M_FORBIDDEN"))
	refusedCode(t, "a report forbidden at the PUT", firstOf(w.report(&ops, reportText)), exits.CodeForbidden)
	f.notices.setSendErr(nil)
	posted(t, w.say(&ops, "main goes on", "repo.2"))
	if tr := w.teamRecord(ops); tr.Counts.Unsent != 1 || tr.Down {
		t.Errorf("the team record after a forbidden report: %+v", tr)
	}
	if n := len(w.store(ops).Outbox.Reports); n != 1 {
		t.Errorf("the forbidden report's slot = %d entries, want it spent", n)
	}
}

// RP6: the verb's deadline is the request's, and the reports state read and the join are inside
// it: a verification that is still waiting when the request ends is 4 timed-out, nothing charged.
func TestRP6TheVerificationIsInsideTheVerbsDeadline(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sending(ops)
	entered := make(chan struct{})
	f.mu.Lock()
	f.reportsHold = func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}
	f.mu.Unlock()
	before := w.store(ops).Outbox
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	got := make(chan answer, 1)
	text := reportText
	go func() { got <- w.request(ctx, wire.OpReport, wire.ReportArgs{Team: &ops}, &text) }()
	<-entered
	cancel()
	refusedCode(t, "a report whose verification outlived its deadline", firstOf(<-got), exits.CodeTimedOut)
	if after := w.store(ops).Outbox; !reflect.DeepEqual(after, before) {
		t.Errorf("a report that timed out charged: %+v", after)
	}
}

// n3f-D16: the sender's verification is bounded by a send's 30 s on the engine's clock, so a
// room that does not answer cannot hold the team's sends for ever: the report stays queued and
// is tried again, and main's sends behind it go on once it is.
func TestN3fD16TheSendersVerificationIsBounded(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sending(ops)
	w.e.SetHealth(ops, team.Health{Down: true})
	reportQueued(t, w.report(&ops, reportText))
	ended := make(chan struct{})
	f.mu.Lock()
	f.reportsHold = func(ctx context.Context) error {
		<-ctx.Done()
		close(ended)
		return ctx.Err()
	}
	f.mu.Unlock()
	w.e.SetHealth(ops, up(startWall))
	w.awaitDue(30 * time.Second)
	w.clk.Advance(30 * time.Second)
	<-ended
	f.mu.Lock()
	f.reportsHold = nil
	f.mu.Unlock()
	if n := len(w.store(ops).Outbox.Sending); n != 1 {
		t.Fatalf("a verification that did not answer removed the report: %d in flight", n)
	}
	w.awaitDue(30 * time.Second)
	w.clk.Advance(30 * time.Second)
	w.until(w.sendsLeft(ops))
	if got := f.notices.sentRooms(); !slices.Equal(got, []string{reportsRoomOf(ops)}) {
		t.Errorf("the report went to %v after its retry", got)
	}
}

// n3d-D27 for a report: the queued content is composed again with the roster at its post, so a
// report whose author became the probe meanwhile is removed 11 role, not posted.
func TestN3dD27AQueuedReportIsCheckedAgainstTheRoster(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sending(ops)
	w.e.SetHealth(ops, team.Health{Down: true})
	reportQueued(t, w.report(&ops, reportText))
	probe := sendRoster(ops)
	probe.Members[agent(ops, 1)] = proto.RosterMember{Role: proto.RoleProbe}
	f.notices.set(probe)
	w.e.SetHealth(ops, up(startWall))
	w.until(w.sendsLeft(ops))
	if len(f.notices.sent()) != 0 {
		t.Errorf("a report whose author became the probe was posted: %v", f.notices.sent())
	}
	if tr := w.teamRecord(ops); tr.Counts.Unsent != 1 {
		t.Errorf("status counts %d unsent, want 1", tr.Counts.Unsent)
	}
}

// RP3: a report request with no text, or arguments that are not report's, is the link's own
// refusal: 64 for no stdin, 10 schema for bad args.
func TestRP3ARequestWithoutItsTextOrArgs(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.sending(ops)
	a := w.request(t.Context(), wire.OpReport, wire.ReportArgs{Team: &ops}, nil)
	refusedCode(t, "a report with no stdin", firstOf(a), exits.CodeUsage)
	if d := w.done(wire.OpReport, map[string]any{"nope": 1}); d.Error == nil || d.Error.Code != exits.CodeSchema {
		t.Errorf("report with other args = %+v", d)
	}
}

// RP1, RP4: status's reports room record is the team's reading, from memory: joined, joining,
// refused with its errcode, untrusted with the failed check. The questions room's is unchanged.
func TestStatusShowsTheReportsRoomsState(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sending(ops)
	reports := func() records.Room {
		for _, r := range w.statusRecords(&ops) {
			if room, ok := r.(records.Room); ok && room.Purpose == proto.PurposeReports {
				return room
			}
		}
		t.Fatal("status shows no reports room")
		return records.Room{}
	}
	for _, c := range []struct {
		in    team.ReportsRoom
		state string
		check *string
	}{
		{team.ReportsRoom{State: team.ReportsJoined}, "joined", nil},
		{team.ReportsRoom{State: team.ReportsJoining}, "joining", nil},
		{team.ReportsRoom{State: team.ReportsRefused, Check: "M_FORBIDDEN"}, "refused", new("M_FORBIDDEN")},
		{team.ReportsRoom{State: team.ReportsUntrusted, Check: "roster"}, "untrusted", new("roster")},
	} {
		f.mu.Lock()
		f.reports = c.in
		f.mu.Unlock()
		got := reports()
		if got.State != c.state || !reflect.DeepEqual(got.Check, c.check) || got.Uncounted != nil {
			t.Errorf("a %s room shows %+v, want state %s check %v", c.in.State, got, c.state, deref(c.check))
		}
	}
	for _, r := range w.statusRecords(&ops) {
		// The team's questions health was never set: a worker's record, whatever the reports room.
		if room, ok := r.(records.Room); ok && room.Purpose == proto.PurposeQuestions && room.State != roomNotCoordinator {
			t.Errorf("the questions room's record changed: %+v", room)
		}
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
