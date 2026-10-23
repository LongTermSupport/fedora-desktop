package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/failpoint"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/leakcheck"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/inbox"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

const (
	testSocket = "/run/u/cc-socks/4242.sock"
	testToken  = "tok-0123456789"
)

// dialStep is one connection's script: the dial's error, then the user line's and Finish's.
type dialStep struct {
	dial, write, finish error
}

// dialEvent is what one connection did: a dial that failed (err set, no text), or a user line
// written (text, with the write's outcome).
type dialEvent struct {
	target inbox.Target
	text   string
	err    error
}

// fakeDialer is the inbox socket as the engine dials it, scripted per connection; a connection
// with no step left succeeds.
type fakeDialer struct {
	mu     sync.Mutex
	steps  []dialStep
	dials  int
	writes int
	events chan dialEvent
}

func newFakeDialer() *fakeDialer { return &fakeDialer{events: make(chan dialEvent, 256)} }

func (d *fakeDialer) Dial(_ context.Context, t inbox.Target) (inbox.Conn, error) {
	d.mu.Lock()
	var step dialStep
	if len(d.steps) > 0 {
		step, d.steps = d.steps[0], d.steps[1:]
	}
	d.dials++
	d.mu.Unlock()
	if step.dial != nil {
		d.events <- dialEvent{target: t, err: step.dial}
		return nil, step.dial
	}
	return &fakeConn{d: d, t: t, step: step}, nil
}

func (d *fakeDialer) then(steps ...dialStep) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.steps = append(d.steps, steps...)
}

func (d *fakeDialer) dialCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.dials
}

func (d *fakeDialer) writeCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.writes
}

type fakeConn struct {
	d    *fakeDialer
	t    inbox.Target
	step dialStep
}

func (c *fakeConn) WriteUser(envelope string) error {
	c.d.mu.Lock()
	c.d.writes++
	c.d.mu.Unlock()
	c.d.events <- dialEvent{target: c.t, text: envelope, err: c.step.write}
	return c.step.write
}

func (c *fakeConn) Finish() error { return c.step.finish }
func (*fakeConn) Close() error    { return nil }

// dworld is a world whose engine delivers to a fake inbox socket.
type dworld struct {
	*world

	dial       *fakeDialer
	transcript string

	hmu      sync.Mutex
	hits     []failpoint.Name
	onHit    func(failpoint.Name)
	unusable []state.Unusable
}

func newDeliveryWorld(t *testing.T, delivery string) *dworld {
	t.Helper()
	leakcheck.Check(t)
	return buildDeliveryWorld(t, delivery)
}

// another is a second delivery world of dw's test, beside dw (a restarted link): the test's
// leak check is dw's.
func (dw *dworld) another(delivery string) *dworld {
	dw.t.Helper()
	return buildDeliveryWorld(dw.t, delivery)
}

func buildDeliveryWorld(t *testing.T, delivery string) *dworld {
	t.Helper()
	dw := &dworld{dial: newFakeDialer()}
	dw.world = buildWorld(t, delivery, func(c *Config) {
		c.Inbox = dw.dial
		c.OnUnusable = dw.recordUnusable
	}, func(e *Engine) { e.hit = dw.hit })
	dw.transcript = filepath.Join(t.TempDir(), "session.jsonl")
	check(t, os.WriteFile(dw.transcript, []byte("{\"type\":\"user\"}\n"), 0o600))
	return dw
}

func (dw *dworld) hit(p point) {
	n := p.name()
	dw.hmu.Lock()
	dw.hits = append(dw.hits, n)
	f := dw.onHit
	dw.hmu.Unlock()
	if f != nil {
		f(n)
	}
}

func (dw *dworld) setOnHit(f func(failpoint.Name)) {
	dw.hmu.Lock()
	defer dw.hmu.Unlock()
	dw.onHit = f
}

func (dw *dworld) hitCount(name failpoint.Name) int {
	dw.hmu.Lock()
	defer dw.hmu.Unlock()
	n := 0
	for _, h := range dw.hits {
		if h == name {
			n++
		}
	}
	return n
}

func (dw *dworld) recordUnusable(u state.Unusable) {
	dw.hmu.Lock()
	defer dw.hmu.Unlock()
	dw.unusable = append(dw.unusable, u)
}

func (dw *dworld) unusableSeen() []state.Unusable {
	dw.hmu.Lock()
	defer dw.hmu.Unlock()
	return slices.Clone(dw.unusable)
}

// facts gives the engine the socket, the token and the transcript.
func (dw *dworld) facts(idle bool) {
	dw.t.Helper()
	check(dw.t, dw.e.SetSession(dw.t.Context(), SessionFacts{Socket: testSocket, Token: testToken, Transcript: dw.transcript, Idle: idle}))
}

// roster is id's verified roster: this seat a worker, seat 2 the coordinator, alice the human.
func roster(id paths.TeamID) proto.Roster {
	return proto.Roster{
		Team: id.String(), Name: id.String(), Humans: []string{alice(id)},
		Members: map[string]proto.RosterMember{
			agent(id, 1): {Role: proto.RoleWorker, HumanText: true},
			agent(id, 2): {Role: proto.RoleCoordinator, HumanText: true},
		},
	}
}

