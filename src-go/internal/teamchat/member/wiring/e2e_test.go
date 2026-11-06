package wiring

import (
	"fmt"
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

// seated is `run --plain --state S --seat alpha/<self's seat>`.
func (w *world) seated() *session {
	s := w.launch("--state", w.state, "--seat", teamName+"/"+w.seatName)
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
	// recv exits once its ack is written; the link applies it after, so inbox may still list the
	// items owed for a moment.
	var owed []map[string]any
	eventually(t, func() bool {
		owed = s.records("inbox", "--team", teamName)
		return len(owed) == 0
	}, func() string { return fmt.Sprintf("inbox after the acks: %v", owed) })
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
	s.awaitDown(teamName)
	if s.down(betaTeam.name) {
		t.Fatalf("beta is down: %s", s.describe())
	}
	check(t, w.hs.fake.Start())
	within(t, upBound, func() bool { return !s.down(teamName) }, s.describe)
	s.recvUntil(w.say("alpha is back"))
}

// answerBound is how soon a verb the link serves from the store answers.
const answerBound = 5 * time.Second

// The schedule of a team whose homeserver cannot be reached, which these tests wait on: the
// waits below are derived from it, not from settle, which bounds only a real process's own work.
const (
	// backoffCap is the most a team's retry waits (teamsync's maxBackoff).
	backoffCap = 30 * time.Second
	// downAfter is how old a team's failure streak is when the retry that finds it so marks the
	// team down (teamsync's downAfter).
	downAfter = 15 * time.Second
	// longPoll is how long the server holds a team's sync when nothing new has happened in its
	// rooms (teamsync's longPoll).
	longPoll = 30 * time.Second
	// downBound is the longest a team takes to show down. The retries come after delays of 1, 2,
	// 4, 8 and 16 s, each less a jitter of up to a quarter, so the fourth falls short of
	// downAfter and the fifth, up to 31 s after the first failure, is the one that marks the team
	// down: a bound of settle alone is shorter than that. No delay is longer than backoffCap, so
	// the first retry past downAfter comes within backoffCap of it.
	downBound = downAfter + backoffCap + settle
	// upBound is the longest a team whose server is back takes to show up. Its retry comes
	// within backoffCap; the session it starts resumes from the stored position with no sync of
	// its own, and the team is shown up only when its first long-poll answers, which in an idle
	// room is longPoll later (teamsync's TestABackTeamIsShownUpAtItsFirstLongPoll).
	upBound = backoffCap + longPoll + settle
	// deliveredBound is the longest a send queued while the server was down takes to be posted
	// once the server is back: the sender posts nothing while the team is shown down, so the
	// send waits for the team to show up and is then posted at once.
	deliveredBound = upBound
)

// awaitDown waits for status to show the team down, within downBound.
func (s *session) awaitDown(name string) {
	s.w.t.Helper()
	within(s.w.t, downBound, func() bool { return s.down(name) }, s.describe)
}

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
