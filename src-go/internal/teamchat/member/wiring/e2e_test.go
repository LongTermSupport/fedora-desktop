package wiring

import (
	"strings"
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

// The owner's offline rule (B 2.4.5: the team is marked down after 15 s; nothing else is
// affected): a team whose homeserver cannot be reached does not stop the session. recv answers
// at once from the store, a verb that needs the server exits 20 unreachable for that team only
// (B 2.2.4's table; 69 is no runtime), status shows it down, and the other team works throughout.
// Once its server is back, so is the team, within the backoff's cap.
func TestE2EOfflineServer(t *testing.T) {
	w := newWorld(t, fakeServer(t))
	betaTeam := mustTeam("beta")
	beta := w.join(betaTeam, fakeServerFor(t, betaTeam))
	check(t, w.hs.fake.Stop())
	s := w.launch("--state", w.state, "--seat", teamName+"/"+mySeat, "--seat", betaTeam.name+"/"+mySeat)
	b := s.as(beta)
	b.seatedIn()
	clk := clock.System()
	from := clk.Boot()
	if recs := s.records("recv", "--team", teamName); len(recs) != 0 || clk.Boot()-from > answerBound {
		t.Fatalf("recv for the offline team: %v in %v", recs, clk.Boot()-from)
	}
	if r := s.exec("history", "--team", teamName); r.ExitCode != 20 || !strings.Contains(string(r.Stderr), `"code":"unreachable"`) {
		t.Fatalf("history for the offline team: exit %d %s", r.ExitCode, r.Stderr)
	}
	b.records("history", "--team", betaTeam.name)
	b.recvUntil(beta.say("beta works"))
	eventually(t, func() bool { return s.down(teamName) }, s.describe)
	if s.down(betaTeam.name) {
		t.Fatalf("beta is down: %s", s.describe())
	}
	check(t, w.hs.fake.Start())
	within(t, backoffCap+settle, func() bool { return !s.down(teamName) }, s.describe)
	s.recvUntil(w.say("alpha is back"))
}

// answerBound is how soon a verb the link serves from the store answers.
const answerBound = 5 * time.Second

// backoffCap is the most a team's retry waits (teamsync's maxBackoff).
const backoffCap = 30 * time.Second

// down is whether status shows the team down.
func (s *session) down(name string) bool {
	s.w.t.Helper()
	for _, r := range s.records("status", "--team", name) {
		if r["kind"] == "team" {
			return r["down"] == true
		}
	}
	s.w.t.Fatalf("status has no team record for %s", name)
	return false
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
