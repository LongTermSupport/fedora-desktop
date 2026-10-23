package teamsync

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// forgeAnswer is the fake forge's answer for one reference: an outcome, else an error; the zero
// answer is proven.
type forgeAnswer struct {
	outcome string
	err     error
}

// fakeForge answers each reference as scripted, or hangs until its check's context ends; it
// records the references checked and the most checks running at once.
type fakeForge struct {
	mu      sync.Mutex
	script  map[string]forgeAnswer
	all     *forgeAnswer
	hang    bool
	calls   []string
	running int
	most    int
	started chan struct{}
	// bounds are the bound each check's context carried; refused counts the checks whose
	// sender budget said no, which, as the checker's, make no request and are forge-rate.
	bounds  []time.Duration
	refused int
}

func newFakeForge(script map[string]forgeAnswer) *fakeForge {
	return &fakeForge{script: script, started: make(chan struct{}, 64)}
}

func (f *fakeForge) Check(ctx context.Context, need proto.ForgeCheck, _ proto.Forge) (team.ForgeResult, error) {
	if spend, ok := team.ReceiveSpend(ctx); ok && !spend() {
		f.mu.Lock()
		f.refused++
		f.mu.Unlock()
		return team.ForgeResult{Outcome: proto.ReasonForgeRate}, nil
	}
	f.mu.Lock()
	f.calls = append(f.calls, need.Ref.Text)
	if at, ok := clock.Bound(ctx); ok {
		f.bounds = append(f.bounds, at)
	}
	f.running++
	f.most = max(f.most, f.running)
	a, hang := f.script[need.Ref.Text], f.hang
	if f.all != nil {
		a = *f.all
	}
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.running--
		f.mu.Unlock()
	}()
	f.started <- struct{}{}
	switch {
	case hang:
		<-ctx.Done()
		return team.ForgeResult{}, team.ErrForgeDown
	case a.err != nil:
		return team.ForgeResult{}, a.err
	case a.outcome == "":
		return team.ForgeResult{Outcome: team.ForgeProven}, nil
	}
	return team.ForgeResult{Outcome: a.outcome}, nil
}

func (f *fakeForge) answerAll(a forgeAnswer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.all = &a
}

func (f *fakeForge) checked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeForge) mostAtOnce() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.most
}

func withForge(f team.Forge) option { return func(c *Config) { c.Forge = f } }

// done posts local's done ping to me with a reference, which the receiver's forge checks.
func (w *world) done(local, reference string) string {
	w.t.Helper()
	out, err := w.composer(local).Ping(proto.Ping{Verb: proto.VerbDone, To: []string{user(me)}, Ref: reference}, "", "")
	return w.post(local, out, err)
}

// SD5-r1: provenance, unresolved and forge-rate are dropped with that reason, as are an error
// and an outcome the forge has no name for (unresolved); each counted with one line. A proven
// reference is queued.
func TestSD5r1FailedChecksAreDropped(t *testing.T) {
	w := newWorld(t)
	f := newFakeForge(map[string]forgeAnswer{
		ref(1): {outcome: proto.ReasonProvenance},
		ref(2): {outcome: proto.ReasonUnresolved},
		ref(3): {outcome: proto.ReasonForgeRate},
		ref(4): {err: team.ErrForgeDown},
		ref(5): {outcome: "something new"},
	})
	r, release := w.held(w.newTeam(withForge(f)))
	ids := make([]string, 0, 6)
	for i := range 6 {
		ids = append(ids, w.done(peer, ref(i+1)))
	}
	release()
	b := w.batch()
	r.poll(t)
	if got := itemIDs(b); len(got) != 1 || got[0] != ids[5] {
		t.Errorf("items %v", got)
	}
	if b.Counts.Dropped != 5 {
		t.Errorf("counts %+v", b.Counts)
	}
	for i, reason := range []string{proto.ReasonProvenance, proto.ReasonUnresolved, proto.ReasonForgeRate, proto.ReasonUnresolved, proto.ReasonUnresolved} {
		if n := w.log.count("dropped " + ids[i] + ": " + reason); n != 1 {
			t.Errorf("%s: said %d times; log %q", reason, n, w.log.String())
		}
	}
	if len(f.checked()) != 6 {
		t.Errorf("checked %v", f.checked())
	}
}