// live is id held, its roster verified.
func (dw *dworld) live(id paths.TeamID) {
	dw.t.Helper()
	dw.held(id).notices.set(roster(id))
}

// next is the next connection's event.
func (dw *dworld) next() dialEvent {
	dw.t.Helper()
	select {
	case ev := <-dw.dial.events:
		return ev
	case <-dw.t.Context().Done():
		dw.t.Fatal("no connection was made")
	}
	return dialEvent{}
}

// advance waits until the loop's timer due d from now is armed, then moves the clock by d.
func (dw *dworld) advance(d time.Duration) {
	dw.t.Helper()
	dw.short(d, d)
}

// short waits until the loop's timer due from now is armed, then moves the clock by d, which
// falls short of it.
func (dw *dworld) short(due, d time.Duration) {
	dw.t.Helper()
	dw.awaitDue(due)
	dw.clk.Advance(d)
}

// idleAfter waits for the delivery loop to go idle more than n times.
func (dw *dworld) idleAfter(n int64) {
	dw.t.Helper()
	for dw.e.idles.Load() <= n {
		if dw.t.Context().Err() != nil {
			dw.t.Fatal("the loop never went idle")
		}
		runtime.Gosched()
	}
}

func peerItem(id paths.TeamID, n int, sent int64, text string) team.Item {
	return team.Item{
		Pending: state.Pending{ID: evID(n), Room: proto.PurposeMain, Item: records.KindPeer, SentMS: sent, From: agent(id, 2)},
		Verdict: proto.Verdict{
			Outcome: proto.Deliver, Event: proto.Event{ID: evID(n), OriginTs: sent},
			Content: proto.Content{Text: proto.Text{To: []string{agent(id, 1)}, Text: text}},
		},
	}
}

func nonceOf(t *testing.T, text string) string {
	t.Helper()
	rest, ok := strings.CutPrefix(text, "teamchat delivery ")
	if !ok || len(rest) < 9 || rest[8] != ':' {
		t.Fatalf("not a delivery: %q", text)
	}
	return rest[:8]
}

type membershipAnswer struct {
	ok  bool
	err error
}

func (dw *dworld) sendMembership(m proto.Membership) <-chan membershipAnswer {
	got := make(chan membershipAnswer, 1)
	go func() {
		ok, err := dw.e.SendMembership(dw.t.Context(), m)
		got <- membershipAnswer{ok, err}
	}()
	return got
}

var workerNotice = proto.Membership{Roles: []proto.MembershipTeam{{Team: "alpha", Role: proto.RoleWorker}}, Teams: []string{"alpha"}}

// The session's facts (DV1): delivery is live only in socket mode, with the socket, the token
// and the transcript all set, and not since gone.
func TestLiveNeedsEveryFact(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	if dw.e.Live() {
		t.Fatal("live before any fact")
	}
	ctx := t.Context()
	check(t, dw.e.SetSession(ctx, SessionFacts{Socket: testSocket, Token: testToken}))
	if dw.e.Live() {
		t.Fatal("live with no transcript")
	}
	check(t, dw.e.SetSession(ctx, SessionFacts{Transcript: dw.transcript}))
	if !dw.e.Live() {
		t.Fatal("not live with every fact set (an empty fact keeps the one set before)")
	}
	var idle bool
	check(t, dw.e.SetSession(ctx, SessionFacts{Idle: true}))
	dw.local(func() { idle = dw.e.sess.idle })
	if !idle {
		t.Fatal("idle not taken")
	}
	wait := dw.another(state.DeliveryWait)
	wait.facts(false)
	if wait.e.Live() {
		t.Fatal("a wait session is live")
	}
	if ok, err := wait.e.SendMembership(ctx, workerNotice); ok || err != nil {
		t.Fatalf("membership to a wait session = %v, %v", ok, err)
	}
	stopped, err := New(dw.config(state.DeliverySocket))
	check(t, err)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	check(t, stopped.Run(cancelled))
	if stopped.Live() {
		t.Fatal("an ended engine is live")
	}
}

// DV1: nothing is delivered before a hook gave both the socket and the transcript; then the
// pending item goes at once.
func TestNoDeliveryBeforeTheFacts(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	a := tid(t, "alpha")
	dw.live(a)
	dw.persist(a, humanItem(a, 1, startWall, "hello"))
	// Two pokes made while the loop works are one, so only one more idle is certain.
	dw.idleAfter(0)
	idles := dw.e.idles.Load()
	check(t, dw.e.SetSession(t.Context(), SessionFacts{Socket: testSocket, Token: testToken}))
	dw.idleAfter(idles)
	if n := dw.dial.dialCount(); n != 0 {
		t.Fatalf("%d dials before the transcript", n)
	}
	check(t, dw.e.SetSession(t.Context(), SessionFacts{Transcript: dw.transcript}))
	ev := dw.next()
	if ev.target.Socket != testSocket || ev.target.Token != testToken || !strings.Contains(ev.text, evID(1)) {
		t.Fatalf("delivered %+v", ev)
	}
	if !strings.Contains(ev.text, "1 message on alpha (1 from humans") {
		t.Fatalf("the delivery's first line: %q", ev.text)
	}
}

