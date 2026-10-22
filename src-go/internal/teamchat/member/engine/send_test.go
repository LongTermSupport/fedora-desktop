package engine

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
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

// memberAccess is the member's own access token, which its free text may never hold (R43).
const memberAccess = "syt_cmVwby4x_abcdefghijklmnopqrst_0a1b2c"

// fakeForge is the forge checker: every check answers result and err.
type fakeForge struct {
	mu     sync.Mutex
	result team.ForgeResult
	err    error
	calls  int
}

func (f *fakeForge) Check(context.Context, proto.ForgeCheck, proto.Forge) (team.ForgeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.result, f.err
}

func (f *fakeForge) set(outcome string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.result, f.err = team.ForgeResult{Outcome: outcome}, err
}

func (f *fakeForge) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// sendRoster is team id's roster for the sends: human alice, workers agent 1 (this member) and
// agent 2, and agent 3 the coordinator.
func sendRoster(id paths.TeamID) proto.Roster {
	return proto.Roster{
		Team: id.String(), Name: "Operations", Rev: 1, Humans: []string{alice(id)},
		Members: map[string]proto.RosterMember{
			agent(id, 1): {Role: proto.RoleWorker, HumanText: true},
			agent(id, 2): {Role: proto.RoleWorker, HumanText: true},
			agent(id, 3): {Role: proto.RoleCoordinator, HumanText: true},
		},
		Rooms:  proto.SideRooms{Questions: qRoom, Reports: "!" + strings.Repeat("r", 43)},
		Forge:  proto.Forge{Kind: proto.ForgeGitHub, Org: "example-org"},
		Limits: proto.DefaultLimits(),
	}
}

// coordinatorRoster is sendRoster with this member (agent 1) the coordinator and agent 3 a
// worker: the roster for a halt, which only a coordinator sends (A 8.1).
func coordinatorRoster(id paths.TeamID) proto.Roster {
	r := sendRoster(id)
	r.Members[agent(id, 1)] = proto.RosterMember{Role: proto.RoleCoordinator, HumanText: true}
	r.Members[agent(id, 3)] = proto.RosterMember{Role: proto.RoleWorker, HumanText: true}
	return r
}

// sendingWith is id held over s with its verified roster and its sends recovered, so its
// sender posts.
func (w *world) sendingWith(id paths.TeamID, s state.Store) *fakeTeam {
	w.t.Helper()
	f := w.add(id, encodeStore(w.t, s))
	<-f.running
	f.notices.set(sendRoster(id))
	f.notices.mu.Lock()
	f.notices.token = memberAccess
	f.notices.mu.Unlock()
	w.recover(id, f)
	return f
}

// downWith is sendingWith for a team down before its recovery, so its sender posts none of the
// store's queued sends.
func (w *world) downWith(id paths.TeamID, s state.Store) {
	w.t.Helper()
	f := w.add(id, encodeStore(w.t, s))
	<-f.running
	f.notices.set(sendRoster(id))
	w.e.SetHealth(id, team.Health{Down: true})
	w.recover(id, f)
}

func (w *world) sending(id paths.TeamID) *fakeTeam {
	w.t.Helper()
	return w.sendingWith(id, w.newStore(id))
}

func (w *world) recover(id paths.TeamID, f *fakeTeam) {
	w.t.Helper()
	check(w.t, w.e.RecoverSends(w.t.Context(), id, f.notices))
}

// request is one send request with its stdin.
func (w *world) request(ctx context.Context, op string, args any, stdin *string) answer {
	w.t.Helper()
	raw, err := json.Marshal(args)
	check(w.t, err)
	out := &recorder{}
	return answer{w.e.Serve(ctx, wire.Request{V: 1, Op: op, Args: raw, Stdin: stdin}, out), out}
}

func (w *world) say(tm *paths.TeamID, text string, to ...string) answer {
	w.t.Helper()
	return w.request(w.t.Context(), wire.OpSay, wire.SayArgs{Team: tm, To: to}, &text)
}

func (w *world) ping(tm *paths.TeamID, verb string, ref *string, to ...string) answer {
	w.t.Helper()
	return w.request(w.t.Context(), wire.OpPing, wire.PingArgs{Team: tm, Verb: verb, Ref: ref, To: to}, nil)
}