// SD5-r2 (n3d-D10): no gh login drops unresolved and says forge-login once, until a check next
// succeeds.
func TestSD5r2NoLoginIsSaidOnce(t *testing.T) {
	w := newWorld(t)
	f := newFakeForge(nil)
	f.answerAll(forgeAnswer{err: team.ErrForgeLogin})
	r := w.start(withForge(f))
	w.batch()
	r.poll(t)
	next := func(n int) team.Batch {
		w.done(peer, ref(n))
		b := w.batch()
		r.poll(t)
		return b
	}
	for n := range 2 {
		if b := next(n); b.Counts.Dropped != 1 || len(b.Items) != 0 {
			t.Errorf("batch %+v", b)
		}
	}
	if n := w.log.count("forge-login"); n != 1 {
		t.Errorf("forge-login said %d times; log %q", n, w.log.String())
	}
	f.answerAll(forgeAnswer{})
	if b := next(2); len(b.Items) != 1 {
		t.Errorf("a proven ping: %+v", b)
	}
	f.answerAll(forgeAnswer{err: team.ErrForgeLogin})
	next(3)
	if n := w.log.count("forge-login"); n != 2 {
		t.Errorf("forge-login said %d times after a success; log %q", n, w.log.String())
	}
}

// SD5-r3: a carried ping and a ping without a reference are never checked.
func TestSD5r3UncheckedPings(t *testing.T) {
	w := newWorld(t)
	f := newFakeForge(nil)
	_, release := w.held(w.newTeam(withForge(f)))
	admin := proto.Composer{Server: server, Self: proto.AdminID(server), Roster: w.roster}
	carried := proto.Carried{
		Kind: proto.CarriedPing, Verb: "review", Ref: ref(1), From: user(peer), SentMs: 1,
		V1Event: "$" + "av1eventfromthelegacybusaaaaaaaaaaaaaaaaaaa", To: user(me), Text: "please review",
	}
	out, err := admin.Carried(carried, "carried from v1")
	check(t, err)
	_, err = w.admin().Send(t.Context(), w.main, proto.MessageType, "", out.Content.Compact())
	check(t, err)
	halt, err := w.composer(coord).Ping(proto.Ping{Verb: proto.VerbHalt, To: []string{user(me)}}, "", "")
	w.post(coord, halt, err)
	release()
	if b := w.batch(); len(b.Items) != 2 {
		t.Errorf("items %+v, dropped %d; log %q", b.Items, b.Counts.Dropped, w.log.String())
	}
	if calls := f.checked(); len(calls) != 0 {
		t.Errorf("checked %v", calls)
	}
}

// SD5-r4 / SD5-r6 (n3d-D29): ten referenced pings against a forge that never answers are
// persisted once 30 s of fake time pass, all dropped unresolved; at most 8 checks ran at once.
func TestSD5r4AHungForgeCostsOneBudget(t *testing.T) {
	w := newWorld(t)
	f := newFakeForge(nil)
	f.hang = true
	r, release := w.held(w.newTeam(withForge(f)))
	for i := range 10 {
		from := peer
		if i%2 == 1 {
			from = coord
		}
		w.done(from, ref(i))
	}
	release()
	for range forgeParallel {
		select {
		case <-f.started:
		case <-t.Context().Done():
			t.Fatal("the checks did not start")
		}
	}
	w.noBatch()
	w.clk.Advance(forgeBudget)
	b := w.batch()
	r.poll(t)
	if len(b.Items) != 0 || b.Counts.Dropped != 10 || w.log.count(": unresolved") != 10 {
		t.Errorf("batch %+v; log %q", b, w.log.String())
	}
	if n := f.mostAtOnce(); n != forgeParallel {
		t.Errorf("%d checks at once, want %d", n, forgeParallel)
	}
}

