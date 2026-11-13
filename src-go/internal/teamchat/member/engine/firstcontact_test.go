package engine

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// `say --wait-contact` (Plan 00246 Task 8.11, X30's scripted message leg): a say that reaches
// the link before its team's first contact in the run (the seat held, the team goroutine
// started, its roster not yet verified nor its sends recovered, and the team not known down)
// waits for that contact, at most firstContactWait, and then answers as any say does. A
// one-shot `run --plain -- teamchat say --wait-contact` sends at that moment. A say without it
// keeps n3d-D19 (TestN3dD19ASendBeforeTheRecoveryIsQueuedAtOnce).

// sayWaiting is `say --wait-contact` to name in id.
func (w *world) sayWaiting(id *paths.TeamID, text, name string) answer {
	return w.request(w.t.Context(), wire.OpSay, wire.SayArgs{Team: id, To: []string{name}, WaitContact: true}, &text)
}

// sayLater is sayWaiting made off the test goroutine, its answer on the channel.
func (w *world) sayLater(id *paths.TeamID, text, name string) <-chan answer {
	got := make(chan answer, 1)
	go func() { got <- w.sayWaiting(id, text, name) }()
	return got
}

// contact is the team's first contact: its roster verified and its sends recovered.
func (w *world) contact(id paths.TeamID, f *fakeTeam) {
	w.t.Helper()
	f.notices.set(sendRoster(id))
	f.notices.mu.Lock()
	f.notices.token = memberAccess
	f.notices.mu.Unlock()
	w.recover(id, f)
}

func TestASayWaitingForContactIsPostedOnceTheTeamIsReached(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.held(ops)
	got := w.sayLater(&ops, "before the first contact", "repo.2")
	w.awaitDue(firstContactWait)
	if !strings.Contains(w.log.String(), "team ops: "+msgWaitsForContact) {
		t.Errorf("no log line for the wait: %s", w.log.String())
	}
	w.contact(ops, f)
	posted(t, <-got)
}

// A seat's first session has no store until the team's first Load, which follows the recovery
// (teamsync's session order): the say waits for the store too, or it is 20 "not synced yet".
func TestASayWaitingForContactWaitsForTheFirstStore(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.add(ops, nil)
	<-f.running
	got := w.sayLater(&ops, "on a seat never synced", "repo.2")
	w.awaitDue(firstContactWait)
	w.contact(ops, f)
	var waiting bool
	w.local(func() { waiting = w.e.teams[ops].contact != nil })
	if !waiting {
		t.Fatal("the say was released before the team's store exists")
	}
	_, err := w.e.Load(ops, agent(ops, 1))
	check(t, err)
	posted(t, <-got)
}

func TestASayWaitingForContactIsQueuedWhenTheTeamIsDown(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	check(t, w.root.MkdirAll(w.layout.Seat(ops).Dir()))
	check(t, state.Write(w.root, w.layout.Seat(ops).RosterCache(), state.RosterCache{Rev: 4, Content: rosterContent(ops, 4)}))
	w.held(ops)
	// alice is a human the cached roster names (TestN3dD3ACachedRosterComposes).
	got := w.sayLater(&ops, "while the server is not answering", "alice")
	w.awaitDue(firstContactWait)
	w.e.SetHealth(ops, team.Health{Down: true})
	queued(t, <-got)
}

func TestASayWaitsForContactAtMostItsBound(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.held(ops)
	got := w.sayLater(&ops, "nobody answers", "repo.2")
	w.awaitDue(firstContactWait)
	w.clk.Advance(firstContactWait)
	refusedCode(t, "no contact within the bound", firstOf(<-got), exits.CodeUnreachable)
}

// Without --wait-contact a say before the first contact is answered at once, as before.
func TestASayWithoutWaitContactDoesNotWait(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.held(ops)
	refusedCode(t, "no roster yet", firstOf(w.say(&ops, "at once", "repo.2")), exits.CodeUnreachable)
	if strings.Contains(w.log.String(), msgWaitsForContact) {
		t.Errorf("a say without --wait-contact waited: %s", w.log.String())
	}
}

// A team already reached holds no say.
func TestASayToAReachedTeamDoesNotWait(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.sending(ops)
	posted(t, w.sayWaiting(&ops, "after the first contact", "repo.2"))
	if strings.Contains(w.log.String(), msgWaitsForContact) {
		t.Errorf("a say to a reached team waited: %s", w.log.String())
	}
}