// answer is a send request's done and its records.
type answer struct {
	d   wire.Done
	out *recorder
}

func firstOf(a answer) wire.Done { return a.d }

// sentOf is the request's one sent record.
func sentOf(t *testing.T, a answer) records.Sent {
	t.Helper()
	d, out := a.d, a.out
	if d.Error != nil || len(out.recs) != 1 {
		t.Fatalf("the send = %+v %v, records %s", d, d.Error, out.kinds())
	}
	s, ok := out.recs[0].(records.Sent)
	if !ok {
		t.Fatalf("the record is %s", out.recs[0].Kind())
	}
	return s
}

func posted(t *testing.T, a answer) string {
	t.Helper()
	s := sentOf(t, a)
	if s.Queued || s.ID == nil || s.Room != proto.PurposeMain {
		t.Fatalf("the send was not posted: %+v", s)
	}
	return *s.ID
}

func queued(t *testing.T, a answer) {
	t.Helper()
	if s := sentOf(t, a); !s.Queued || s.ID != nil || s.Room != proto.PurposeMain {
		t.Fatalf("the send was not queued: %+v", s)
	}
}

func refusedCode(t *testing.T, what string, d wire.Done, code string) {
	t.Helper()
	if d.Error == nil || d.Error.Code != code {
		t.Fatalf("%s = %+v %v, want %s", what, d, d.Error, code)
	}
	exit, ok := exits.ExitOf(code)
	if !ok || d.Exit != int(exit) {
		t.Fatalf("%s exits %d, want %d", what, d.Exit, exit)
	}
}

func (n *fakeNotices) sent() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.txns)
}

func (n *fakeNotices) setSendErr(err error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sendErr = err
}

func (n *fakeNotices) owned() map[string]proto.Related {
	n.mu.Lock()
	defer n.mu.Unlock()
	return maps.Clone(n.own)
}

// lastContent is the content of f's last send.
func lastContent(t *testing.T, f *fakeTeam) []byte {
	t.Helper()
	f.notices.mu.Lock()
	defer f.notices.mu.Unlock()
	if len(f.notices.contents) == 0 {
		t.Fatal("nothing was sent")
	}
	return f.notices.contents[len(f.notices.contents)-1]
}

// statusRecords are status's records for every team, or only for team id when given.
func (w *world) statusRecords(id *paths.TeamID) []records.Record {
	w.t.Helper()
	d, out := w.serve(w.t.Context(), wire.OpStatus, wire.TeamsArgs{Team: id})
	if d.Error != nil {
		w.t.Fatalf("status = %v", d.Error)
	}
	return out.recs
}

func (w *world) queuedRecords() []records.Queued {
	w.t.Helper()
	var got []records.Queued
	for _, r := range w.statusRecords(nil) {
		if q, ok := r.(records.Queued); ok {
			got = append(got, q)
		}
	}
	return got
}

func (w *world) teamRecord(id paths.TeamID) records.Team {
	w.t.Helper()
	for _, r := range w.statusRecords(&id) {
		if tr, ok := r.(records.Team); ok {
			return tr
		}
	}
	w.t.Fatal("no team record")
	return records.Team{}
}

func contentOf(t *testing.T, raw []byte) proto.Content {
	t.Helper()
	v, err := proto.Parse(raw)
	check(t, err)
	c, reason := proto.DecodeContent(v, proto.MainKeys)
	if reason != "" {
		t.Fatalf("the content does not decode: %s", reason)
	}
	return c
}

// sendsLeft is how many sends team id's store holds in flight, read on the state goroutine.
func (w *world) sendsLeft(id paths.TeamID) func() bool {
	return func() bool { return len(w.e.teams[id].store.Outbox.Sending) == 0 }
}

// waitFor polls cond, giving the state goroutine a turn each time.
func waitFor(w *world, cond func() bool) {
	w.t.Helper()
	for !cond() {
		w.local(func() {})
	}
}

func up(sync int64) team.Health { return team.Health{LastSyncOKMS: sync} }