// DV1: a launch with inbound refuse (delivery wait) never dials, whatever the facts.
func TestNeverToAWaitSession(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliveryWait)
	a := tid(t, "alpha")
	dw.live(a)
	dw.facts(true)
	dw.persist(a, humanItem(a, 1, startWall, "hello"))
	dw.local(func() {})
	if n := dw.dial.dialCount(); n != 0 {
		t.Fatalf("%d dials to a wait session", n)
	}
	if dw.e.idles.Load() != 0 {
		t.Fatal("a wait session runs the delivery loop")
	}
}

// DV1: a socket that is gone stops delivery until a hook tells a socket again.
func TestAGoneSocketStopsDelivery(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	a := tid(t, "alpha")
	dw.live(a)
	dw.dial.then(dialStep{dial: fmt.Errorf("%w: connect: no such file", inbox.ErrGone)})
	dw.facts(false)
	dw.persist(a, humanItem(a, 1, startWall, "one"))
	if ev := dw.next(); !errors.Is(ev.err, inbox.ErrGone) {
		t.Fatalf("first connection %+v", ev)
	}
	dw.until(func() bool { return dw.e.sess.gone })
	if dw.e.Live() {
		t.Fatal("live after the socket went")
	}
	idles := dw.e.idles.Load()
	dw.persist(a, humanItem(a, 2, startWall+1, "two"))
	dw.idleAfter(idles)
	if n := dw.dial.dialCount(); n != 1 {
		t.Fatalf("%d dials to a gone socket", n)
	}
	if !strings.Contains(dw.log.String(), "gone") {
		t.Fatalf("link.log: %q", dw.log.String())
	}
	dw.facts(false)
	dw.advance(paceWait)
	ev := dw.next()
	if ev.err != nil || !strings.Contains(ev.text, evID(1)) || !strings.Contains(ev.text, evID(2)) {
		t.Fatalf("after a new socket fact: %+v", ev)
	}
}

// what a failpoint saw: the store on disk and the user lines written so far.
type seen struct {
	store  state.Store
	writes int
	err    error
}

// DV3 and the failpoints of PT1: nothing is persisted at auth; the awaiting entry, written
// unknown with the transcript's size, is persisted before the user line; written yes and the
// ledger after it. The items stay in pending, handed to the delivery's nonce, until proof.
func TestTheAwaitingEntryIsPersistedBeforeTheUserLine(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	a := tid(t, "alpha")
	dw.live(a)
	dw.persist(a, humanItem(a, 1, startWall, "hello"))
	size := int64(len("{\"type\":\"user\"}\n"))
	var (
		mu  sync.Mutex
		got = map[failpoint.Name]seen{}
	)
	dw.setOnHit(func(n failpoint.Name) {
		f, err := dw.root.Read(dw.layout.Seat(a).Store(), 1<<22)
		var s state.Store
		if err == nil {
			s, err = state.DecodeStore(f.Data)
		}
		writes := dw.dial.writeCount()
		mu.Lock()
		got[n] = seen{s, writes, err}
		mu.Unlock()
	})
	dw.facts(false)
	ev := dw.next()
	nonce := nonceOf(t, ev.text)
	dw.until(func() bool {
		s := dw.e.teams[a].store
		return len(s.Awaiting) == 1 && s.Awaiting[0].Written == state.WrittenYes
	})
	mu.Lock()
	defer mu.Unlock()
	checkPhases(t, got, state.Awaiting{
		Nonce: nonce, IDs: []string{evID(1)}, Transcript: state.TranscriptMark{Path: dw.transcript, Offset: size},
		AtMS: dw.clk.Boot().Milliseconds(), Written: state.WrittenUnknown,
	})
	s := dw.store(a)
	if len(s.Ledger) != 1 || s.Ledger[0].ID != evID(1) || s.Ledger[0].From != alice(a) || s.Ledger[0].Item != records.KindHuman {
		t.Fatalf("the ledger: %+v", s.Ledger)
	}
	if len(s.Pending) != 1 || s.Pending[0].Handed == nil {
		t.Fatalf("pending after the write: %+v", s.Pending)
	}
	if st := dw.status(); st.Teams[0].Pending != 0 {
		t.Fatalf("status.json counts an item awaiting proof as pending: %+v", st.Teams[0])
	}
}

