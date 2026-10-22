package seat

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

const (
	testRun    = "0123456789abcdef"
	lockRecord = `{"v":1,"run":"` + testRun + `","started_ms":1}`
)

// releasedPool is where a release moves the world's held/ for a claim.
func (w *world) releasedPool(taken Taken) string {
	return w.store.store.ReleasedPool(w.key, w.seat.Team, w.seat.Name, taken.Claim)
}

// releasedSession is where a release moves session s1's seat directory for a claim.
func (w *world) releasedSession(taken Taken) string {
	return w.store.store.ReleasedSession(mustSession(w.t, "s1"), w.seat.Team, taken.Claim).Dir()
}

// SX2, SX4, SX5: a leave moves the session seat and held/ to quarantine, logs the token out,
// records the release, and leaves the pool record as it was. No notice is asked.
func TestReleaseOfALeave(t *testing.T) {
	w := newWorld(t)
	w.seed()
	poolBundle, poolPassword := readFile(t, w.pool().Bundle()), readFile(t, w.pool().Password())
	taken := w.take("s1")
	token := readFile(t, taken.Dir+"/token")
	w.keep("s1", lockRecord)()
	if err := w.release("s1", true); err != nil {
		t.Fatal(err)
	}
	pool, sess := w.releasedPool(taken), w.releasedSession(taken)
	if exists(t, taken.Dir) || exists(t, w.pool().Held()) || !exists(t, sess+"/seat.json") || !exists(t, pool+"/held.json") {
		t.Errorf("the release did not move the seat to quarantine")
	}
	if exists(t, pool+"/token") || w.alive(token) {
		t.Errorf("the held token survived the release")
	}
	rec, err := decodeReleasedRecord([]byte(readFile(t, w.pool().ReleasedRecord())))
	if err != nil || rec.Claim != taken.Claim || rec.ReleasedMS != w.clock.Now().UnixMilli() {
		t.Errorf("released.json %+v, %v", rec, err)
	}
	if readFile(t, w.pool().Bundle()) != poolBundle || readFile(t, w.pool().Password()) != poolPassword {
		t.Errorf("the release changed the pool record")
	}
	if len(w.notices.calls()) != 0 {
		t.Errorf("a leave asked for a notice: %v", w.notices.calls())
	}
	if err := w.release("s1", true); err != nil {
		t.Errorf("a second release: %v", err)
	}
}

// SX3, SD1: a session that is over has its notice asked with held/'s inputs, and recorded.
func TestReleaseOfAnEndedSessionAnnounces(t *testing.T) {
	w := newWorld(t)
	w.seed()
	taken := w.take("s1")
	w.keep("s1", lockRecord)()
	if err := w.release("s1", false); err != nil {
		t.Fatal(err)
	}
	calls := w.notices.calls()
	if len(calls) != 1 || calls[0].Bundle != w.pool().Held() || calls[0].SinceMS != testStart.UnixMilli() || calls[0].Run.String() != testRun {
		t.Errorf("notices %+v", calls)
	}
	rec, err := decodeNoticeRecord([]byte(readFile(t, w.releasedPool(taken)+"/notice.json")))
	if err != nil || !rec.Posted || rec.Run.String() != testRun || rec.Claim != taken.Claim {
		t.Errorf("notice.json %+v, %v", rec, err)
	}
}

// SX3, SD4: a transient notice failure keeps held/; the retry reads the run from the
// quarantined seat lock and records the outcome.
func TestReleaseRetriesATransientNotice(t *testing.T) {
	w := newWorld(t)
	w.seed()
	taken := w.take("s1")
	w.keep("s1", lockRecord)()
	w.notices.answer(Outcome{}, errors.New("unreachable"))
	if err := w.release("s1", false); err == nil {
		t.Fatal("a transient notice failure completed the release")
	}
	if !exists(t, w.pool().HeldToken()) || exists(t, taken.Dir) || !exists(t, w.releasedSession(taken)) {
		t.Errorf("after the failed notice: held/ kept, session seat in quarantine")
	}
	w.notices.answer(Outcome{Posted: false, Why: "token refused"}, nil)
	if err := w.release("s1", false); err != nil {
		t.Fatal(err)
	}
	calls := w.notices.calls()
	if len(calls) != 2 || calls[1].Run.String() != testRun {
		t.Errorf("notices %+v", calls)
	}
	rec, err := decodeNoticeRecord([]byte(readFile(t, w.releasedPool(taken)+"/notice.json")))
	if err != nil || rec.Posted || rec.Why != "token refused" {
		t.Errorf("notice.json %+v, %v", rec, err)
	}
}