// SD2-1: names resolve per team: a name two teams hold needs --team; an unknown name and an
// ambiguous short name are refused; --to-coordinator is the team's coordinator.
func TestSD2_1NamesResolvePerTeam(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops, dev := tid(t, "ops"), tid(t, "dev")
	fo := w.sending(ops)
	w.sending(dev)
	refusedCode(t, "repo.2 in two teams", firstOf(w.say(nil, "hello", "repo.2")), exits.CodeAmbiguousTeam)
	posted(t, w.say(&ops, "hello", "repo.2"))
	refusedCode(t, "nobody", firstOf(w.say(nil, "hello", "nobody")), exits.CodeUnknownMember)
	refusedCode(t, "nobody in ops", firstOf(w.say(&ops, "hello", "nobody")), exits.CodeUnknownMember)

	twin := "@repo.2+other.podman:" + paths.ServerName(ops)
	r := sendRoster(ops)
	r.Members[twin] = proto.RosterMember{Role: proto.RoleWorker, HumanText: true}
	fo.notices.set(r)
	refusedCode(t, "an ambiguous short name", firstOf(w.say(&ops, "hello", "repo.2")), exits.CodeAmbiguousMember)
	posted(t, w.say(&ops, "hello", "repo.2+other.podman"))
	if c := contentOf(t, lastContent(t, fo)); !slices.Equal(c.Text.To, []string{twin}) {
		t.Errorf("the full handle is to %v", c.Text.To)
	}
	posted(t, w.say(&ops, "hi alice", "alice", "repo.3"))

	posted(t, w.request(t.Context(), wire.OpSay, wire.SayArgs{Team: &ops, ToCoordinator: true}, new("to the coordinator")))
	if c := contentOf(t, lastContent(t, fo)); !slices.Equal(c.Text.To, []string{agent(ops, 3)}) {
		t.Errorf("--to-coordinator is to %v", c.Text.To)
	}
	refusedCode(t, "neither --to nor --to-coordinator", firstOf(w.request(t.Context(), wire.OpSay, wire.SayArgs{Team: &ops}, new("x"))), exits.CodeUsage)
	refusedCode(t, "both", firstOf(w.request(t.Context(), wire.OpSay, wire.SayArgs{Team: &ops, To: []string{"alice"}, ToCoordinator: true}, new("x"))), exits.CodeUsage)
	other := tid(t, "elsewhere")
	refusedCode(t, "a team not held", firstOf(w.say(&other, "x", "alice")), exits.CodeUsage)
}

// SD2-2: ping with an answer verb points at reply --verb; ping takes no stdin. The bare ping is
// cli's TestABarePingListsTheVerbs.
func TestSD2_2PingRefusesAnswerVerbs(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.sending(ops)
	for _, verb := range []string{proto.VerbAck, proto.VerbNack} {
		d := firstOf(w.ping(&ops, verb, nil, "repo.2"))
		refusedCode(t, "ping "+verb, d, exits.CodeUsage)
		if !strings.Contains(d.Error.Message, "reply") || !strings.Contains(d.Error.Message, "--verb") {
			t.Errorf("ping %s says %q", verb, d.Error.Message)
		}
	}
	d := firstOf(w.request(t.Context(), wire.OpPing, wire.PingArgs{Team: &ops, Verb: "halt", To: []string{"repo.2"}}, new("text")))
	refusedCode(t, "ping with stdin", d, exits.CodeUsage)
}

// n3d-D28: say's text is on stdin, not empty, UTF-8, within the cap and no secret.
func TestN3dD28SayReadsItsText(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.sending(ops)
	refusedCode(t, "no stdin", firstOf(w.request(t.Context(), wire.OpSay, wire.SayArgs{Team: &ops, To: []string{"alice"}}, nil)), exits.CodeUsage)
	refusedCode(t, "empty", firstOf(w.say(&ops, "", "alice")), exits.CodeEmpty)
	refusedCode(t, "not UTF-8", firstOf(w.say(&ops, "a\xffb", "alice")), exits.CodeSchema)
	refusedCode(t, "over 4096 bytes", firstOf(w.say(&ops, strings.Repeat("a", 4097), "alice")), exits.CodeSize)
	refusedCode(t, "the own token", firstOf(w.say(&ops, "token "+memberAccess, "alice")), exits.CodeSecret)
	if d := w.done(wire.OpSay, map[string]any{"nope": 1}); d.Error == nil || d.Error.Code != exits.CodeSchema {
		t.Fatalf("say with bad arguments = %+v", d)
	}
	if d := w.done(wire.OpPing, map[string]any{"nope": 1}); d.Error == nil || d.Error.Code != exits.CodeSchema {
		t.Fatalf("ping with bad arguments = %+v", d)
	}
}