// checkPhases is what each failpoint saw: nothing at auth; the entry want, its item handed to
// the nonce, before the user line; the line written and the entry still unknown after it.
func checkPhases(t *testing.T, got map[failpoint.Name]seen, want state.Awaiting) {
	t.Helper()
	auth, awaiting, line := got[failpoint.LinkAfterAuth], got[failpoint.LinkAfterAwaiting], got[failpoint.LinkAfterUserLine]
	for _, s := range []seen{auth, awaiting, line} {
		check(t, s.err)
	}
	if len(auth.store.Awaiting) != 0 || auth.store.Pending[0].Handed != nil || auth.writes != 0 {
		t.Fatalf("at auth: %+v", auth)
	}
	if len(awaiting.store.Awaiting) != 1 || !sameAwaiting(awaiting.store.Awaiting[0], want) || awaiting.writes != 0 {
		t.Fatalf("after the awaiting entry: %+v, want %+v", awaiting, want)
	}
	if h := awaiting.store.Pending[0].Handed; h == nil || *h != want.Nonce || awaiting.store.Pending[0].Repeat {
		t.Fatalf("the item while awaiting: %+v", awaiting.store.Pending[0])
	}
	if line.writes != 1 || line.store.Awaiting[0].Written != state.WrittenUnknown || len(line.store.Ledger) != 0 {
		t.Fatalf("after the user line: %+v", line)
	}
}

func sameAwaiting(a, b state.Awaiting) bool {
	stop := (a.StopMS == nil) == (b.StopMS == nil) && (a.StopMS == nil || *a.StopMS == *b.StopMS)
	return stop && a.Nonce == b.Nonce && slices.Equal(a.IDs, b.IDs) && a.Transcript == b.Transcript &&
		a.AtMS == b.AtMS && a.Written == b.Written
}

// The quiet clock starts at the write when the session is idle then (n3e-D9: boot ms); a
// transcript not yet created is offset 0 (n3e-D10).
func TestAnIdleSessionsEntryHasItsStop(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	a := tid(t, "alpha")
	dw.live(a)
	check(t, os.Remove(dw.transcript))
	dw.persist(a, humanItem(a, 1, startWall, "hello"))
	dw.facts(true)
	dw.next()
	dw.until(func() bool { return len(dw.e.teams[a].store.Awaiting) == 1 })
	got := dw.store(a).Awaiting[0]
	if got.StopMS == nil || *got.StopMS != dw.clk.Boot().Milliseconds() || got.Transcript.Offset != 0 {
		t.Fatalf("an idle session's entry: %+v", got)
	}
}

// DV3: a connect or auth failure persists nothing, leaves the items pending with no repeat, and
// retries in 10 s.
func TestAConnectFailurePersistsNothing(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	a := tid(t, "alpha")
	dw.live(a)
	dw.persist(a, humanItem(a, 1, startWall, "hello"))
	before, err := dw.root.Read(dw.layout.Seat(a).Store(), 1<<22)
	check(t, err)
	dw.dial.then(dialStep{dial: fmt.Errorf("%w: auth: closed", inbox.ErrRetry)})
	dw.facts(false)
	if ev := dw.next(); !errors.Is(ev.err, inbox.ErrRetry) {
		t.Fatalf("first connection %+v", ev)
	}
	dw.short(retryWait, retryWait-time.Millisecond)
	after, err := dw.root.Read(dw.layout.Seat(a).Store(), 1<<22)
	check(t, err)
	if string(after.Data) != string(before.Data) {
		t.Fatal("a failed connect changed the store")
	}
	if n := dw.dial.dialCount(); n != 1 {
		t.Fatalf("%d dials before 10 s", n)
	}
	dw.advance(time.Millisecond)
	ev := dw.next()
	if ev.err != nil || !strings.Contains(ev.text, evID(1)) || strings.Contains(ev.text, "repeat:") {
		t.Fatalf("the retry: %+v", ev)
	}
}

// DV3, 15b: a user line of which nothing was written takes the awaiting entry back in one write,
// the items pending again with no repeat, and the delivery retried in 10 s.
func TestAUserLineNotWrittenReturnsTheItems(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	a := tid(t, "alpha")
	dw.live(a)
	dw.persist(a, humanItem(a, 1, startWall, "hello"))
	dw.dial.then(dialStep{write: fmt.Errorf("%w: user line: broken pipe", inbox.ErrRetry)})
	dw.facts(false)
	first := dw.next()
	dw.until(func() bool {
		s := dw.e.teams[a].store
		return len(s.Awaiting) == 0 && s.Pending[0].Handed == nil
	})
	s := dw.store(a)
	if len(s.Awaiting) != 0 || len(s.Ledger) != 0 || s.Pending[0].Repeat || s.Pending[0].Handed != nil {
		t.Fatalf("after a user line not written: %+v", s)
	}
	dw.advance(retryWait)
	again := dw.next()
	if again.err != nil || nonceOf(t, again.text) == nonceOf(t, first.text) || strings.Contains(again.text, "repeat:") {
		t.Fatalf("the retry: %+v", again)
	}
}

// DV3: a user line cut part-way leaves the entry unknown, for the transcript to settle.
func TestAPartWrittenUserLineStaysUnknown(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	a := tid(t, "alpha")
	dw.live(a)
	dw.persist(a, humanItem(a, 1, startWall, "hello"))
	dw.dial.then(dialStep{write: fmt.Errorf("%w: 10 of 400 bytes", inbox.ErrPartial)})
	dw.facts(false)
	dw.next()
	dw.idleAfter(0)
	s := dw.store(a)
	if len(s.Awaiting) != 1 || s.Awaiting[0].Written != state.WrittenUnknown || len(s.Ledger) != 0 || s.Pending[0].Handed == nil {
		t.Fatalf("after a cut user line: %+v", s)
	}
}

