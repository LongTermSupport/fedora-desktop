package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/leakcheck"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The windows' values (Plan 00267 Tasks 7.1, 7.5): an agent burst settles 2 s after its last
// item, capped 10 s after its first; a human's posts settle 3 s after the last, capped 10 s after
// the first; New gives every engine these.
func TestTheWindowsAreTheAgreedValues(t *testing.T) {
	want := window{quiet: 2 * time.Second, ceiling: 10 * time.Second, settle: 3 * time.Second, settleCeiling: 10 * time.Second}
	if coalesce != want {
		t.Fatalf("coalesce = %+v, want %+v", coalesce, want)
	}
	e, err := New(newWorld(t, state.DeliverySocket).config(state.DeliverySocket))
	check(t, err)
	if e.coalescing != coalesce {
		t.Fatalf("New's hold = %+v", e.coalescing)
	}
}

// dueAt is the window's rule on its own: a halt ping goes now; with a HUMAN item the human
// settle decides (the agent window is skipped); else the agent window.
func TestDueAt(t *testing.T) {
	s := time.Second
	agent := func(at time.Duration) arrival { return arrival{at: at} }
	human := func(at time.Duration) arrival { return arrival{human: true, at: at} }
	for name, tc := range map[string]struct {
		items []arrival
		want  time.Duration
	}{
		"nothing":              {nil, 0},
		"one agent item":       {[]arrival{agent(5 * s)}, 7 * s},
		"a burst restarts":     {[]arrival{agent(5 * s), agent(6 * s), agent(7 * s)}, 9 * s},
		"the agent ceiling":    {[]arrival{agent(5 * s), agent(9 * s), agent(14 * s)}, 15 * s},
		"one human item":       {[]arrival{human(5 * s)}, 8 * s},
		"a human restarts":     {[]arrival{human(5 * s), human(7 * s)}, 10 * s},
		"the human ceiling":    {[]arrival{human(5 * s), human(9 * s), human(13 * s)}, 15 * s},
		"a human skips agents": {[]arrival{agent(5 * s), human(5 * s), agent(7 * s)}, 8 * s},
		"a halt goes now":      {[]arrival{agent(5 * s), {halt: true, at: 6 * s}, human(6 * s)}, 0},
	} {
		if got := coalesce.dueAt(tc.items); got != tc.want {
			t.Errorf("%s: due at %s, want %s", name, got, tc.want)
		}
	}
	if got := (window{}).dueAt([]arrival{agent(5 * s), human(6 * s)}); got > 6*s {
		t.Errorf("no window holds until %s", got)
	}
}

// newHeldWorld is a delivery world whose engine holds deliveries by the production windows.
func newHeldWorld(t *testing.T) *dworld {
	t.Helper()
	leakcheck.Check(t)
	dw := &dworld{dial: newFakeDialer()}
	dw.world = buildWorld(t, state.DeliverySocket, func(c *Config) { c.Inbox = dw.dial }, func(e *Engine) { e.coalescing = coalesce })
	dw.transcript = filepath.Join(t.TempDir(), "session.jsonl")
	check(t, os.WriteFile(dw.transcript, []byte("{\"type\":\"user\"}\n"), 0o600))
	return dw
}

func haltItem(id paths.TeamID, n int, sent int64) team.Item {
	verb := proto.VerbHalt
	return team.Item{
		Pending: state.Pending{ID: evID(n), Room: proto.PurposeMain, Item: records.KindPing, SentMS: sent, From: agent(id, 2), Verb: &verb},
		Verdict: proto.Verdict{
			Outcome: proto.Deliver, Event: proto.Event{ID: evID(n), OriginTs: sent},
			Content: proto.Content{Ping: proto.Ping{Verb: verb, To: []string{agent(id, 1)}}},
		},
	}
}

// noDial fails the test if any connection was made.
func (dw *dworld) noDial() {
	dw.t.Helper()
	dw.settle()
	if n := dw.dial.dialCount(); n != 0 {
		dw.t.Fatalf("%d dials inside the window", n)
	}
}

// carries fails the test unless text holds every item n.
func carries(t *testing.T, text string, ns ...int) {
	t.Helper()
	for _, n := range ns {
		if !strings.Contains(text, evID(n)) {
			t.Fatalf("the delivery does not carry item %d:\n%s", n, text)
		}
	}
}