// SD1: a lock that names no run, or a run whose launch failed, asks no notice.
func TestReleaseSkipsANoticeWithNoRun(t *testing.T) {
	for name, content := range map[string]*string{
		"missing":       nil,
		"empty":         new(""),
		"undecodable":   new(`{"v":1}`),
		"bad run":       new(`{"v":1,"run":"X","started_ms":1}`),
		"case-folded":   new(`{"v":2,"run":"` + testRun + `","started_ms":1,"V":1}`),
		"launch failed": new(`{"v":1,"run":"` + testRun + `","started_ms":1,"launch_failed_ms":2}`),
	} {
		w := newWorld(t)
		w.seed()
		w.take("s1")
		if content != nil {
			w.keep("s1", *content)()
		}
		if err := w.release("s1", false); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if len(w.notices.calls()) != 0 {
			t.Errorf("%s: asked %v", name, w.notices.calls())
		}
	}
}

// SX1, n2c-D14: a held seat lock is waited for, bounded; at the bound the release goes on and
// the token is retired, as a keeper that never lets go must not keep it live.
func TestReleaseWaitsForTheSeatLock(t *testing.T) {
	w := newWorld(t)
	w.seed()
	taken := w.take("s1")
	end := w.keep("s1", lockRecord)
	w.sessions.set("s1", true)
	token := readFile(t, w.pool().HeldToken())
	before := w.clock.Now()
	if err := w.release("s1", true); err != nil {
		t.Fatalf("a held seat lock: %v", err)
	}
	if waited := w.clock.Now().Sub(before); waited < releaseWait || waited > releaseWait+allocPoll {
		t.Errorf("waited %v", waited)
	}
	w.revoked(taken, token)
	end()
}

// SX1: a session that is over is not waited for, its lock still held.
func TestReleaseOfAnEndedSessionDoesNotWait(t *testing.T) {
	w := newWorld(t)
	w.seed()
	taken := w.take("s1")
	token := readFile(t, w.pool().HeldToken())
	w.keep("s1", lockRecord)
	before := w.clock.Now()
	if err := w.release("s1", true); err != nil {
		t.Fatalf("a session that is over, its lock still held: %v", err)
	}
	if waited := w.clock.Now().Sub(before); waited != 0 {
		t.Errorf("waited %v", waited)
	}
	w.revoked(taken, token)
}

// SX4: a transient logout failure stops the release with held/ kept; the retry completes.
func TestReleaseRetriesAFailedLogout(t *testing.T) {
	w := newWorld(t)
	w.seed()
	taken := w.take("s1")
	token := readFile(t, w.pool().HeldToken())
	w.hs.Inject(fakehs.Fault{Method: http.MethodPost, Path: "/logout", Status: http.StatusBadGateway})
	if err := w.release("s1", true); err == nil {
		t.Fatal("a failed logout completed the release")
	}
	if !exists(t, w.pool().HeldToken()) || !w.alive(token) {
		t.Errorf("held/ lost, or the token dead, after a failed logout")
	}
	if err := w.release("s1", true); err != nil {
		t.Fatal(err)
	}
	if w.alive(token) || !exists(t, w.releasedPool(taken)+"/held.json") {
		t.Errorf("the retried release did not complete")
	}
}