// A failed Finish changes nothing: a full write counts as sent (E11).
func TestAFailedFinishKeepsTheWrite(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	a := tid(t, "alpha")
	dw.live(a)
	dw.persist(a, humanItem(a, 1, startWall, "hello"))
	dw.dial.then(dialStep{finish: errors.New("read to EOF: reset")})
	dw.facts(false)
	dw.next()
	dw.until(func() bool { return len(dw.e.teams[a].store.Ledger) == 1 })
	// Finish follows the write's record; its error is only logged.
	dw.until(func() bool { return strings.Contains(dw.log.String(), "reset") })
	if s := dw.store(a); s.Awaiting[0].Written != state.WrittenYes {
		t.Fatalf("after a failed Finish: %+v", s.Awaiting)
	}
}

// DV3: one delivery per 2 s; items arriving meanwhile join the next.
func TestOneDeliveryEvery2s(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	a := tid(t, "alpha")
	dw.live(a)
	dw.facts(false)
	dw.persist(a, humanItem(a, 1, startWall, "one"))
	dw.next()
	dw.persist(a, humanItem(a, 2, startWall+1, "two"))
	dw.persist(a, humanItem(a, 3, startWall+2, "three"))
	dw.short(paceWait, paceWait-time.Millisecond)
	if n := dw.dial.dialCount(); n != 1 {
		t.Fatalf("%d dials within 2 s", n)
	}
	dw.advance(time.Millisecond)
	ev := dw.next()
	if !strings.Contains(ev.text, "2 messages on alpha") || !strings.Contains(ev.text, evID(2)) || !strings.Contains(ev.text, evID(3)) {
		t.Fatalf("the second delivery: %q", ev.text)
	}
}

// DV3: 8 entries and a 9th: the 9th stays pending for the next delivery.
func TestTheNinthItemFollows(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	a := tid(t, "alpha")
	dw.live(a)
	items := make([]team.Item, 0, 9)
	for n := 1; n <= 9; n++ {
		items = append(items, humanItem(a, n, startWall+int64(n), "hi"))
	}
	dw.persist(a, items...)
	dw.facts(false)
	first := dw.next()
	if !strings.Contains(first.text, "8 messages on alpha") || strings.Contains(first.text, evID(9)) || !strings.Contains(first.text, "1 more pending") {
		t.Fatalf("the first delivery: %q", first.text)
	}
	dw.advance(paceWait)
	if second := dw.next(); !strings.Contains(second.text, "1 message on alpha") || !strings.Contains(second.text, evID(9)) {
		t.Fatalf("the second delivery: %q", second.text)
	}
}

// B24: HUMAN items arriving behind agent items in one team go first in the delivery.
func TestHumanItemsGoFirstInTheirTeam(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	a := tid(t, "alpha")
	dw.live(a)
	dw.persist(a, peerItem(a, 1, startWall, "status"), humanItem(a, 2, startWall+5, "stop"))
	dw.facts(false)
	ev := dw.next()
	human, peer := strings.Index(ev.text, "id: "+evID(2)), strings.Index(ev.text, "id: "+evID(1))
	if human < 0 || peer < 0 || human > peer {
		t.Fatalf("order in %q", ev.text)
	}
}

// T18, DV3: three busy teams, one team per delivery: the oldest HUMAN item's team first, then the
// oldest item's; each awaiting entry in its own team's store.
func TestOneTeamPerDelivery(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	a, b, c := tid(t, "alpha"), tid(t, "bravo"), tid(t, "charlie")
	for _, id := range []paths.TeamID{a, b, c} {
		dw.live(id)
	}
	dw.persist(a, peerItem(a, 1, startWall, "a1"))
	dw.persist(b, humanItem(b, 2, startWall+10, "b2"))
	dw.persist(c, peerItem(c, 3, startWall+5, "c3"))
	dw.facts(false)
	order := make([]string, 0, 3)
	for i := range 3 {
		if i > 0 {
			dw.advance(paceWait)
		}
		ev := dw.next()
		order = append(order, strings.Fields(ev.text)[6])
		if n := strings.Count(ev.text, "<<teamchat "); n != 1 {
			t.Fatalf("delivery %d holds %d entries", i, n)
		}
	}
	if !slices.Equal(order, []string{"bravo", "alpha", "charlie"}) {
		t.Fatalf("teams in order %v", order)
	}
	dw.until(func() bool {
		s := dw.e.teams[c].store
		return len(s.Awaiting) == 1 && s.Awaiting[0].Written == state.WrittenYes
	})
	for _, id := range []paths.TeamID{a, b, c} {
		if s := dw.store(id); len(s.Awaiting) != 1 || len(s.Awaiting[0].IDs) != 1 {
			t.Fatalf("team %s's awaiting: %+v", id, s.Awaiting)
		}
	}
}