// SD5-r5 (n3d-D12): Refetch of a restored ping runs the check and returns a drop when it fails;
// with no forge every referenced ping is dropped unresolved.
func TestSD5r5RefetchRunsTheCheck(t *testing.T) {
	w := newWorld(t)
	f := newFakeForge(map[string]forgeAnswer{ref(1): {outcome: proto.ReasonProvenance}})
	tm := w.newTeam(withForge(f))
	good, bad := w.done(peer, ref(2)), w.done(peer, ref(1))
	v, err := tm.Refetch(t.Context(), proto.PurposeMain, good)
	check(t, err)
	if v.Outcome != proto.Deliver || v.Forge == nil {
		t.Errorf("proven: %+v", v)
	}
	v, err = tm.Refetch(t.Context(), proto.PurposeMain, bad)
	check(t, err)
	if v.Outcome != proto.Drop || v.Reason != proto.ReasonProvenance {
		t.Errorf("failed: %+v", v)
	}
	v, err = w.newTeam().Refetch(t.Context(), proto.PurposeMain, good)
	check(t, err)
	if v.Outcome != proto.Drop || v.Reason != proto.ReasonUnresolved {
		t.Errorf("no forge: %+v", v)
	}
}

// A check cut short by the team's own end drops nothing: the error is the context's.
func TestAnEndedCheckDropsNothing(t *testing.T) {
	w := newWorld(t)
	f := newFakeForge(nil)
	f.hang = true
	tm := w.newTeam(withForge(f))
	id := w.done(peer, ref(1))
	ctx, cancel := context.WithCancel(t.Context())
	cancelled := make(chan struct{})
	go func() {
		defer close(cancelled)
		select {
		case <-f.started:
		case <-ctx.Done():
		}
		cancel()
	}()
	if _, err := tm.Refetch(ctx, proto.PurposeMain, id); !errors.Is(err, context.Canceled) {
		t.Errorf("Refetch: %v", err)
	}
	cancel()
	<-cancelled
}

// SD5-r7 (n3d-D31): each receive check carries the batch's budget as its bound, so a rate wait
// past it is forge-rate at once.
func TestSD5r7EachCheckIsBoundByTheBudget(t *testing.T) {
	w := newWorld(t)
	f := newFakeForge(nil)
	r, release := w.held(w.newTeam(withForge(f)))
	w.done(peer, ref(1))
	want := w.clk.Boot() + forgeBudget
	release()
	w.batch()
	r.poll(t)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bounds) != 1 || f.bounds[0] != want {
		t.Errorf("bounds %v, want [%v]", f.bounds, want)
	}
}

// SD5-r8 (n3d-D31): a sender spends at most proto.ForgeChecksPerSender uncached checks per
// receive window; past it the ping drops forge-rate (n3d-D34) and no request is made. Another
// sender keeps its own budget, and the window restores it.
func TestSD5r8ASendersChecksAreBudgeted(t *testing.T) {
	w := newWorld(t)
	f := newFakeForge(nil)
	r, release := w.held(w.newTeam(withForge(f)))
	over := int(proto.ForgeChecksPerSender) + 2
	for i := range over {
		w.done(peer, ref(i))
	}
	w.done(coord, ref(over))
	release()
	b := w.batch()
	r.poll(t)
	if len(b.Items) != over-1 || b.Counts.Dropped != 2 || w.log.count(": forge-rate") != 2 {
		t.Errorf("items %d, dropped %d; log %q", len(b.Items), b.Counts.Dropped, w.log.String())
	}
	if n := len(f.checked()); n != over-1 {
		t.Errorf("%d checks asked, want %d", n, over-1)
	}
	w.clk.Advance(time.Duration(proto.ReceiveWindowMs) * time.Millisecond)
	w.done(peer, ref(over+1))
	if b := w.batch(); len(b.Items) != 1 {
		t.Errorf("after the window: items %d, dropped %d", len(b.Items), b.Counts.Dropped)
	}
}

