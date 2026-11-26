package teamsync

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
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

// halt posts coord's halt to me, a ping with no reference: an item every answer that holds it
// persists, so a test of a held ping always has a batch to read.
func (w *world) halt() {
	w.t.Helper()
	out, err := w.composer(coord).Ping(proto.Ping{Verb: proto.VerbHalt, To: []string{user(me)}}, "", "")
	w.post(coord, out, err)
}

// heldIn is whether the batch b took nothing of the held ping id: no item, no drop, no bounce,
// and main's floor, when it moves, still below it.
func (w *world) heldIn(b team.Batch, id string) bool {
	w.t.Helper()
	if slices.Contains(itemIDs(b), id) || b.Counts.Dropped != 0 || len(b.Bounces) != 0 {
		return false
	}
	f, moved := b.Floors[proto.PurposeMain]
	return !moved || !f.Covers(w.ts(id), id)
}

// Plan 00267 Task 3.2 (DESIGN A row 42): a transient forge failure is held, not dropped, and
// checked again; once the forge proves it, it is delivered and the floor passes it.
func TestATransientForgeFailureIsHeldUntilItProves(t *testing.T) {
	w := newWorld(t)
	f := newFakeForge(map[string]forgeAnswer{ref(1): {outcome: proto.ReasonForgeRate}})
	r, release := w.held(w.newTeam(withForge(f)))
	id := w.done(peer, ref(1))
	w.halt()
	release()
	b := w.batch()
	if !w.heldIn(b, id) || len(b.Items) != 1 {
		t.Fatalf("the failed check: batch %+v; log %q", b, w.log.String())
	}
	r.settled(t, b)
	if n := w.log.count("held " + id + ": its forge check gave forge-rate; it is checked again until "); n != 1 {
		t.Errorf("the held line said %d times; log %q", n, w.log.String())
	}
	f.answerAll(forgeAnswer{})
	w.clk.StepWall(forgeRetryEvery.Milliseconds())
	w.halt()
	b = w.batch()
	if got := itemIDs(b); len(got) != 2 || got[0] != id || b.Counts.Dropped != 0 {
		t.Errorf("rechecked: items %v, counts %+v; log %q", got, b.Counts, w.log.String())
	}
	if fl := b.Floors[proto.PurposeMain]; !fl.Covers(w.ts(id), id) {
		t.Errorf("the floor %+v does not pass the delivered ping", fl)
	}
}

// Plan 00267 Task 3.2: a failure still there at forgeRetryLimit is dropped with its reason and
// bounced to its sender; before then a recheck that fails again holds it still.
func TestATransientForgeFailureIsBouncedAtItsDeadline(t *testing.T) {
	w := newWorld(t)
	f := newFakeForge(map[string]forgeAnswer{ref(1): {outcome: proto.ReasonUnresolved}})
	r, release := w.held(w.newTeam(withForge(f)))
	id := w.done(peer, ref(1))
	w.halt()
	release()
	if b := w.batch(); !w.heldIn(b, id) {
		t.Fatalf("the failed check: batch %+v", b)
	}
	r.poll(t)
	w.clk.StepWall(forgeRetryEvery.Milliseconds())
	w.halt()
	if b := w.batch(); !w.heldIn(b, id) {
		t.Fatalf("a recheck before the deadline: batch %+v", b)
	}
	r.poll(t)
	w.clk.StepWall((forgeRetryLimit - forgeRetryEvery).Milliseconds())
	w.halt()
	b := w.batch()
	r.settled(t, b)
	if slices.Contains(itemIDs(b), id) || b.Counts.DroppedBy[proto.ReasonUnresolved] != 1 || w.log.count("dropped "+id+": unresolved") != 1 {
		t.Errorf("at the deadline: batch %+v; log %q", b, w.log.String())
	}
	if len(b.Bounces) != 1 || b.Bounces[0].ID != id || b.Bounces[0].Reason != proto.ReasonUnresolved {
		t.Errorf("bounces %+v", b.Bounces)
	}
	if n := len(f.checked()); n != 3 {
		t.Errorf("checked %d times, want 3", n)
	}
}