// SD3-1: the charge and the sending entry are one write, before any PUT: a write that fails
// spends nothing and sends nothing.
func TestSD3_1TheChargeAndTheEntryAreOneWrite(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sending(ops)
	before := w.store(ops).Outbox
	w.local(func() { check(t, w.e.teams[ops].writer.Close()) })
	if d := firstOf(w.say(&ops, "hello", "repo.2")); d.Error == nil {
		t.Fatalf("a send whose write failed = %+v", d)
	}
	if got := w.store(ops).Outbox; got.Gate.Credit != before.Gate.Credit || len(got.Sending) != 0 || len(got.Gate.Recent) != 0 {
		t.Errorf("the failed charge wrote %+v", got)
	}
	var mem state.Outbox
	w.local(func() { mem = w.e.teams[ops].store.Outbox })
	if mem.Gate.Credit != before.Gate.Credit || len(mem.Sending) != 0 {
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

// SD3-3: the duplicate window: the same text to the same addressee is refused within 60 s and
// sent at 60 s; different texts both send; two identical sends racing, one is refused.
func TestSD3_3TheDuplicateWindow(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.sending(ops)
	start := w.clk.WallMS()
	posted(t, w.say(&ops, "hello", "repo.2"))
	w.advanceTo(start + 59_999)
	refusedCode(t, "the same text at 59.999 s", firstOf(w.say(&ops, "hello", "repo.2")), exits.CodeDuplicate)
	w.advanceTo(start + 60_000)
	posted(t, w.say(&ops, "hello", "repo.2"))
	posted(t, w.say(&ops, "hello again", "repo.2"))
	if n := len(w.store(ops).Outbox.Gate.Recent); n != 2 {
		t.Errorf("the window holds %d keys, want the two inside it", n)
	}

	var wg sync.WaitGroup
	codes := make([]string, 2)
	for i := range codes {
		wg.Go(func() {
			if d := firstOf(w.say(&ops, "racing", "repo.2")); d.Error != nil {
				codes[i] = d.Error.Code
			}
		})
	}
	wg.Wait()
	slices.Sort(codes)
	if !slices.Equal(codes, []string{"", exits.CodeDuplicate}) {
		t.Errorf("two racing sends = %v", codes)
	}
}

// SD3-4: send_burst 10 gives ten sends and the eleventh is rate, which spends nothing and
// enters no window; send_per_minute 20 refills one token in 3 s.
func TestSD3_4TheBurstAndTheRefill(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.sending(ops)
	for i := range 10 {
		posted(t, w.say(&ops, "text "+strconv.Itoa(i), "repo.2"))
	}
	before := w.store(ops).Outbox.Gate
	refusedCode(t, "the eleventh", firstOf(w.say(&ops, "the eleventh", "repo.2")), exits.CodeRate)
	if got := w.store(ops).Outbox.Gate; got.Credit != before.Credit || len(got.Recent) != len(before.Recent) {
		t.Errorf("a refused send changed the gate: %+v, was %+v", got, before)
	}
	w.advanceTo(w.clk.WallMS() + 2_999)
	refusedCode(t, "before the refill", firstOf(w.say(&ops, "the eleventh", "repo.2")), exits.CodeRate)
	w.advanceTo(w.clk.WallMS() + 1)
	posted(t, w.say(&ops, "the eleventh", "repo.2"))
}

// SD3-5: the order: the forge check comes before Admit, so a duplicate whose reference fails
// provenance is 13; an unknown addressee is refused before the secret check.
func TestSD3_5TheSendOrder(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.sending(ops)
	posted(t, w.ping(&ops, "review", new(reviewRef), "repo.3"))
	w.forge.set(proto.ReasonProvenance, nil)
	refusedCode(t, "a duplicate failing provenance", firstOf(w.ping(&ops, "review", new(reviewRef), "repo.3")), exits.CodeProvenance)
	refusedCode(t, "a secret to nobody", firstOf(w.say(&ops, "token "+memberAccess, "nobody")), exits.CodeUnknownMember)
}

// n3d-D11: the send side's forge results, each spending nothing.
func TestN3dD11TheForgeResults(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sending(ops)
	credit := w.store(ops).Outbox.Gate.Credit
	for _, c := range []struct {
		outcome string
		err     error
		code    string
	}{
		{proto.ReasonProvenance, nil, exits.CodeProvenance},
		{proto.ReasonUnresolved, nil, exits.CodeForge},
		{proto.ReasonForgeRate, nil, exits.CodeForgeRate},
		{"", team.ErrForgeDown, exits.CodeForgeDown},
		{"", team.ErrForgeLogin, exits.CodeForgeLogin},
		{"", context.DeadlineExceeded, exits.CodeForgeDown},
		{"what", nil, exits.CodeForge},
	} {
		w.forge.set(c.outcome, c.err)
		refusedCode(t, "forge "+c.outcome, firstOf(w.ping(&ops, "review", new(reviewRef), "repo.3")), c.code)
	}
	if got := w.store(ops).Outbox.Gate.Credit; got != credit {
		t.Errorf("a forge refusal spent %d", credit-got)
	}
	w.forge.set(team.ForgeProven, nil)
	f.notices.set(coordinatorRoster(ops))
	calls := w.forge.count()
	posted(t, w.ping(&ops, "halt", nil, "repo.3"))
	if w.forge.count() != calls {
		t.Error("a ping with no reference was forge-checked")
	}
}

// A nil forge checker answers no reference: 20 forge-down.
func TestN3dD11NoForgeCheckerIsForgeDown(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	w.local(func() { w.e.cfg.Forge = nil })
	ops := tid(t, "ops")
	w.sending(ops)
	refusedCode(t, "no checker", firstOf(w.ping(&ops, "review", new(reviewRef), "repo.3")), exits.CodeForgeDown)
}

// SD3-6 (offline): a down team's say is queued at once and listed; once up it is posted once by
// its txn; another team's say meanwhile is posted at once.
func TestSD3_6ADownTeamQueues(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops, dev := tid(t, "ops"), tid(t, "dev")
	fo := w.sending(ops)
	w.sending(dev)
	w.e.SetHealth(ops, team.Health{Down: true})
	queued(t, w.say(&ops, "while down", "repo.2"))
	q := w.queuedRecords()
	if len(q) != 1 || q[0].Team != ops || q[0].Send != state.SendText || q[0].Tried || !slices.Equal(q[0].To, records.List[string]{"repo.2"}) {
		t.Fatalf("status lists %+v", q)
	}
	posted(t, w.say(&dev, "meanwhile", "repo.2"))
	if len(fo.notices.sent()) != 0 {
		t.Fatalf("a down team was sent to: %v", fo.notices.sent())
	}
	txn := w.store(ops).Outbox.Sending[0].Txn
	w.e.SetHealth(ops, up(startWall))
	w.until(w.sendsLeft(ops))
	if got := fo.notices.sent(); !slices.Equal(got, []string{txn}) {
		t.Errorf("posted %v, want once by %s", got, txn)
	}
	if q := w.queuedRecords(); len(q) != 0 {
		t.Errorf("status still lists %+v", q)
	}
}

// SD3-8: a 401 on a waiting op's PUT removes its entry and answers 21; its token stays spent.
func TestSD3_8A401AnswersTheWaitingOp(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sending(ops)
	full := proto.FullBucket(proto.DefaultLimits(), w.clk.WallMS()).Credit
	f.notices.setSendErr(matrix.StatusError("PUT", "/send", 401, "M_UNKNOWN_TOKEN"))
	refusedCode(t, "a 401", firstOf(w.say(&ops, "hello", "repo.2")), exits.CodeToken)
	s := w.store(ops)
	if len(s.Outbox.Sending) != 0 || s.Outbox.Gate.Credit >= full {
		t.Errorf("after a 401: sending %d, credit %d of %d", len(s.Outbox.Sending), s.Outbox.Gate.Credit, full)
	}
	f.notices.setSendErr(matrix.StatusError("PUT", "/send", 403, "M_FORBIDDEN"))
	w.e.SetHealth(ops, up(startWall+1))
	refusedCode(t, "a 403", firstOf(w.say(&ops, "hello again", "repo.2")), exits.CodeForbidden)
}

// SD3-8b: a queued send outlives a 401: listed until the team runs again, then posted once.
func TestSD3_8bAQueuedSendOutlivesA401(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sending(ops)
	w.e.SetHealth(ops, team.Health{Down: true})
	queued(t, w.say(&ops, "while down", "repo.2"))
	f.notices.setSendErr(matrix.StatusError("PUT", "/send", 401, "M_UNKNOWN_TOKEN"))
	w.e.SetHealth(ops, up(startWall))
	waitFor(w, func() bool { return len(f.notices.sent()) == 1 })
	w.e.SetHealth(ops, team.Health{Stopped: exits.CodeToken, LastSyncOKMS: startWall})
	if q := w.queuedRecords(); len(q) != 1 || !q[0].Tried {
		t.Fatalf("after the 401 status lists %+v", q)
	}
	if tr := w.teamRecord(ops); !tr.Down {
		t.Errorf("the stopped team is not shown down: %+v", tr)
	}
	refusedCode(t, "a send to the stopped team", firstOf(w.say(&ops, "after", "repo.2")), exits.CodeToken)
	f.notices.setSendErr(nil)
	w.e.SetHealth(ops, up(startWall+5))
	w.until(w.sendsLeft(ops))
	if got := f.notices.sent(); len(got) != 2 || got[0] != got[1] {
		t.Errorf("posts %v, want the one txn twice (the refused try, then the post)", got)
	}
}

// SD3-9: the 33rd send in flight is 78 store, and an answer-expecting ping past 256 outbox and
// pending pings; nothing is charged.
func TestSD3_9TheSendingAndPingCaps(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops, dev := tid(t, "ops"), tid(t, "dev")
	s := w.newStore(ops)
	for i := range 32 {
		s.Outbox.Sending = append(s.Outbox.Sending, queuedText(w, ops, i))
	}
	w.downWith(ops, s)
	credit := w.store(ops).Outbox.Gate.Credit
	refusedCode(t, "the 33rd", firstOf(w.say(&ops, "one more", "repo.2")), exits.CodeStore)
	if got := w.store(ops).Outbox.Gate.Credit; got != credit {
		t.Errorf("a refused send spent %d", credit-got)
	}

	d := w.newStore(dev)
	for i := range 255 {
		d.Outbox.Pings = append(d.Outbox.Pings, outPing(i, startWall, agent(dev, 3)))
	}
	d.Outbox.Sending = append(d.Outbox.Sending, queuedHalt(w, dev))
	w.downWith(dev, d)
	refusedCode(t, "the 257th ping", firstOf(w.ping(&dev, "review", new(reviewRef), "repo.3")), exits.CodeStore)
	queued(t, w.ping(&dev, proto.VerbDone, new(reviewRef), "repo.3"))
}

// queuedText is an untried text send i of team id.
func queuedText(w *world, id paths.TeamID, i int) state.Sending {
	w.t.Helper()
	c := proto.Composer{Server: paths.ServerName(id), Self: agent(id, 1), Roster: sendRoster(id)}
	out, err := c.Text(proto.Text{To: []string{agent(id, 2)}, Text: "queued " + strconv.Itoa(i)}, "", "queued")
	check(w.t, err)
	return state.Sending{
		Txn: "tc" + strings.Repeat("0", 30) + strconv.FormatInt(int64(16+i), 16), Room: proto.PurposeMain,
		Kind: state.SendText, SinceMS: startWall, Type: proto.MessageType, Content: out.Content.Compact(),
	}
}

// queuedHalt is an untried halt to agent 2 of team id, from its coordinator.
func queuedHalt(w *world, id paths.TeamID) state.Sending {
	w.t.Helper()
	c := proto.Composer{Server: paths.ServerName(id), Self: agent(id, 3), Roster: sendRoster(id)}
	out, err := c.Ping(proto.Ping{Verb: proto.VerbHalt, To: []string{agent(id, 2)}}, "", "halt")
	check(w.t, err)
	return state.Sending{
		Txn: "tc" + strings.Repeat("1", 32), Room: proto.PurposeMain, Kind: state.SendPing, SinceMS: startWall,
		Type: proto.MessageType, Content: out.Content.Compact(),
		Ping: &state.SendingPing{Verb: proto.VerbHalt, To: []string{agent(id, 2)}, TimeoutMS: 60_000},
	}
}

// SD3-11: queued sends post oldest first, and a new send behind them answers queued; nothing is
// posted before the start's recovery.
func TestSD3_11QueuedSendsPostOldestFirst(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.add(ops, encodeStore(t, w.newStore(ops)))
	<-f.running
	f.notices.set(sendRoster(ops))
	w.e.SetHealth(ops, team.Health{Down: true})
	queued(t, w.say(&ops, "first", "repo.2"))
	w.e.SetHealth(ops, up(startWall))
	queued(t, w.say(&ops, "second", "repo.2"))
	s := w.store(ops)
	want := []string{s.Outbox.Sending[0].Txn, s.Outbox.Sending[1].Txn}
	if len(f.notices.sent()) != 0 {
		t.Fatal("the sender posted before its recovery")
	}
	w.recover(ops, f)
	w.until(w.sendsLeft(ops))
	if got := f.notices.sent(); !slices.Equal(got, want) {
		t.Errorf("posted %v, want %v", got, want)
	}
}

// SD3-12: a queued ping to a member parked meanwhile is removed at its post, logged and counted.
func TestSD3_12AParkedAddresseeIsNotPosted(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sending(ops)
	w.e.SetHealth(ops, team.Health{Down: true})
	queued(t, w.ping(&ops, "review", new(reviewRef), "repo.2"))
	r := sendRoster(ops)
	r.Members[agent(ops, 2)] = proto.RosterMember{Role: proto.RoleWorker, HumanText: true, Parked: true}
	f.notices.set(r)
	w.e.SetHealth(ops, up(startWall))
	w.until(w.sendsLeft(ops))
	if len(f.notices.sent()) != 0 {
		t.Errorf("a refused send was posted")
	}
	if !strings.Contains(w.log.String(), "not posted") {
		t.Errorf("no log line: %s", w.log.String())
	}
	if tr := w.teamRecord(ops); tr.Counts.Unsent != 1 {
		t.Errorf("unsent %d", tr.Counts.Unsent)
	}
}

// n3d-D3: with only a cached roster a down team's say is queued; with no roster at all it is
// 20 unreachable and nothing is charged.
func TestN3dD3ACachedRosterComposes(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops, dev := tid(t, "ops"), tid(t, "dev")
	check(t, w.root.MkdirAll(w.layout.Seat(ops).Dir()))
	check(t, state.Write(w.root, w.layout.Seat(ops).RosterCache(), state.RosterCache{Rev: 4, Content: rosterContent(ops, 4)}))
	<-w.add(ops, encodeStore(t, w.newStore(ops))).running
	w.e.SetHealth(ops, team.Health{Down: true})
	queued(t, w.say(&ops, "from the cache", "alice"))

	<-w.add(dev, encodeStore(t, w.newStore(dev))).running
	credit := w.store(dev).Outbox.Gate.Credit
	refusedCode(t, "no roster", firstOf(w.say(&dev, "nobody known", "alice")), exits.CodeUnreachable)
	refusedCode(t, "no roster, no --team", firstOf(w.say(nil, "nobody known", "repo.9")), exits.CodeUnknownMember)
	if w.store(dev).Outbox.Gate.Credit != credit {
		t.Error("a send with no roster was charged")
	}
	w.local(func() { w.e.teams[ops].roster = nil })
	refusedCode(t, "no roster anywhere", firstOf(w.say(nil, "x", "alice")), exits.CodeUnreachable)
}

// SD4-n3d: a posted answer-expecting ping enters the outbox with its due_ms; a say and a done
// do not.
func TestSD4n3dASentPingEntersTheOutbox(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.sending(ops)
	id := posted(t, w.ping(&ops, "review", new(reviewRef), "repo.3"))
	posted(t, w.say(&ops, "hello", "repo.3"))
	posted(t, w.ping(&ops, proto.VerbDone, new(reviewRef), "repo.3"))
	pings := w.store(ops).Outbox.Pings
	if len(pings) != 1 {
		t.Fatalf("the outbox holds %+v", pings)
	}
	p := pings[0]
	timeout := proto.DefaultLimits().AckTimeoutS * 1000
	if p.ID != id || p.Verb != "review" || *p.Ref != reviewRef || !slices.Equal(p.To, []string{agent(ops, 3)}) || p.DueMS != p.SentMS+timeout {
		t.Errorf("the outbox ping = %+v", p)
	}
	if o := w.outbox(); len(o) != 1 || o[0].ID != id {
		t.Errorf("status shows %+v", o)
	}
}

// SY4a-n3d: every post records the own send with the flags the lookup gives outbox pings.
func TestSY4an3dEachPostRecordsTheOwnSend(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sending(ops)
	f.notices.set(coordinatorRoster(ops))
	pingID := posted(t, w.ping(&ops, "halt", nil, "repo.3"))
	textID := posted(t, w.say(&ops, "hello alice", "alice"))
	bothID := posted(t, w.say(&ops, "hello both", "alice", "repo.2"))
	own := f.notices.owned()
	if r := own[pingID]; r != (proto.Related{Sender: agent(ops, 1), RoomID: mainRoom, Verb: "halt", AddressedToRosterAgent: true}) {
		t.Errorf("the ping's own send = %+v", r)
	}
	// A text to a human alone reaches no agent that hears humans (HumanReader).
	if r := own[textID]; r != (proto.Related{Sender: agent(ops, 1), RoomID: mainRoom}) {
		t.Errorf("the text to alice's own send = %+v", r)
	}
	if r := own[bothID]; r != (proto.Related{Sender: agent(ops, 1), RoomID: mainRoom, AddressedToRosterAgent: true}) {
		t.Errorf("the text to alice and repo.2's own send = %+v", r)
	}
}

// n3d-D5: a member's ping names at most 16 addressees.
func TestN3dD5AtMostSixteenAddressees(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sending(ops)
	r := coordinatorRoster(ops)
	var to []string
	for i := 10; i < 27; i++ {
		r.Members[agent(ops, i)] = proto.RosterMember{Role: proto.RoleWorker, HumanText: true}
		to = append(to, "repo."+strconv.Itoa(i))
	}
	f.notices.set(r)
	refusedCode(t, "17 addressees", firstOf(w.ping(&ops, "halt", nil, to...)), exits.CodeUsage)
	posted(t, w.ping(&ops, "halt", nil, to[:16]...))
}

// SY8: a team stopped permanently refuses a send with its stop.
func TestAStoppedTeamRefusesSends(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.sending(ops)
	for _, code := range []string{exits.CodeUntrusted, exits.CodeStore} {
		w.e.SetHealth(ops, team.Health{Stopped: code})
		refusedCode(t, "stopped "+code, firstOf(w.say(&ops, "x", "alice")), code)
	}
}

// SD3-7 in process: a request that ends while every try fails transiently is answered queued;
// the post goes on 30 s later, by the same txn.
func TestSD3_7ADeadlineWhileWaitingAnswersQueued(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.sending(ops)
	f.notices.setSendErr(matrix.StatusError("PUT", "/send", 502, ""))
	ctx, cancel := context.WithCancel(t.Context())
	got := make(chan answer, 1)
	go func() {
		got <- w.request(ctx, wire.OpSay, wire.SayArgs{Team: &ops, To: []string{"repo.2"}}, new("slow"))
	}()
	waitFor(w, func() bool { return len(f.notices.sent()) == 1 })
	cancel()
	a := <-got
	queued(t, a)
	f.notices.setSendErr(nil)
	check(t, w.clk.BlockUntil(t.Context(), 1))
	w.clk.Advance(30 * time.Second)
	w.until(w.sendsLeft(ops))
	if got := f.notices.sent(); len(got) != 2 || got[0] != got[1] {
		t.Errorf("posts %v, want one txn tried twice", got)
	}
}

// n3d-D23: a removal drops the team's queued sends with one log line counting them.
func TestN3dD23ARemovalDropsQueuedSends(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.sending(ops)
	w.e.SetHealth(ops, team.Health{Down: true})
	queued(t, w.say(&ops, "one", "repo.2"))
	queued(t, w.say(&ops, "two", "repo.2"))
	rm, err := w.e.Remove(t.Context(), ops)
	check(t, err)
	if rm.UserID != agent(ops, 1) || !strings.Contains(w.log.String(), "2 queued sends") {
		t.Errorf("the removal %+v; its line: %s", rm, w.log.String())
	}
}