// B 2.7.3's stall: every write failed for 120 s shows stalled on the rows with pending items;
// the next full write clears it.
func TestTheStall(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	a, b := tid(t, "alpha"), tid(t, "bravo")
	dw.live(a)
	dw.live(b)
	for range 13 {
		dw.dial.then(dialStep{dial: fmt.Errorf("%w: connect: refused by load", inbox.ErrRetry)})
	}
	dw.persist(a, humanItem(a, 1, startWall, "hello"))
	dw.facts(false)
	for i := range 13 {
		if i > 0 {
			dw.advance(retryWait)
		}
		dw.next()
		if i == 11 {
			dw.local(func() {})
			if dw.status().Teams[0].Stalled {
				t.Fatal("stalled before 120 s")
			}
		}
	}
	dw.until(func() bool { return dw.e.stall.stalled })
	st := dw.status()
	if !st.Teams[0].Stalled || st.Teams[1].Stalled {
		t.Fatalf("status.json after 120 s of failures: %+v", st.Teams)
	}
	dw.advance(retryWait)
	dw.next()
	dw.until(func() bool { return !dw.e.stall.stalled })
	if dw.status().Teams[0].Stalled {
		t.Fatal("still stalled after a full write")
	}
}

// OF1: a team whose server is unreachable never holds another team's delivery: its restored
// item's re-fetch is bounded by 2 s, it waits out a retry, and the running team's item goes.
func TestAnUnreachableTeamHoldsNoOther(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	a, b := tid(t, "alpha"), tid(t, "bravo")
	down := dw.newStore(b)
	down.Pending = []state.Pending{{ID: evID(9), Room: proto.PurposeMain, Item: records.KindHuman, SentMS: startWall - 100, From: alice(b), Restored: true}}
	fb := dw.add(b, encodeStore(t, down))
	<-fb.running
	fb.notices.set(roster(b))
	fb.mu.Lock()
	fb.block = true
	fb.mu.Unlock()
	dw.e.SetHealth(b, team.Health{Down: true})
	dw.live(a)
	dw.persist(a, humanItem(a, 1, startWall, "hello"))
	dw.facts(false)
	dw.advance(refetchWait)
	ev := dw.next()
	if !strings.Contains(ev.text, "on alpha") || strings.Contains(ev.text, evID(9)) {
		t.Fatalf("the running team's delivery: %q", ev.text)
	}
	if s := dw.store(b); len(s.Pending) != 1 || s.Pending[0].Handed != nil {
		t.Fatalf("the down team's item: %+v", s.Pending)
	}
	fb.mu.Lock()
	fb.block = false
	fb.verdicts[evID(9)] = humanItem(b, 9, startWall-100, "restored").Verdict
	fb.mu.Unlock()
	dw.advance(retryWait)
	if ev := dw.next(); !strings.Contains(ev.text, evID(9)) || !strings.Contains(ev.text, "restored:") {
		t.Fatalf("the restored item once its server answers: %q", ev.text)
	}
}

// DV2: a slot that fails drops its item as unusable.render, with a link.log line naming it; a
// HUMAN one is on the not-shown line.
func TestAFailedSlotIsNotShown(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	a := tid(t, "alpha")
	dw.live(a)
	bad := humanItem(a, 1, startWall, "edited")
	bad.Verdict.Human.Revises = "not an event id"
	dw.persist(a, bad)
	dw.facts(false)
	ev := dw.next()
	if !strings.Contains(ev.text, "not shown: a message from a team human could not be shown; read it with teamchat verify '"+evID(1)+"'") {
		t.Fatalf("the delivery: %q", ev.text)
	}
	dw.until(func() bool { return len(dw.e.teams[a].store.Pending) == 0 })
	if s := dw.store(a); len(s.Pending) != 0 || len(s.Awaiting) != 0 {
		t.Fatalf("the store after the drop: %+v", s)
	}
	if u := dw.unusableSeen(); len(u) != 1 || u[0].Render != 1 {
		t.Fatalf("unusable counts told: %+v", u)
	}
	if !strings.Contains(dw.log.String(), evID(1)) {
		t.Fatalf("link.log: %q", dw.log.String())
	}
}

// A failed slot that is not a HUMAN one sends nothing, and still leaves pending.
func TestAFailedAgentSlotIsDropped(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	a := tid(t, "alpha")
	dw.live(a)
	bad := peerItem(a, 1, startWall, "x")
	bad.Verdict.Content.Text.To = []string{"not a user id"}
	bad.Verdict.Content.Relation.InReplyTo = "not an event id"
	dw.persist(a, bad)
	dw.facts(false)
	dw.until(func() bool { return len(dw.e.teams[a].store.Pending) == 0 })
	dw.idleAfter(0)
	if n := dw.dial.dialCount(); n != 0 {
		t.Fatalf("%d dials for a delivery with nothing to show", n)
	}
	if u := dw.unusableSeen(); len(u) != 1 || u[0].Render != 1 {
		t.Fatalf("unusable counts told: %+v", u)
	}
}