// Plan 00267 Task 3.2: provenance is no transient failure: it is dropped and bounced at once.
func TestAProvenanceFailureIsBouncedAtOnce(t *testing.T) {
	w := newWorld(t)
	f := newFakeForge(map[string]forgeAnswer{ref(1): {outcome: proto.ReasonProvenance}})
	r, release := w.held(w.newTeam(withForge(f)))
	id := w.done(peer, ref(1))
	release()
	b := w.batch()
	r.settled(t, b)
	if b.Counts.DroppedBy[proto.ReasonProvenance] != 1 || len(b.Bounces) != 1 || b.Bounces[0].ID != id {
		t.Errorf("batch %+v", b)
	}
	if w.log.count("held ") != 0 {
		t.Errorf("held a provenance failure; log %q", w.log.String())
	}
}

// Plan 00267 Task 3.2: past forgeHeldCap held pings a transient failure is dropped and bounced
// at once, never lost.
func TestPastTheHeldCapAFailureIsDroppedAtOnce(t *testing.T) {
	transient := proto.Verdict{Outcome: proto.Drop, Reason: proto.ReasonForgeRate}
	if !holds(nil, "$a", transient) {
		t.Error("a transient failure with room to hold it is not held")
	}
	if holds(nil, "$a", proto.Verdict{Outcome: proto.Drop, Reason: proto.ReasonProvenance}) {
		t.Error("provenance is held")
	}
	if holds(nil, "", transient) {
		t.Error("an event with no id is held")
	}
	full := make([]forgeHeld, forgeHeldCap)
	for i := range full {
		full[i].id = fmt.Sprintf("$%d", i)
	}
	if holds(full, "$new", transient) {
		t.Error("held past the cap")
	}
	if !holds(full[:forgeHeldCap-1], "$new", transient) {
		t.Error("not held below the cap")
	}
}

// Plan 00267 Task 3.2: the floor is kept just below the earliest held ping it would cover.
func TestTheFloorStaysBelowAHeldPing(t *testing.T) {
	high := state.Floor{MS: 500, IDs: []string{"$c"}}
	held := []forgeHeld{
		{id: "$b", v: proto.Verdict{Event: proto.Event{OriginTs: 300}}},
		{id: "$a", v: proto.Verdict{Event: proto.Event{OriginTs: 200}}},
		{id: "$z", v: proto.Verdict{Event: proto.Event{OriginTs: 900}}},
	}
	if got := heldFloor(high, held); !got.Equal(state.Before(200)) {
		t.Errorf("heldFloor %+v, want %+v", got, state.Before(200))
	}
	if got := heldFloor(high, nil); !got.Equal(high) {
		t.Errorf("with nothing held %+v, want %+v", got, high)
	}
}

// SD5-r1: provenance is dropped with that reason; unresolved and forge-rate, an error and an
// outcome the forge has no name for (unresolved) are held, each with one line. A proven
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
	if b.Counts.Dropped != 1 || w.log.count("dropped "+ids[0]+": "+proto.ReasonProvenance) != 1 {
		t.Errorf("counts %+v; log %q", b.Counts, w.log.String())
	}
	for i, reason := range []string{proto.ReasonUnresolved, proto.ReasonForgeRate, proto.ReasonUnresolved, proto.ReasonUnresolved} {
		if n := w.log.count("held " + ids[i+1] + ": its forge check gave " + reason + ";"); n != 1 {
			t.Errorf("%s: said %d times; log %q", reason, n, w.log.String())
		}
	}
	if len(f.checked()) != 6 {
		t.Errorf("checked %v", f.checked())
	}
}