// SX6: a release of a seat with no held/ is done; one held by another session is refused; a
// held/ with no held.json goes to quarantine.
func TestReleaseOfNothingAndOfAnother(t *testing.T) {
	w := newWorld(t)
	w.seed()
	if err := w.release("s1", true); err != nil {
		t.Errorf("no held/: %v", err)
	}
	w.take("s1")
	if err := w.release("s2", true); !errors.Is(err, ErrHeld) {
		t.Errorf("another session's seat: %v", err)
	}
	if err := w.release("s1", true); err != nil {
		t.Fatal(err)
	}
	writeFile(t, w.pool().HeldToken(), "dead")
	if err := w.release("s1", true); err != nil {
		t.Errorf("a broken held/: %v", err)
	}
	if exists(t, w.pool().Held()) {
		t.Errorf("a broken held/ was kept")
	}
}

// SX1, SX4, SL1: a release stops, held/ kept, on a session record it cannot read, a held token
// that is not a credential, or an allocation lock another holds; the retry completes.
func TestReleaseStopsOnWhatItCannotRead(t *testing.T) {
	w := newWorld(t)
	w.seed()
	taken := w.take("s1")
	end := w.keep("s1", lockRecord)
	w.sessions.fail(errors.New("unreadable record"))
	if err := w.release("s1", true); err == nil || errors.Is(err, ErrBusy) {
		t.Errorf("an unreadable session record: %v", err)
	}
	w.sessions.fail(nil)
	end()
	token := readFile(t, w.pool().HeldToken())
	if err := os.WriteFile(w.pool().HeldToken(), []byte("two words"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := w.release("s1", true); !errors.Is(err, ErrInvalid) {
		t.Errorf("a held token that is not a credential: %v", err)
	}
	if !exists(t, w.pool().Held()) || !w.alive(token) {
		t.Errorf("held/ lost, or the token ended, after a refused retire")
	}
	if err := os.WriteFile(w.pool().HeldToken(), []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}
	a := w.lock()
	if err := w.release("s1", true); !errors.Is(err, ErrBusy) {
		t.Errorf("an allocation lock another holds: %v", err)
	}
	w.unlock(a)
	if err := w.release("s1", true); err != nil {
		t.Fatal(err)
	}
	if exists(t, w.pool().Held()) || w.alive(token) || !exists(t, w.releasedPool(taken)+"/held.json") {
		t.Errorf("the retried release did not complete")
	}
}

// SD2, SD3: Announce for a seat another session holds is ErrHeld, and a notice.json that is
// not a regular file stops it before the announcer is asked.
func TestAnnounceRefusals(t *testing.T) {
	w := newWorld(t)
	w.seed()
	w.take("s1")
	w.keep("s1", lockRecord)()
	if err := w.store.Announce(t.Context(), w.seat, mustSession(t, "s2")); !errors.Is(err, ErrHeld) {
		t.Errorf("another session's seat: %v", err)
	}
	if err := os.Mkdir(w.pool().HeldNotice(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := w.store.Announce(t.Context(), w.seat, mustSession(t, "s1")); err == nil {
		t.Errorf("a notice.json that is a directory was taken")
	}
	if len(w.notices.calls()) != 0 {
		t.Errorf("the announcer was asked: %v", w.notices.calls())
	}
}

// hookAnnouncer runs hook on its first notice, then answers posted.
type hookAnnouncer struct {
	fired bool
	hook  func()
}

func (h *hookAnnouncer) Disconnected(context.Context, Notice) (Outcome, error) {
	if !h.fired {
		h.fired = true
		h.hook()
	}
	return Outcome{Posted: true}, nil
}

// SX3-SX5: a release outside the lock at step 3, while a take for another session releases the
// same ended holding itself and takes the seat, touches nothing of the new holding: its token
// stays live, no notice is written into its held/, and the first release is done.
func TestReleaseMeetsATakeThatReleasedItFirst(t *testing.T) {
	w := newWorld(t)
	w.seed()
	w.take("s1")
	w.keep("s1", lockRecord)()
	var next Taken
	w.store.announcer = &hookAnnouncer{hook: func() { next = w.take("s2") }}
	if err := w.release("s1", false); err != nil {
		t.Fatalf("the first release: %v", err)
	}
	token := readFile(t, w.pool().HeldToken())
	rec, err := decodeHeldRecord([]byte(readFile(t, w.pool().HeldRecord())))
	if err != nil || rec.Claim != next.Claim || !w.alive(token) || !exists(t, next.Dir) {
		t.Errorf("the next holding was touched: %+v, %v, token alive %v", rec, err, w.alive(token))
	}
	if exists(t, w.pool().HeldNotice()) {
		t.Errorf("a notice was written into the next holder's held/")
	}
}

// SX5, SX6: two releases of one holding at once (the launcher's and the seat sweep's) both
// succeed; the one that finds held/ gone at step 5 is done, and the holding is released once.
func TestTwoReleasesOfOneHolding(t *testing.T) {
	w := newWorld(t)
	w.seed()
	taken := w.take("s1")
	w.keep("s1", lockRecord)()
	w.store.announcer = &hookAnnouncer{hook: func() {
		if err := w.release("s1", false); err != nil {
			t.Errorf("the second release: %v", err)
		}
	}}
	if err := w.release("s1", false); err != nil {
		t.Fatalf("the first release: %v", err)
	}
	rec, err := decodeReleasedRecord([]byte(readFile(t, w.pool().ReleasedRecord())))
	if err != nil || rec.Claim != taken.Claim || exists(t, w.pool().Held()) || len(quarantined(t, w)) != 1 {
		t.Errorf("released %+v, %v; quarantine %v", rec, err, quarantined(t, w))
	}
}

// SL1, SX6: an incomplete held/ met while another holds the allocation lock is ErrBusy, its
// fd closed once.
func TestAnIncompleteHeldWithTheLockBusy(t *testing.T) {
	w := newWorld(t)
	w.seed()
	if err := os.Mkdir(w.pool().Held(), 0o700); err != nil {
		t.Fatal(err)
	}
	a := w.lock()
	defer w.unlock(a)
	err := w.store.Announce(t.Context(), w.seat, mustSession(t, "s1"))
	if !errors.Is(err, ErrBusy) || errors.Is(err, unix.EBADF) {
		t.Errorf("announce: %v", err)
	}
}

// SX1, SX2, n2c-D12: a release resumed after step 2, once its session has taken another seat of
// the same team into the same session seat directory, leaves that directory alone and completes.
func TestAResumedReleaseLeavesAnotherSeatsCopy(t *testing.T) {
	w := newWorld(t)
	w.seed()
	seatB := w.seedSeatB(t)
	w.take("s1")
	w.hs.Inject(fakehs.Fault{Method: http.MethodPost, Path: "/logout", Status: http.StatusBadGateway})
	if stopped := w.release("s1", true); stopped == nil {
		t.Fatal("the release of A did not stop at step 4")
	}
	a := w.lock()
	takenB, err := w.store.Take(t.Context(), a, Take{Seat: seatB, Session: mustSession(t, "s1"), State: StateActive})
	w.unlock(a)
	if err != nil {
		t.Fatalf("take of B: %v", err)
	}
	end := w.keep("s1", lockRecord)
	defer end()
	if err := w.release("s1", true); err != nil {
		t.Fatalf("the resumed release of A: %v", err)
	}
	if !exists(t, takenB.Dir+"/token") || exists(t, w.pool().Held()) {
		t.Errorf("B's session copy was moved, or A's holding kept")
	}
}

// seedSeatB is a second complete pool seat of the world's team, for a session taking two.
func (w *world) seedSeatB(t *testing.T) Seat {
	t.Helper()
	const handleB = "repo.2+site.podman"
	w.hs.AddUser(handleB, testPassword, false)
	seatB := Seat{Team: w.seat.Team, Name: mustSeatName(t, handleB)}
	poolB := w.store.store.PoolSeat(w.key, seatB.Team, seatB.Name)
	bundleB := w.bundle()
	bundleB.UserID = "@" + handleB + ":" + paths.ServerName(w.seat.Team)
	data, err := bundleB.Encode()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, poolB.Password(), testPassword)
	writeFile(t, poolB.Bundle(), string(data))
	return seatB
}

// SX1, SX2, C41, n2c-D12: whether steps 1 and 2 are done is judged from host-owned state, never
// from the session's copy, which its container may rewrite: a forged claim in the copy does not
// skip the wait for a live keeper.
func TestAForgedCopyClaimDoesNotSkipTheWait(t *testing.T) {
	w := newWorld(t)
	w.seed()
	taken := w.take("s3")
	end := w.keep("s3", lockRecord)
	defer end()
	w.sessions.set("s3", true)
	b, err := DecodeHeldBundle([]byte(readFile(t, taken.Dir+"/seat.json")))
	if err != nil {
		t.Fatal(err)
	}
	if b.Claim, err = newClaim(); err != nil {
		t.Fatal(err)
	}
	forged, err := b.Encode()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, taken.Dir+"/seat.json", string(forged))
	token := readFile(t, w.pool().HeldToken())
	before := w.clock.Now()
	if err := w.release("s3", true); err != nil {
		t.Errorf("a leave with the keeper live: %v", err)
	}
	if waited := w.clock.Now().Sub(before); waited < releaseWait {
		t.Errorf("the wait for the live keeper was skipped: waited %v", waited)
	}
	w.revoked(taken, token)
}

// SX2, C41: the copy's seat.json is never read, so a link put there by the container stops
// neither the release nor a later take by another session.
func TestALinkedCopyDoesNotStopTheRelease(t *testing.T) {
	w := newWorld(t)
	w.seed()
	taken := w.take("s1")
	if err := os.Remove(taken.Dir + "/seat.json"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(w.pool().Password(), taken.Dir+"/seat.json"); err != nil {
		t.Fatal(err)
	}
	if err := w.release("s1", false); err != nil {
		t.Errorf("the release of a session that is over: %v", err)
	}
	if _, err := w.tryTake("s2", StateActive); err != nil {
		t.Errorf("a take by another session: %v", err)
	}
}

// SX2: a session seat directory present at step 1 and gone by step 2 (another release of the
// holding moved it) is already gone, and the release goes on.
func TestADirectoryGoneByStepTwoGoesOn(t *testing.T) {
	w := newWorld(t)
	w.seed()
	taken := w.take("s3")
	end := w.keep("s3", lockRecord)
	w.sessions.set("s3", true)
	g := &gateClock{fakeClock: w.clock, waiting: make(chan struct{}), goOn: make(chan struct{})}
	w.store.clock = g
	done := make(chan error)
	go func() {
		done <- w.store.Release(context.Background(), Release{Seat: w.seat, Session: mustSession(t, "s3"), Leaving: true})
	}()
	<-g.waiting
	end()
	if err := os.Rename(taken.Dir, w.home+"/moved-away"); err != nil {
		t.Fatal(err)
	}
	close(g.goOn)
	if err := <-done; err != nil {
		t.Errorf("release: %v", err)
	}
	if exists(t, w.pool().Held()) {
		t.Errorf("the holding was kept")
	}
}

// SX1, SX2, n2c-D12: a session seat directory gone at step 1 counts as gone at step 2, so a
// directory that a take of another seat of the team made between them, under the lock, is left
// in place.
func TestADirectoryMadeAfterStepOneIsNotMoved(t *testing.T) {
	w := newWorld(t)
	w.seed()
	seatB := w.seedSeatB(t)
	takenA := w.take("s1")
	if err := os.RemoveAll(takenA.Dir); err != nil {
		t.Fatal(err)
	}
	a := w.lock()
	g := &gateClock{fakeClock: w.clock, waiting: make(chan struct{}), goOn: make(chan struct{})}
	w.store.clock = g
	done := make(chan error)
	go func() {
		done <- w.store.Release(context.Background(), Release{Seat: w.seat, Session: mustSession(t, "s1"), Leaving: true})
	}()
	<-g.waiting
	takenB, err := w.store.Take(t.Context(), a, Take{Seat: seatB, Session: mustSession(t, "s1"), State: StateActive})
	w.unlock(a)
	close(g.goOn)
	if err != nil {
		t.Fatalf("take of B: %v", err)
	}
	if err := <-done; err != nil {
		t.Errorf("release of A: %v", err)
	}
	if !exists(t, takenB.Dir+"/token") || exists(t, w.releasedSession(takenA)) {
		t.Errorf("B's live session copy was moved by A's release")
	}
}

// gateClock signals its first sleep on waiting and holds it until goOn is closed.
type gateClock struct {
	*fakeClock

	waiting chan struct{}
	goOn    chan struct{}
	first   bool
}

func (g *gateClock) Sleep(ctx context.Context, d time.Duration) error {
	if !g.first {
		g.first = true
		g.waiting <- struct{}{}
		<-g.goOn
	}
	return g.fakeClock.Sleep(ctx, d)
}

// SX6, ST1: a held/ that looked incomplete outside the lock (a take between mkdirat and
// held.json) is judged again under it; complete by then, it is a holding like any other, here
// another session's, so the release is ErrHeld and the take's holding is untouched.
func TestReleaseJudgesAnIncompleteHeldUnderTheLock(t *testing.T) {
	w := newWorld(t)
	w.seed()
	taken := w.take("s1")
	token := readFile(t, w.pool().HeldToken())
	a := w.lock()
	aside := w.home + "/held.json.aside"
	if err := os.Rename(w.pool().HeldRecord(), aside); err != nil {
		t.Fatal(err)
	}
	g := &gateClock{fakeClock: w.clock, waiting: make(chan struct{}), goOn: make(chan struct{})}
	w.store.clock = g
	session := mustSession(t, "s9")
	done := make(chan error)
	go func() {
		done <- w.store.Release(context.Background(), Release{Seat: w.seat, Session: session, Leaving: true})
	}()
	<-g.waiting
	if err := os.Rename(aside, w.pool().HeldRecord()); err != nil {
		t.Fatal(err)
	}
	w.unlock(a)
	close(g.goOn)
	if err := <-done; !errors.Is(err, ErrHeld) {
		t.Errorf("a release by another session: %v", err)
	}
	if !exists(t, w.pool().HeldRecord()) || !exists(t, taken.Dir) || !w.alive(token) {
		t.Errorf("the take's holding was moved or its token ended")
	}
}

// n2c-D4: a take that meets a held/ naming a session that is over releases it first, under its
// own allocation lock.
func TestTakeReleasesASeatWhoseSessionIsOver(t *testing.T) {
	w := newWorld(t)
	w.seed()
	first := w.take("s1")
	w.keep("s1", lockRecord)()
	second := w.take("s2")
	if second.Claim == first.Claim || exists(t, first.Dir) {
		t.Errorf("the stale holding was not released")
	}
	if len(w.notices.calls()) != 1 || !exists(t, w.releasedPool(first)+"/notice.json") {
		t.Errorf("the ended session's notice: %v", w.notices.calls())
	}
}

// SD2, SD3: the crashed-session path announces a free seat once and keeps it held; the later
// release does not ask again.
func TestAnnounceKeepsTheSeatHeld(t *testing.T) {
	w := newWorld(t)
	w.seed()
	taken := w.take("s1")
	end := w.keep("s1", lockRecord)
	if err := w.store.Announce(t.Context(), w.seat, mustSession(t, "s1")); err != nil || len(w.notices.calls()) != 0 {
		t.Errorf("a held lock was announced: %v, %v", err, w.notices.calls())
	}
	end()
	for range 2 {
		if err := w.store.Announce(t.Context(), w.seat, mustSession(t, "s1")); err != nil {
			t.Fatal(err)
		}
	}
	if len(w.notices.calls()) != 1 || !exists(t, w.pool().HeldNotice()) || !exists(t, taken.Dir) {
		t.Errorf("announce: %v", w.notices.calls())
	}
	if err := w.release("s1", false); err != nil {
		t.Fatal(err)
	}
	if len(w.notices.calls()) != 1 {
		t.Errorf("the release asked again: %v", w.notices.calls())
	}
	if err := w.store.Announce(t.Context(), w.seat, mustSession(t, "s1")); err != nil {
		t.Errorf("announcing a released seat: %v", err)
	}
	w.notices.answer(Outcome{}, nil)
	w.take("s2")
	w.keep("s2", `{"v":1,"run":"1123456789abcdef","started_ms":1}`)()
	if err := w.store.Announce(t.Context(), w.seat, mustSession(t, "s2")); !errors.Is(err, ErrInvalid) {
		t.Errorf("an outcome neither posted nor with a why: %v", err)
	}
}
