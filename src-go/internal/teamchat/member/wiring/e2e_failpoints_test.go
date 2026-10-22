//go:build busfailpoints

package wiring

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/failpoint"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
)

// Each n3c failpoint in a real session (SPEC-n3 PT1, handoff section 8): the point's process is
// killed by SIGKILL there once, and the row checks what B 2.15 holds before n3e: P1 no accepted
// item lost, P3 the position never ahead of its items, P4 a removed team's items gone, P5 each
// notice at most once, P6 one link at a time, P11 a handed item back after the kill.

// linkKilledLine is keeper.log's line for a link SIGKILL ended (128+9).
const linkKilledLine = "the link exited 137"

// armed is a world whose first process of each verb arms its point, once.
func armed(t *testing.T, pairs ...string) *world {
	t.Helper()
	w := newWorld(t, fakeServer(t))
	w.arm = strings.Join(pairs, ",")
	return w
}

func arm(verb string, point failpoint.Name) string { return verb + "=" + string(point) }

// linkKilled is whether a link of the session ended by SIGKILL: an armed point fired.
func (s *session) linkKilled() bool { return strings.Contains(s.log("keeper.log"), linkKilledLine) }

// awaitLinkKilled waits for an armed link point to fire, receiving meanwhile when recv is set,
// and returns the ids those receives printed.
func (s *session) awaitLinkKilled(recv bool) map[string]int {
	s.w.t.Helper()
	seen := map[string]int{}
	eventually(s.w.t, func() bool {
		if recv {
			for _, id := range s.recvAny() {
				seen[id]++
			}
		}
		return s.linkKilled()
	}, s.describe)
	return seen
}