// SD5-r2 (n3d-D10): no gh login holds the ping unresolved and says forge-login once, until a
// check next succeeds.
func TestSD5r2NoLoginIsSaidOnce(t *testing.T) {
	w := newWorld(t)
	f := newFakeForge(nil)
	f.answerAll(forgeAnswer{err: team.ErrForgeLogin})
	r := w.start(withForge(f))
	w.batch()
	r.poll(t)
	// held waits until the answer holding the n-th ping is settled: a held ping alone may move
	// nothing the store keeps, so there need be no batch to wait for.
	held := func(n int) {
		id := w.done(peer, ref(n))
		for w.log.count("held "+id+": its forge check gave unresolved;") == 0 {
			r.poll(t)
		}
	}
	for n := range 2 {
		held(n)
	}
	if n := w.log.count("forge-login"); n != 1 || w.log.count("dropped ") != 0 {
		t.Errorf("forge-login said %d times; log %q", n, w.log.String())
	}
	f.answerAll(forgeAnswer{})
	id := w.done(peer, ref(2))
	b := w.batch()
	for !slices.Contains(itemIDs(b), id) {
		b = w.batch()
	}
	r.settled(t, b)
	f.answerAll(forgeAnswer{err: team.ErrForgeLogin})
	held(3)
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
// persisted once 30 s of fake time pass, all held unresolved; at most 8 checks ran at once.
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
	w.halt()
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
	if len(b.Items) != 1 || b.Counts.Dropped != 0 || w.log.count("gave unresolved;") != 10 {
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

// Plan 00267 Task 3.2: Refetch judges an item pending already took, read again, so staleness
// never drops it: past its age limit it is delivered marked late.
func TestARefetchedItemPastItsAgeIsDeliveredLate(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam(withForge(newFakeForge(nil)))
	id := w.done(peer, ref(1))
	w.clk.StepWall((w.roster.Limits.HumanMaxAgeS + 1) * 1000)
	v, err := tm.Refetch(t.Context(), proto.PurposeMain, id)
	check(t, err)
	if v.Outcome != proto.Deliver || !v.Late {
		t.Errorf("refetched past its age: %+v", v)
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
// receive window; past it the ping is held forge-rate (n3d-D34) and no request is made. Another
// sender keeps its own budget, and the window restores it, when the held two are delivered.
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
	if len(b.Items) != over-1 || b.Counts.Dropped != 0 || w.log.count("gave forge-rate;") != 2 {
		t.Errorf("items %d, dropped %d; log %q", len(b.Items), b.Counts.Dropped, w.log.String())
	}
	if n := len(f.checked()); n != over-1 {
		t.Errorf("%d checks asked, want %d", n, over-1)
	}
	w.clk.Advance(time.Duration(proto.ReceiveWindowMs) * time.Millisecond)
	w.done(peer, ref(over+1))
	if b := w.batch(); len(b.Items) != 3 {
		t.Errorf("after the window: items %d, dropped %d; log %q", len(b.Items), b.Counts.Dropped, w.log.String())
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
// no request, and held.
func TestSD5r11AGroupWithNoBudgetIsForgeRate(t *testing.T) {
	w := newWorld(t)
	f := newFakeForge(nil)
	r, release := w.held(w.spentTeam(peer, f))
	w.done(peer, ref(1))
	w.done(peer, ref(1))
	w.halt()
	release()
	b := w.batch()
	r.poll(t)
	if len(b.Items) != 1 || b.Counts.Dropped != 0 || w.log.count("gave forge-rate;") != 2 {
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
		w.halt()
		w.batch()
		r.poll(t)
	}
	if n := w.log.count("gave forge-rate;"); n != 1 {
		t.Errorf("the budget's refusal said %d times; log %q", n, w.log.String())
	}
	if n := w.log.count("forge-login"); n != 1 {
		t.Errorf("forge-login said %d times; log %q", n, w.log.String())
	}
}
