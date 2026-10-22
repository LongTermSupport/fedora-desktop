package seat

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// At most one holding of a session and team has the session's seat directory (n2c-D12's
// addendum): a take refuses a session whose seat directory for the team is there already, and
// a release moves the directory only when no other holding of the team that names the session
// is as new as its own. Both are judged under the allocation lock from host-owned state: the
// directory is only looked at, never opened, and the holdings are the pool's held.json files.

// takeSeat takes seat for session under the world's allocation lock.
func (w *world) takeSeat(seat Seat, session string) (Taken, error) {
	w.t.Helper()
	a := w.lock()
	defer w.unlock(a)
	return w.store.Take(w.t.Context(), a, Take{Seat: seat, Session: mustSession(w.t, session), State: StateActive})
}

// releaseSeat releases seat for session.
func (w *world) releaseSeat(seat Seat, session string, leaving bool) error {
	w.t.Helper()
	return w.store.Release(w.t.Context(), Release{Seat: seat, Session: mustSession(w.t, session), Leaving: leaving})
}

// poolOf is seat's pool directory in the world's checkout.
func (w *world) poolOf(seat Seat) paths.PoolSeat {
	return w.store.store.PoolSeat(w.key, seat.Team, seat.Name)
}

// diedBeforeCopy is a take of seat for session that dies at step 5: held/ complete with its
// token, and no session seat directory, as when sessions/<session>/ is not the owner's 0700.
func (w *world) diedBeforeCopy(seat Seat, session string) {
	w.t.Helper()
	dir := w.store.store.Session(mustSession(w.t, session))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		w.t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		w.t.Fatal(err)
	}
	if _, err := w.takeSeat(seat, session); !errors.Is(err, ErrConfig) {
		w.t.Fatalf("a take whose session directory is 0755: %v", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		w.t.Fatal(err)
	}
	if !exists(w.t, w.poolOf(seat).HeldToken()) || exists(w.t, w.sessionSeat(session).Dir()) {
		w.t.Fatal("the take did not die between steps 4 and 5")
	}
}

// ST1, n2c-D12: a take into a session that has the team's seat directory already, another
// seat's copy, is ErrHeld naming it before held/ is made, so no token is minted.
func TestATakeRefusesASessionThatHasTheTeamsSeat(t *testing.T) {
	w := newWorld(t)
	w.seed()
	seatB := w.seedSeatB(t)
	takenA := w.take("s1")
	_, err := w.takeSeat(seatB, "s1")
	if !errors.Is(err, ErrHeld) || !strings.Contains(err.Error(), takenA.Dir) {
		t.Fatalf("a second seat of the team into s1: %v", err)
	}
	if exists(t, w.poolOf(seatB).Held()) {
		t.Errorf("held/ was made for a take that was refused")
	}
	if !exists(t, takenA.Dir+"/token") {
		t.Errorf("the refused take touched the copy that is there")
	}
	if _, err := w.takeSeat(seatB, "s2"); err != nil {
		t.Errorf("a take of the seat into another session: %v", err)
	}
}