// n3e-D14: a team's trailing facts alone are one delivery, not awaiting proof, at most once a
// minute; n3e-D1: a held-back count is rendered and cleared at its full write.
func TestTrailingFactsAloneOnceAMinute(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	a := tid(t, "alpha")
	dw.live(a)
	dw.facts(false)
	dw.persistBatch(team.Batch{Team: a, NextBatch: "s1", Gap: true, HeldBack: map[string]int64{agent(a, 2): 3}})
	ev := dw.next()
	if !strings.Contains(ev.text, "0 messages on alpha") || !strings.Contains(ev.text, "\ngap: ") ||
		!strings.Contains(ev.text, "held back: 3 from repo.2+site.podman") {
		t.Fatalf("the trailing delivery: %q", ev.text)
	}
	dw.until(func() bool { return !dw.e.teams[a].delivery.gap })
	if s := dw.store(a); len(s.Awaiting) != 0 {
		t.Fatalf("a trailing delivery awaits proof: %+v", s.Awaiting)
	}
	dw.persistBatch(team.Batch{Team: a, NextBatch: "s2", Gap: true})
	// The loop idles to the minute from the first trailing delivery; nothing is due at 2 s.
	dw.short(trailingEvery, paceWait)
	dw.short(trailingEvery-paceWait, trailingEvery-paceWait-time.Millisecond)
	if n := dw.dial.dialCount(); n != 1 {
		t.Fatalf("%d trailing deliveries within a minute", n)
	}
	dw.advance(time.Millisecond)
	if ev := dw.next(); !strings.Contains(ev.text, "\ngap: ") || strings.Contains(ev.text, "held back:") {
		t.Fatalf("the second trailing delivery: %q", ev.text)
	}
}

// n3e-D13: a stopped team is told by a stopped line in its next delivery, once per stop.
func TestAStopIsToldOnce(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	a := tid(t, "alpha")
	dw.live(a)
	dw.e.SetHealth(a, team.Health{Stopped: "untrusted"})
	dw.persist(a, humanItem(a, 1, startWall, "one"))
	dw.facts(false)
	if ev := dw.next(); !strings.Contains(ev.text, "\nstopped: ") {
		t.Fatalf("the first delivery after the stop: %q", ev.text)
	}
	dw.persist(a, humanItem(a, 2, startWall+1, "two"))
	dw.advance(paceWait)
	if ev := dw.next(); strings.Contains(ev.text, "\nstopped: ") {
		t.Fatalf("the stop told twice: %q", ev.text)
	}
	dw.e.SetHealth(a, team.Health{})
	dw.e.SetHealth(a, team.Health{Stopped: "forbidden"})
	dw.persist(a, humanItem(a, 3, startWall+2, "three"))
	dw.advance(paceWait)
	if ev := dw.next(); !strings.Contains(ev.text, "\nstopped: ") {
		t.Fatalf("a second stop not told: %q", ev.text)
	}
}

// Facts that fail their slot are dropped with a log line, so they never hold the team.
func TestFactsThatDoNotRenderAreDropped(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	a := tid(t, "alpha")
	dw.live(a)
	dw.facts(false)
	// No answer persisted names a sender that fails the slot, so the fact is held by hand.
	if err := dw.e.do(t.Context(), func() {
		dw.e.teams[a].delivery.answered = []proto.Answered{{From: "no such handle"}}
	}); err != nil {
		t.Fatal(err)
	}
	dw.persistBatch(team.Batch{Team: a, NextBatch: "s1"})
	dw.until(func() bool { return len(dw.e.teams[a].delivery.answered) == 0 })
	dw.persist(a, humanItem(a, 1, startWall, "one"))
	dw.advance(paceWait)
	if ev := dw.next(); !strings.Contains(ev.text, evID(1)) || strings.Contains(ev.text, "answered:") {
		t.Fatalf("the next delivery: %q", ev.text)
	}
	if !strings.Contains(dw.log.String(), "trailing facts") {
		t.Fatalf("link.log: %q", dw.log.String())
	}
}

// DV6's session-level send: a membership notice shares the 2 s pace and is written true only
// after a full write; with no live socket it is not sent.
func TestSendMembership(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	a := tid(t, "alpha")
	dw.live(a)
	if ok, err := dw.e.SendMembership(t.Context(), workerNotice); ok || err != nil {
		t.Fatalf("with no socket: %v, %v", ok, err)
	}
	dw.facts(false)
	dw.persist(a, humanItem(a, 1, startWall, "one"))
	dw.next()
	got := dw.sendMembership(workerNotice)
	dw.advance(paceWait)
	if ev := dw.next(); !strings.Contains(ev.text, "a membership notice from teamchat") {
		t.Fatalf("the notice: %q", ev.text)
	}
	if r := <-got; !r.ok || r.err != nil {
		t.Fatalf("SendMembership = %+v", r)
	}
	dw.dial.then(dialStep{write: fmt.Errorf("%w: user line", inbox.ErrRetry)})
	got = dw.sendMembership(workerNotice)
	dw.advance(paceWait)
	dw.next()
	if r := <-got; r.ok || r.err != nil {
		t.Fatalf("a failed notice = %+v", r)
	}
	got = dw.sendMembership(proto.Membership{})
	dw.advance(retryWait)
	if r := <-got; r.ok || r.err == nil {
		t.Fatalf("a notice that does not render = %+v", r)
	}
}