// SD5-r9 (n3d-D31): identical checks in one batch are asked once, and the answer is each one's.
func TestSD5r9IdenticalChecksAreAskedOnce(t *testing.T) {
	w := newWorld(t)
	f := newFakeForge(map[string]forgeAnswer{ref(1): {outcome: proto.ReasonProvenance}})
	r, release := w.held(w.newTeam(withForge(f)))
	for range 3 {
		w.done(peer, ref(1))
	}
	w.done(coord, ref(1))
	release()
	b := w.batch()
	r.poll(t)
	if len(b.Items) != 0 || w.log.count(": provenance") != 4 {
		t.Errorf("items %d; log %q", len(b.Items), w.log.String())
	}
	if calls := f.checked(); len(calls) != 1 {
		t.Errorf("checked %v, want one", calls)
	}
}

// spentTeam is a team whose sender local has spent its whole receive budget.
func (w *world) spentTeam(local string, f team.Forge) *Team {
	w.t.Helper()
	tm := w.newTeam(withForge(f))
	for range int(proto.ForgeChecksPerSender) {
		if !tm.spent.spend(user(local), w.clk.WallMS()) {
			w.t.Fatal("the budget was spent early")
		}
	}
	return tm
}

// SD5-r10 (n3d-D34): a merged check is charged to a sender in its group with budget left, so a
// sender past its budget never drops another's ping; every sender in the group gets the answer.
func TestSD5r10AMergedCheckIsChargedToASenderWithBudget(t *testing.T) {
	w := newWorld(t)
	f := newFakeForge(nil)
	r, release := w.held(w.spentTeam(peer, f))
	w.done(peer, ref(1))
	w.done(coord, ref(1))
	release()
	b := w.batch()
	r.poll(t)
	if len(b.Items) != 2 || b.Counts.Dropped != 0 {
		t.Errorf("items %d, dropped %d; log %q", len(b.Items), b.Counts.Dropped, w.log.String())
	}
	if calls := f.checked(); len(calls) != 1 {
		t.Errorf("checked %v, want one", calls)
	}
}

// SD5-r11 (n3d-D34): a group none of whose senders has budget left is answered forge-rate, with
// no request.
func TestSD5r11AGroupWithNoBudgetIsForgeRate(t *testing.T) {
	w := newWorld(t)
	f := newFakeForge(nil)
	r, release := w.held(w.spentTeam(peer, f))
	w.done(peer, ref(1))
	w.done(peer, ref(1))
	release()
	b := w.batch()
	r.poll(t)
	if len(b.Items) != 0 || b.Counts.Dropped != 2 || w.log.count(": forge-rate") != 2 {
		t.Errorf("items %d, dropped %d; log %q", len(b.Items), b.Counts.Dropped, w.log.String())
	}
	if calls := f.checked(); len(calls) != 0 {
		t.Errorf("checked %v, want none", calls)
	}
}

// SD5-r12 (n3d-D10): a check that made no request (refused by the budget) is not an answer, so
// it does not make forge-login said again.
func TestSD5r12ACheckWithNoRequestIsNotAnAnswer(t *testing.T) {
	w := newWorld(t)
	f := newFakeForge(nil)
	f.answerAll(forgeAnswer{err: team.ErrForgeLogin})
	r := w.startTeam(w.spentTeam(peer, f))
	w.batch()
	r.poll(t)
	for i, from := range []string{coord, peer, coord} {
		w.done(from, ref(i))
		w.batch()
		r.poll(t)
	}
	if n := w.log.count(": forge-rate"); n != 1 {
		t.Errorf("the budget's refusal said %d times; log %q", n, w.log.String())
	}
	if n := w.log.count("forge-login"); n != 1 {
		t.Errorf("forge-login said %d times; log %q", n, w.log.String())
	}
}
