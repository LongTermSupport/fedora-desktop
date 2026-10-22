package seat

import (
	"errors"
	"net/http"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
)

const (
	testRun    = "0123456789abcdef"
	lockRecord = `{"v":1,"run":"` + testRun + `","started_ms":1}`
)

// releasedPool is where a release moves the world's held/ for a claim.
func (w *world) releasedPool(taken Taken) string {
	return w.store.store.ReleasedPool(w.key, w.seat.Team, w.seat.Name, taken.Claim)
}

// releasedSession is where a release moves a session's seat directory for a claim.
func (w *world) releasedSession(session string, taken Taken) string {
	return w.store.store.ReleasedSession(mustSession(w.t, session), w.seat.Team, taken.Claim).Dir()
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
	pool, sess := w.releasedPool(taken), w.releasedSession("s1", taken)
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
	if !exists(t, w.pool().HeldToken()) || exists(t, taken.Dir) || !exists(t, w.releasedSession("s1", taken)) {
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

// SX1: a held seat lock is waited for, bounded, unless the session is over.
func TestReleaseWaitsForTheSeatLock(t *testing.T) {
	w := newWorld(t)
	w.seed()
	taken := w.take("s1")
	end := w.keep("s1", lockRecord)
	w.sessions.set("s1", true)
	before := w.clock.Now()
	if err := w.release("s1", true); !errors.Is(err, ErrBusy) {
		t.Fatalf("a held seat lock: %v", err)
	}
	if waited := w.clock.Now().Sub(before); waited < releaseWait || waited > releaseWait+allocPoll {
		t.Errorf("waited %v", waited)
	}
	if !exists(t, taken.Dir) || !exists(t, w.pool().Held()) {
		t.Errorf("a busy release moved the seat")
	}
	w.sessions.set("s1", false)
	if err := w.release("s1", true); err != nil {
		t.Errorf("a session that is over, its lock still held: %v", err)
	}
	end()
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
