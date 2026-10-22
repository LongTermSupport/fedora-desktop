package seat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

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

// diedBeforeCopy is a take of seat for session s1 that dies at step 5: held/ complete with its
// token, and no session seat directory, as when sessions/s1/ is not the owner's 0700.
func (w *world) diedBeforeCopy(seat Seat) {
	w.t.Helper()
	const session = "s1"
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

// ST1, n2c-D12: a session directory the take cannot look into (a link or a FIFO where it was)
// is ErrHeld too, before held/ is made: the take mints nothing it could not copy, and the FIFO is
// never opened, so it cannot hold the take.
func TestATakeRefusesASessionItCannotLookInto(t *testing.T) {
	for name, plant := range map[string]plant{
		"a link": func(w *world, p string) error { return os.Symlink(w.home+"/elsewhere", p) },
		"a FIFO": func(_ *world, p string) error { return unix.Mkfifo(p, 0o600) },
	} {
		w := newWorld(t)
		w.seed()
		dir := w.store.store.Session(mustSession(t, "s1"))
		writeFile(t, w.home+"/elsewhere/x", "")
		if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := plant(w, dir); err != nil {
			t.Fatal(err)
		}
		if _, err := w.tryTake("s1", StateActive); !errors.Is(err, ErrHeld) || !strings.Contains(err.Error(), w.sessionSeat("s1").Dir()) {
			t.Errorf("%s: a take into the session: %v", name, err)
		}
		if exists(t, w.pool().Held()) {
			t.Errorf("%s: held/ was made for a take that was refused", name)
		}
	}
}

// SX1, SX2, n2c-D12, the n2c report's open low: seat A's take for s1 died before step 5, then
// seat B's take for s1 made the session's seat directory. A leave of A neither waits on B's
// live keeper nor moves B's copy, and A's holding is released.
func TestALeaveOfAHoldingWithNoCopyLeavesAnotherSeatsCopy(t *testing.T) {
	w := newWorld(t)
	w.seed()
	seatB := w.seedSeatB(t)
	w.diedBeforeCopy(w.seat)
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
	w.diedBeforeCopy(w.seat)
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
	w.diedBeforeCopy(seatB)
	w.sleep(time.Second)
	takenA := w.take("s1")
	if err := w.release("s1", true); err != nil {
		t.Fatalf("the leave of A: %v", err)
	}
	if exists(t, takenA.Dir) || !exists(t, w.releasedSession(takenA)) {
		t.Errorf("A's own copy was not moved to its quarantine")
	}
}

// SD3, n2c-D12: the crashed-session path for a holding with no copy announces nothing; the
// session seat directory is another holding's, whose own Announce posts its run, once.
func TestAnnounceOfAHoldingWithNoCopyIsTheOthers(t *testing.T) {
	w := newWorld(t)
	w.seed()
	seatB := w.seedSeatB(t)
	w.diedBeforeCopy(w.seat)
	w.sleep(time.Second)
	if _, err := w.takeSeat(seatB, "s1"); err != nil {
		t.Fatalf("take of B: %v", err)
	}
	w.keep("s1", lockRecord)()
	w.sessions.set("s1", true)
	for _, seat := range []Seat{w.seat, seatB} {
		if err := w.store.Announce(t.Context(), seat, mustSession(t, "s1")); err != nil {
			t.Fatalf("announce of %s: %v", seat.Name, err)
		}
	}
	if calls := w.notices.calls(); len(calls) != 1 || calls[0].Bundle != w.poolOf(seatB).Held() {
		t.Errorf("want one notice, from B: %v", calls)
	}
}

// ST2, n2c-D12: since_ms orders the holdings of a session and team strictly, whatever the wall
// clock does: a take in the same millisecond, or after the clock stepped back, is still newer, so
// A's leave neither waits on B's keeper nor moves B's copy.
func TestSinceOrdersTheHoldingsOfASessionStrictly(t *testing.T) {
	for name, step := range map[string]time.Duration{"the same millisecond": 0, "a clock stepped back": -time.Hour} {
		w := newWorld(t)
		w.seed()
		seatB := w.seedSeatB(t)
		w.diedBeforeCopy(w.seat)
		w.clock.mu.Lock()
		w.clock.now = w.clock.now.Add(step)
		w.clock.mu.Unlock()
		takenB, err := w.takeSeat(seatB, "s1")
		if err != nil {
			t.Fatalf("%s: take of B: %v", name, err)
		}
		a, b := w.heldOf(w.seat), w.heldOf(seatB)
		if b.SinceMS <= a.SinceMS {
			t.Errorf("%s: B's since_ms %d is not after A's %d", name, b.SinceMS, a.SinceMS)
		}
		end := w.keep("s1", lockRecord)
		w.sessions.set("s1", true)
		before := w.clock.Now()
		if err := w.release("s1", true); err != nil {
			t.Fatalf("%s: the leave of A: %v", name, err)
		}
		end()
		if waited := w.clock.Now().Sub(before); waited != 0 || !exists(t, takenB.Dir+"/token") {
			t.Errorf("%s: the leave of A waited %v, or moved B's copy", name, waited)
		}
	}
}

// heldOf is seat's held.json.
func (w *world) heldOf(seat Seat) HeldRecord {
	w.t.Helper()
	rec, err := decodeHeldRecord([]byte(readFile(w.t, w.poolOf(seat).HeldRecord())))
	if err != nil {
		w.t.Fatal(err)
	}
	return rec
}

// SX2, SX3, n2c-D12: in the other order, B's release moves its own copy and announces its run,
// and A's release after it announces nothing.
func TestTheNewerHoldingReleasedFirstMovesItsCopy(t *testing.T) {
	w := newWorld(t)
	w.seed()
	seatB := w.seedSeatB(t)
	w.diedBeforeCopy(w.seat)
	takenB, err := w.takeSeat(seatB, "s1")
	if err != nil {
		t.Fatalf("take of B: %v", err)
	}
	w.keep("s1", lockRecord)()
	if err := w.releaseSeat(seatB, "s1", false); err != nil {
		t.Fatalf("the release of B: %v", err)
	}
	if exists(t, takenB.Dir) || len(w.notices.calls()) != 1 {
		t.Fatalf("B's release did not move its copy and announce: %v", w.notices.calls())
	}
	if err := w.release("s1", false); err != nil {
		t.Fatalf("the release of A: %v", err)
	}
	if len(w.notices.calls()) != 1 || exists(t, w.pool().Held()) {
		t.Errorf("A's release announced again, or kept its holding: %v", w.notices.calls())
	}
}

// ST1, SX2, n2c-D12: a take that releases an ended holding with no copy itself, under its own
// lock, leaves the copy a newer holding of the same session has.
func TestATakeReleasingAnEndedHoldingLeavesAnotherSeatsCopy(t *testing.T) {
	w := newWorld(t)
	w.seed()
	seatB := w.seedSeatB(t)
	w.diedBeforeCopy(w.seat)
	w.sleep(time.Second)
	takenB, err := w.takeSeat(seatB, "s1")
	if err != nil {
		t.Fatalf("take of B: %v", err)
	}
	if _, err := w.tryTake("s2", StateActive); err != nil {
		t.Fatalf("a take of A by s2, s1 over: %v", err)
	}
	if !exists(t, takenB.Dir+"/token") {
		t.Errorf("the take that released A's ended holding moved B's copy")
	}
}

// SX2, n2c-D12: the pool is the owner's, so a name in the team's directory the store never makes
// stops a release before step 4, naming it, and the owner mends it; the token is not retired yet.
func TestAStrangePoolNameStopsTheRelease(t *testing.T) {
	w := newWorld(t)
	w.seed()
	w.take("s1")
	token := readFile(t, w.pool().HeldToken())
	stray := w.store.store.PoolTeam(w.key, w.seat.Team) + "/NOT A SEAT"
	writeFile(t, stray, "")
	if err := w.release("s1", true); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), stray) {
		t.Fatalf("a release with a strange pool name: %v", err)
	}
	if !w.alive(token) || !exists(t, w.pool().Held()) {
		t.Errorf("the release went past step 3")
	}
}

// ST1, n2c-D12: a FIFO where the session directory was is never opened by the take.
func TestATakeNeverOpensAFIFOSessionDirectory(t *testing.T) {
	w := newWorld(t)
	w.seed()
	dir := w.store.store.Session(mustSession(t, "s1"))
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(dir, 0o600); err != nil {
		t.Fatal(err)
	}
	watch, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := unix.Close(watch); err != nil {
			t.Error(err)
		}
	}()
	if _, err := unix.InotifyAddWatch(watch, dir, unix.IN_OPEN); err != nil {
		t.Fatal(err)
	}
	if _, err := w.tryTake("s1", StateActive); !errors.Is(err, ErrHeld) {
		t.Errorf("a take into a FIFO session directory: %v", err)
	}
	var events [unix.SizeofInotifyEvent + unix.PathMax]byte
	if n, err := unix.Read(watch, events[:]); !errors.Is(err, unix.EAGAIN) {
		t.Errorf("the FIFO was opened: %d bytes of events, %v", n, err)
	}
}
