package wiring

import (
	"syscall"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
)

// The member end to end, every process real: the keeper (`run`), its link, its CMD and the
// client verbs, each the test binary as teamchat, over fakehs on HTTP (W, handoff section 8).

// linkEndBound is how soon a link whose keeper died posts its disconnected (endWait).
const linkEndBound = 5 * time.Second

// seated is `run --plain --state S --seat alpha/2`.
func (w *world) seated() *session {
	s := w.launch("--state", w.state, "--seat", teamName+"/"+mySeat)
	s.seatedIn()
	return s
}

// A human's item and a peer's reach recv, once each, and are acked: the next recv and the
// inbox are empty.
func TestE2EItems(t *testing.T) {
	w := newWorld(t, fakeServer(t))
	s := w.seated()
	human, fromPeer := w.say("hello"), w.peerText("hi from peer")
	got := s.recvUntil(human, fromPeer)
	kinds := map[any]any{}
	for _, r := range got {
		kinds[r["id"]] = r["kind"]
	}
	if kinds[human] != "human" || kinds[fromPeer] != "peer" {
		t.Fatalf("records %v", got)
	}
	if recs := s.records("recv", "--team", teamName); len(recs) != 0 {
		t.Fatalf("a second recv: %v", recs)
	}
	if recs := s.records("inbox", "--team", teamName); len(recs) != 0 {
		t.Fatalf("inbox after the acks: %v", recs)
	}
}

// A seat desired.json adds while the session runs receives; one it takes out with a farewell
// says left (RC1, B 2.10).
func TestE2EHotAddRemove(t *testing.T) { hotAddRemove(t, fakeServer(t)) }

func hotAddRemove(t *testing.T, hs homeserver) {
	t.Helper()
	w := newWorld(t, hs)
	w.desire(1, nil, nil)
	s := w.launch("--desired", w.desired, "--state", w.state)
	w.desire(2, []state.DesiredSeat{{Team: alphaID, Seat: mySeat}}, nil)
	s.seatedIn()
	s.recvUntil(w.say("after the hot add"))
	w.desire(3, nil, []state.Farewell{{Team: alphaID, Seat: mySeat, Reason: state.FarewellReleased}})
	w.awaitNotice("left released")
}

// A SIGTERM to the keeper ends the session: its goodbye is left other, and it exits.
func TestE2EKeeperTerm(t *testing.T) {
	w := newWorld(t, fakeServer(t))
	s := w.seated()
	kill(t, s.keeper.Pid(), syscall.SIGTERM)
	s.wait()
	w.awaitNotice("left other")
}

// CMD killed by SIGKILL is a session that ended without a goodbye: disconnected.
func TestE2ECmdKilled(t *testing.T) {
	w := newWorld(t, fakeServer(t))
	s := w.seated()
	kill(t, s.agent, syscall.SIGKILL)
	s.wait()
	w.awaitNotice("disconnected")
}

// The keeper killed by SIGKILL: the link sees its pidfd and posts disconnected within 5 s
// (LN1, T20).
func TestE2EKeeperKilled(t *testing.T) {
	w := newWorld(t, fakeServer(t))
	s := w.seated()
	clk := clock.System()
	from := clk.Boot()
	kill(t, s.keeper.Pid(), syscall.SIGKILL)
	w.awaitNotice("disconnected")
	if took := clk.Boot() - from; took > linkEndBound {
		t.Fatalf("disconnected took %v", took)
	}
}