// A notice to a socket that refuses, or is gone, is not written.
func TestSendMembershipToAFailingSocket(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	dw.facts(false)
	dw.dial.then(dialStep{dial: fmt.Errorf("%w: refused", inbox.ErrRetry)}, dialStep{dial: fmt.Errorf("%w: gone", inbox.ErrGone)})
	if ok, err := dw.e.SendMembership(t.Context(), workerNotice); ok || err != nil {
		t.Fatalf("to a refusing socket: %v, %v", ok, err)
	}
	got := dw.sendMembership(workerNotice)
	dw.advance(retryWait)
	if r := <-got; r.ok || r.err != nil {
		t.Fatalf("to a gone socket = %+v", r)
	}
	if dw.e.Live() {
		t.Fatal("live after the socket went")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := dw.e.SendMembership(ctx, workerNotice); err == nil {
		t.Fatal("an ended context sent")
	}
}

// One goroutine owns every transition: an item awaiting proof is never handed to a recv, an ack
// never settles it, a link restart does not return it, and a redaction leaves it.
func TestAnItemAwaitingProofIsNotPending(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	a := tid(t, "alpha")
	dw.live(a)
	dw.persist(a, humanItem(a, 1, startWall, "one"))
	dw.facts(false)
	nonce := nonceOf(t, dw.next().text)
	dw.until(func() bool { return len(dw.e.teams[a].store.Ledger) == 1 })
	if d := dw.done(wire.OpRecv, map[string]any{"team": nil}); d.Ack != "" {
		t.Fatalf("recv handed an item awaiting proof: %+v", d)
	}
	check(t, dw.e.Ack(t.Context(), nonce))
	check(t, dw.e.Return(t.Context(), nonce))
	dw.persistBatch(team.Batch{Team: a, NextBatch: "s2", Redacts: []string{evID(1)}})
	s := dw.store(a)
	if len(s.Pending) != 1 || s.Pending[0].Handed == nil || *s.Pending[0].Handed != nonce || s.Pending[0].Repeat {
		t.Fatalf("the item after recv, ack, return and a redaction: %+v", s.Pending)
	}
	again := dw.another(state.DeliverySocket)
	again.add(a, encodeStore(t, s))
	if p := again.store(a).Pending[0]; p.Handed == nil || p.Repeat || p.Restored {
		t.Fatalf("a restarted link returned an item awaiting proof: %+v", p)
	}
}

// n3e-D8: the failpoint between two stores' writes is hit between the hand-out's writes and
// between Ack's and Return's, never before the first.
func TestBetweenStoreWrites(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliveryWait)
	a, b := tid(t, "alpha"), tid(t, "bravo")
	dw.live(a)
	dw.live(b)
	dw.persist(a, humanItem(a, 1, startWall, "a"))
	dw.persist(b, humanItem(b, 2, startWall+1, "b"))
	d := dw.done(wire.OpRecv, map[string]any{"team": nil})
	if n := dw.hitCount(failpoint.LinkBetweenStoreWrites); n != 1 {
		t.Fatalf("%d hits in a two-store hand-out", n)
	}
	check(t, dw.e.Return(t.Context(), d.Ack))
	if n := dw.hitCount(failpoint.LinkBetweenStoreWrites); n != 2 {
		t.Fatalf("%d hits after a two-store return", n)
	}
	d = dw.done(wire.OpRecv, map[string]any{"team": nil})
	check(t, dw.e.Ack(t.Context(), d.Ack))
	if n := dw.hitCount(failpoint.LinkBetweenStoreWrites); n != 4 {
		t.Fatalf("%d hits after a two-store ack", n)
	}
}

// A recv that takes the items between the auth and the awaiting entry wins: the delivery writes
// nothing and sends no user line.
func TestARecvBetweenAuthAndTheEntryWins(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	a := tid(t, "alpha")
	dw.live(a)
	dw.persist(a, humanItem(a, 1, startWall, "one"))
	var (
		once sync.Once
		ack  = make(chan string, 1)
	)
	dw.setOnHit(func(n failpoint.Name) {
		if n == failpoint.LinkAfterAuth {
			once.Do(func() {
				d := dw.e.Serve(context.Background(), wire.Request{V: 1, Op: wire.OpRecv, Args: []byte(`{"team":null}`)}, &recorder{})
				ack <- d.Ack
			})
		}
	})
	dw.facts(false)
	if <-ack == "" {
		t.Fatal("the recv handed nothing")
	}
	dw.idleAfter(0)
	if n := dw.dial.writeCount(); n != 0 {
		t.Fatalf("%d user lines written for items a recv took", n)
	}
	if s := dw.store(a); len(s.Awaiting) != 0 {
		t.Fatalf("an awaiting entry for items a recv took: %+v", s.Awaiting)
	}
}