// Task 7.1: an agent burst arrives as one delivery: each item restarts the 2 s quiet bound, and
// nothing goes until the burst is quiet for it.
func TestAnAgentBurstIsOneDelivery(t *testing.T) {
	dw := newHeldWorld(t)
	a := tid(t, "alpha")
	dw.live(a)
	dw.facts(false)
	dw.persist(a, peerItem(a, 1, startWall, "one"))
	dw.short(coalesceQuiet, time.Second)
	dw.persist(a, peerItem(a, 2, startWall+1000, "two"))
	dw.short(coalesceQuiet, time.Second)
	dw.persist(a, peerItem(a, 3, startWall+2000, "three"))
	dw.short(coalesceQuiet, coalesceQuiet-time.Millisecond)
	dw.noDial()
	dw.advance(time.Millisecond)
	ev := dw.next()
	carries(t, ev.text, 1, 2, 3)
	if !strings.Contains(ev.text, "3 messages on alpha") {
		t.Fatalf("not one delivery of three: %q", ev.text)
	}
}

// Task 7.1: the ceiling bounds the wait: items that keep arriving inside the quiet bound go 10 s
// after the first, however many follow.
func TestTheCeilingBoundsAnAgentBurst(t *testing.T) {
	dw := newHeldWorld(t)
	a := tid(t, "alpha")
	dw.live(a)
	dw.facts(false)
	step := 1500 * time.Millisecond
	for n := 1; n <= 7; n++ {
		dw.persist(a, peerItem(a, n, startWall+int64(n), "burst"))
		if n < 7 {
			dw.short(coalesceQuiet, step)
		}
	}
	// 9 s have passed: the ceiling, not the quiet bound, is the timer now.
	dw.short(coalesceCeiling-6*step, coalesceCeiling-6*step-time.Millisecond)
	dw.noDial()
	dw.advance(time.Millisecond)
	carries(t, dw.next().text, 1, 2, 3, 4, 5, 6, 7)
}

// Task 7.5: a human's quick posts settle into one delivery: each restarts the 3 s settle, and an
// agent item beside them waits for the human window, not its own.
func TestAHumansPostsSettleIntoOneDelivery(t *testing.T) {
	dw := newHeldWorld(t)
	a := tid(t, "alpha")
	dw.live(a)
	dw.facts(false)
	dw.persist(a, humanItem(a, 1, startWall, "first thought"))
	dw.short(humanSettle, 2*time.Second)
	dw.persist(a, peerItem(a, 2, startWall+1, "an agent beside it"), humanItem(a, 3, startWall+2000, "second thought"))
	dw.short(humanSettle, humanSettle-time.Millisecond)
	dw.noDial()
	dw.advance(time.Millisecond)
	carries(t, dw.next().text, 1, 2, 3)
}

// Task 7.5: the 10 s cap bounds how late a human's first post can be.
func TestTheHumanCeiling(t *testing.T) {
	dw := newHeldWorld(t)
	a := tid(t, "alpha")
	dw.live(a)
	dw.facts(false)
	step := 2500 * time.Millisecond
	for n := 1; n <= 4; n++ {
		dw.persist(a, humanItem(a, n, startWall+int64(n), "post"))
		if n < 4 {
			dw.short(humanSettle, step)
		}
	}
	dw.short(humanCeiling-3*step, humanCeiling-3*step-time.Millisecond)
	dw.noDial()
	dw.advance(time.Millisecond)
	carries(t, dw.next().text, 1, 2, 3, 4)
}

// Tasks 7.1, 7.5: a halt ping passes straight through both windows, and takes what is pending
// with it.
func TestAHaltPingGoesAtOnce(t *testing.T) {
	dw := newHeldWorld(t)
	a := tid(t, "alpha")
	dw.live(a)
	dw.facts(false)
	dw.persist(a, humanItem(a, 1, startWall, "a human, settling"), peerItem(a, 2, startWall+1, "an agent"))
	dw.short(humanSettle, time.Second)
	dw.persist(a, haltItem(a, 3, startWall+2))
	carries(t, dw.next().text, 1, 2, 3)
}