// recvAny is one recv whatever its exit, a link killed under it included: the ids it printed.
func (s *session) recvAny() []string {
	s.w.t.Helper()
	var ids []string
	for line := range strings.SplitSeq(string(s.exec("recv", "--team", s.w.team.name).Stdout), "\n") {
		var r map[string]any
		if json.Unmarshal([]byte(line), &r) != nil {
			continue
		}
		if id, ok := r["id"].(string); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

// receiveAll receives until every id has come at least once, adding each copy to seen.
func (s *session) receiveAll(seen map[string]int, ids ...string) map[string]int {
	s.w.t.Helper()
	eventually(s.w.t, func() bool {
		for _, id := range s.recvAny() {
			seen[id]++
		}
		return !slices.ContainsFunc(ids, func(id string) bool { return seen[id] == 0 })
	}, s.describe)
	return seen
}

// notices counts me's notices that are want (P5).
func (w *world) notices(want string) int {
	w.t.Helper()
	n := 0
	for _, got := range w.lifecycle() {
		if got == want {
			n++
		}
	}
	return n
}

// oneLink is P6 as a session shows it: of the links keeper.log names, at most one is alive.
func (s *session) oneLink() {
	s.w.t.Helper()
	alive := 0
	for _, pid := range s.links() {
		if !gone(pid) {
			alive++
		}
	}
	if alive > 1 {
		s.w.t.Fatalf("%d links alive: %s", alive, s.describe())
	}
}

// P1, P3: a link killed after it judged its first batch and before it persisted the position
// re-fetches from the old position: the item said before the session comes once, and so does
// one said after the restart.
func TestFailpointAfterJudgeBeforePersist(t *testing.T) {
	w := armed(t, arm("link", failpoint.LinkAfterJudgeBeforePersist))
	early := w.say("before the session")
	s := w.seated()
	s.awaitLinkKilled(false)
	s.seatedIn()
	later := w.say("after the restart")
	if seen := s.receiveAll(map[string]int{}, early, later); seen[early] != 1 || seen[later] != 1 {
		t.Fatalf("seen %v: %s", seen, s.describe())
	}
	s.oneLink()
}

// P11: a link killed after the hand-out's write, before the records are sent, loses none:
// the recv under it printed nothing, and the next link hands the item out once.
func TestFailpointAfterHandOut(t *testing.T) {
	w, s, id := handedThenKilled(t, failpoint.LinkAfterHandOut)
	if seen := s.receiveAll(map[string]int{}, id); seen[id] != 1 {
		t.Fatalf("seen %v: %s", seen, s.describe())
	}
	finishHanded(w, s)
}

// P11: a link killed while the items are handed, after the records reached the recv and
// before its ack: the item is back in pending, and the next link hands it out again.
func TestFailpointWhileHanded(t *testing.T) {
	w, s, id := handedThenKilled(t, failpoint.LinkWhileHanded)
	if seen := s.receiveAll(map[string]int{}, id); seen[id] != 1 {
		t.Fatalf("after the kill seen %v: %s", seen, s.describe())
	}
	finishHanded(w, s)
}

// handedThenKilled is a seated session whose armed link is killed under a recv of the item it
// returns; the recvs before the kill may print it at most once.
func handedThenKilled(t *testing.T, point failpoint.Name) (*world, *session, string) {
	t.Helper()
	w := armed(t, arm("link", point))
	s := w.seated()
	id := w.say("handed")
	if before := s.awaitLinkKilled(true); before[id] > 1 {
		t.Fatalf("before the kill seen %v: %s", before, s.describe())
	}
	s.seatedIn()
	return w, s, id
}

// finishHanded is a handed row's end: the item acked, so a further recv is empty, and one link.
func finishHanded(w *world, s *session) {
	w.t.Helper()
	if again := s.recvAny(); len(again) != 0 {
		w.t.Fatalf("after the ack recv printed %v: %s", again, s.describe())
	}
	s.oneLink()
}

// desiredSeated is a session launched on the desired membership holding alpha's seat.
func (w *world) desiredSeated() *session {
	w.t.Helper()
	w.desire(1, []state.DesiredSeat{{Team: alphaID, Seat: mySeat}}, nil)
	s := w.launch("--desired", w.desired, "--state", w.state)
	s.seatedIn()
	if _, err := os.Stat(w.storePath()); err != nil {
		w.t.Fatalf("the seated store: %v", err)
	}
	return s
}

// release is generation 2: alpha's seat released.
func (w *world) release() {
	w.t.Helper()
	w.desire(2, nil, []state.Farewell{{Team: alphaID, Seat: mySeat, Reason: state.FarewellReleased}})
}

// P4, P5: a link killed inside remove leaves the removal to the keeper, which deletes the
// store under the lock; left released is posted at most once, and the team's later items
// reach no recv.
func TestFailpointInRemove(t *testing.T) {
	w := armed(t, arm("link", failpoint.LinkInRemove))
	s := w.desiredSeated()
	w.release()
	s.awaitLinkKilled(false)
	removedEnd(t, w, s)
}

// P5: a link killed after it answered removed and before it posted the leave: the keeper's
// owed leave is posted by the next link, once.
func TestFailpointAfterRemoveAnswered(t *testing.T) {
	w := armed(t, arm("link", failpoint.LinkAfterRemoveAnswered))
	s := w.desiredSeated()
	w.release()
	s.awaitLinkKilled(false)
	w.awaitNotice("left released")
	removedEnd(t, w, s)
}

// removedEnd is a removal's outcome: the store gone, left released at most once, and the
// team's later item in no recv.
func removedEnd(t *testing.T, w *world, s *session) {
	t.Helper()
	eventually(t, func() bool {
		_, err := os.Stat(w.storePath())
		return os.IsNotExist(err)
	}, s.describe)
	after := w.say("after the removal")
	if slices.Contains(s.recvAny(), after) {
		t.Fatalf("a removed team's item reached recv: %s", s.describe())
	}
	if n := w.notices("left released"); n > 1 {
		t.Fatalf("left released %d times: %s", n, s.describe())
	}
	s.oneLink()
}

// P5: a link killed after the end's notice is posted and before it is marked sent: the
// keeper's end runs on, and left other is posted once.
func TestFailpointMidEndSends(t *testing.T) {
	w := armed(t, arm("link", failpoint.LinkMidEndSends))
	s := w.seated()
	kill(t, s.keeper.Pid(), syscall.SIGTERM)
	s.wait()
	w.awaitNotice("left other")
	if n := w.notices("left other"); n != 1 {
		t.Fatalf("left other %d times", n)
	}
	// The run directory is gone with the keeper; the store says the link died between the
	// post and its record: the end is attempted, never sent.
	if end := w.store().Run.Ended; end == nil || end.State != state.StateAttempted {
		t.Fatalf("the run's end %+v", end)
	}
}

// storePath is alpha's store in the world's state root.
func (w *world) storePath() string { return w.state + "/" + teamName + "/store.json" }

// store is alpha's store as it is on disk.
func (w *world) store() state.Store {
	w.t.Helper()
	data, err := os.ReadFile(w.storePath())
	check(w.t, err)
	var st state.Store
	check(w.t, json.Unmarshal(data, &st))
	return st
}

// P5: the keeper killed after CMD's exit and before end is sent: the link sees its keeper go
// and posts disconnected, once.
func TestFailpointKeeperBeforeEnd(t *testing.T) {
	w := armed(t, arm("run", failpoint.KeeperBeforeEnd))
	s := w.seated()
	kill(t, s.agent, syscall.SIGKILL)
	keeperKilled(t, w, s)
}

// P1, P6: the keeper killed at its link's first state, before CMD: the link ends, so no writer
// of the store remains, and the next session receives what was said meanwhile.
func TestFailpointKeeperLinkWriting(t *testing.T) {
	w := armed(t, arm("run", failpoint.KeeperLinkWriting))
	s := w.start("--state", w.state, "--seat", teamName+"/"+mySeat)
	s.wait()
	st := <-s.ended
	s.ended <- st
	if st.Signal != syscall.SIGKILL || len(s.links()) != 1 {
		t.Fatalf("the keeper ended %+v: %s", st, s.describe())
	}
	eventually(t, func() bool { return gone(s.links()[0]) }, s.describe)
	meanwhile := w.say("while no session ran")
	again := w.seated()
	if seen := again.receiveAll(map[string]int{}, meanwhile); seen[meanwhile] != 1 {
		t.Fatalf("seen %v: %s", seen, again.describe())
	}
}

// A relaunch of the same seat at once after a keeper crash is seated: a guard of the relaunch.
// Against fakehs the seat is free by the time it is reached, so the wait for a link that still
// holds it is proven by keeper's TestHeldSeatOfAnEndingSessionIsWaitedFor, not here.
func TestFailpointRelaunchWhileTheLinkEnds(t *testing.T) {
	w := armed(t, arm("run", failpoint.KeeperLinkWriting))
	s := w.start("--state", w.state, "--seat", teamName+"/"+mySeat)
	s.wait()
	st := <-s.ended
	s.ended <- st
	if st.Signal != syscall.SIGKILL || len(s.links()) != 1 {
		t.Fatalf("the keeper ended %+v: %s", st, s.describe())
	}
	link := s.links()[0]
	again := w.seated()
	eventually(t, func() bool { return gone(link) }, again.describe)
}

// keeperKilled is a keeper's crash at its armed point: ended by SIGKILL, disconnected posted
// once, and every link it started gone.
func keeperKilled(t *testing.T, w *world, s *session) {
	t.Helper()
	s.wait()
	st := <-s.ended
	s.ended <- st
	if st.Signal != syscall.SIGKILL {
		t.Fatalf("the keeper ended %+v: %s", st, s.describe())
	}
	w.awaitNotice("disconnected")
	if n := w.notices("disconnected"); n != 1 {
		t.Fatalf("disconnected %d times: %s", n, s.describe())
	}
	for _, pid := range s.links() {
		eventually(t, func() bool { return gone(pid) }, s.describe)
	}
}

// P4's pinned exception: the keeper killed between its link's death in a removal and the
// store's deletion leaves the store, and a re-add delivers the gap.
func TestFailpointKeeperBeforeStoreDelete(t *testing.T) {
	w := armed(t, arm("link", failpoint.LinkInRemove), arm("run", failpoint.KeeperBeforeStoreDelete))
	s := w.desiredSeated()
	w.release()
	s.wait()
	st := <-s.ended
	s.ended <- st
	if st.Signal != syscall.SIGKILL || !s.linkKilled() {
		t.Fatalf("the keeper ended %+v: %s", st, s.describe())
	}
	if _, err := os.Stat(w.storePath()); err != nil {
		t.Fatalf("the store: %v", err)
	}
	s.cleanup()
	gap := w.say("in the gap")
	w.desire(3, []state.DesiredSeat{{Team: alphaID, Seat: mySeat}}, nil)
	again := w.launch("--desired", w.desired, "--state", w.state)
	again.seatedIn()
	if seen := again.receiveAll(map[string]int{}, gap); seen[gap] != 1 {
		t.Fatalf("seen %v: %s", seen, again.describe())
	}
}