// ST1, n2c-D12: a session directory the take cannot look into (a link where it was) is ErrHeld
// too, before held/ is made: the take mints nothing it could not copy.
func TestATakeRefusesASessionItCannotLookInto(t *testing.T) {
	w := newWorld(t)
	w.seed()
	dir := w.store.store.Session(mustSession(t, "s1"))
	writeFile(t, w.home+"/elsewhere/x", "")
	if err := os.MkdirAll(dir[:strings.LastIndex(dir, "/")], 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(w.home+"/elsewhere", dir); err != nil {
		t.Fatal(err)
	}
	if _, err := w.tryTake("s1", StateActive); !errors.Is(err, ErrHeld) || !strings.Contains(err.Error(), w.sessionSeat("s1").Dir()) {
		t.Fatalf("a take into a linked session directory: %v", err)
	}
	if exists(t, w.pool().Held()) {
		t.Errorf("held/ was made for a take that was refused")
	}
}

// SX1, SX2, n2c-D12, the n2c report's open low: seat A's take for s1 died before step 5, then
// seat B's take for s1 made the session's seat directory. A leave of A neither waits on B's
// live keeper nor moves B's copy, and A's holding is released.
func TestALeaveOfAHoldingWithNoCopyLeavesAnotherSeatsCopy(t *testing.T) {
	w := newWorld(t)
	w.seed()
	seatB := w.seedSeatB(t)
	w.diedBeforeCopy(w.seat, "s1")
	tokenA := readFile(t, w.pool().HeldToken())
	w.sleep(time.Second)
	takenB, err := w.takeSeat(seatB, "s1")
	if err != nil {
		t.Fatalf("take of B: %v", err)
	}
	end := w.keep("s1", lockRecord)
	defer end()
	w.sessions.set("s1", true)
	before := w.clock.Now()
	if err := w.release("s1", true); err != nil {
		t.Fatalf("the leave of A: %v", err)
	}
	if waited := w.clock.Now().Sub(before); waited != 0 {
		t.Errorf("the leave of A waited %v on B's keeper", waited)
	}
	if !exists(t, takenB.Dir+"/token") {
		t.Errorf("B's live session copy was moved by A's leave")
	}
	if exists(t, w.pool().Held()) || w.alive(tokenA) {
		t.Errorf("A's holding was not released")
	}
}

// SX2, SX3, n2c-D12, the n2c report's open low: once s1 is over, A's release leaves B's copy
// and announces no run of B's; B's own release then moves its copy and announces its run.
func TestAReleaseOfAHoldingWithNoCopyLeavesAnotherSeatsCopy(t *testing.T) {
	w := newWorld(t)
	w.seed()
	seatB := w.seedSeatB(t)
	w.diedBeforeCopy(w.seat, "s1")
	w.sleep(time.Second)
	takenB, err := w.takeSeat(seatB, "s1")
	if err != nil {
		t.Fatalf("take of B: %v", err)
	}
	w.keep("s1", lockRecord)()
	if err := w.release("s1", false); err != nil {
		t.Fatalf("the release of A: %v", err)
	}
	if !exists(t, takenB.Dir+"/token") {
		t.Fatalf("B's session copy was moved by A's release")
	}
	if calls := w.notices.calls(); len(calls) != 0 {
		t.Errorf("A's release announced B's run: %v", calls)
	}
	if err := w.releaseSeat(seatB, "s1", false); err != nil {
		t.Fatalf("the release of B: %v", err)
	}
	if exists(t, takenB.Dir) || len(w.notices.calls()) != 1 {
		t.Errorf("B's release did not move its copy and announce its run: %v", w.notices.calls())
	}
}

// SX2, n2c-D12: step 2 judges again under the lock. A's copy is there at step 1 with its keeper
// live; while A's release waits, the member removes the copy and B's take for s1 makes its own
// (in the same millisecond as A's, so a holding as new as A's keeps the directory too). Step 2
// leaves B's copy where it is.
func TestACopyAnotherSeatMadeDuringTheWaitIsNotMoved(t *testing.T) {
	w := newWorld(t)
	w.seed()
	seatB := w.seedSeatB(t)
	takenA := w.take("s1")
	end := w.keep("s1", lockRecord)
	w.sessions.set("s1", true)
	g := &gateClock{fakeClock: w.clock, waiting: make(chan struct{}), goOn: make(chan struct{})}
	w.store.clock = g
	done := make(chan error)
	go func() {
		done <- w.store.Release(context.Background(), Release{Seat: w.seat, Session: mustSession(t, "s1"), Leaving: true})
	}()
	<-g.waiting
	end()
	removeErr := os.RemoveAll(takenA.Dir)
	takenB, takeErr := w.takeSeat(seatB, "s1")
	close(g.goOn)
	if err := errors.Join(removeErr, takeErr); err != nil {
		t.Fatalf("the member's remove and the take of B: %v", err)
	}
	if err := <-done; err != nil {
		t.Errorf("the release of A: %v", err)
	}
	if !exists(t, takenB.Dir+"/token") || exists(t, w.releasedSession(takenA)) {
		t.Errorf("B's live session copy was moved by A's release")
	}
}

// SX2, n2c-D12: an older holding of another seat naming the session, its take dead before
// step 5, does not keep this holding's own copy from its release.
func TestAnOlderHoldingDoesNotKeepThisOnesCopy(t *testing.T) {
	w := newWorld(t)
	w.seed()
	seatB := w.seedSeatB(t)
	w.diedBeforeCopy(seatB, "s1")
	w.sleep(time.Second)
	takenA := w.take("s1")
	if err := w.release("s1", true); err != nil {
		t.Fatalf("the leave of A: %v", err)
	}
	if exists(t, takenA.Dir) || !exists(t, w.releasedSession(takenA)) {
		t.Errorf("A's own copy was not moved to its quarantine")
	}
}
