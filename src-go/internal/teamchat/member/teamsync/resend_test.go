package teamsync

import (
	"net/http"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
)

// pendingOnce is whether id is pending in the engine's store exactly once.
func (w *world) pendingOnce(id string) bool {
	n := 0
	for _, p := range w.eng.pending() {
		if p.ID == id {
			n++
		}
	}
	return n == 1
}

// batchOrWait is the next batch, advancing the clock through any backoff the team waits first.
func (w *world) batchOrWait(r *running) team.Batch {
	w.t.Helper()
	for {
		select {
		case b := <-w.eng.batches:
			return b
		case d := <-r.waits:
			w.clk.Advance(d)
		case <-w.t.Context().Done():
			w.t.Fatal("no batch")
		}
	}
}

// SY7 case 1: an answer lost after the server handled it is asked again from the same since;
// its item is pending once and its position written once.
func TestALostAnswerIsAskedAgain(t *testing.T) {
	w := newWorld(t)
	r, release := w.held(w.newTeam())
	since := w.eng.position()
	w.srv.Inject(fakehs.Fault{Method: http.MethodGet, Path: "/sync", DropAfter: true})
	id := w.say(alice, human("once", user(me)))
	release()
	b := w.batchOrWait(r)
	if got := itemIDs(b); len(got) != 1 || got[0] != id || !w.pendingOnce(id) {
		t.Errorf("items %v, pending %+v", got, w.eng.pending())
	}
	if n := countOf(w.syncs(), since); n != 2 {
		t.Errorf("%d syncs from %q, want 2", n, since)
	}
}

func countOf(ss []string, s string) int {
	n := 0
	for _, x := range ss {
		if x == s {
			n++
		}
	}
	return n
}

// SY7 case 2: connections dropped while idle between polls (here, during a store write): the
// next answer is checked and persisted as any other.
func TestARestartedServerBetweenPolls(t *testing.T) {
	w := newWorld(t)
	writes := 0
	w.eng.setPersistErr(func(team.Batch) error {
		if writes++; writes == 2 {
			check(t, w.srv.Restart())
		}
		return nil
	})
	r := w.start()
	w.batch()
	r.poll(t)
	w.say(alice, human("before the restart", user(me)))
	w.batch()
	id := w.say(alice, human("after the restart", user(me)))
	if got := itemIDs(w.batchOrWait(r)); len(got) != 1 || got[0] != id {
		t.Errorf("items %v", got)
	}
}

// SY7 case 6: the epoch read failing transiently persists nothing and sends no long-poll.
func TestAFailedEpochReadHoldsTheStart(t *testing.T) {
	w := newWorld(t)
	w.srv.Inject(fakehs.Fault{Method: http.MethodGet, Path: "/state/teamchat.roster", Status: http.StatusBadGateway, Body: []byte(`{}`)})
	r := w.start()
	d := r.wait(t)
	w.noBatch()
	if len(r.polls) != 0 {
		t.Error("a long-poll before the epoch was read")
	}
	w.clk.Advance(d)
	w.batch()
	r.poll(t)
}

// SY7 case 3: a restore under an in-flight long-poll: the restored server holds the poll to its
// timeout and answers its own lower position, which SY6 discards; the seat resumes from its
// marker alone, and a post made after the restore is delivered.
func TestARestoreUnderALongPoll(t *testing.T) {
	w := newWorld(t)
	r := w.start()
	w.batch()
	r.poll(t)
	snap := w.srv.Snapshot()
	lost := w.say(alice, human("lost to the restore", user(me)))
	w.batch()
	r.poll(t)
	check(t, w.srv.RestoreTo(snap))
	// fakehs holds the poll to its timeout; the request's own deadline is later and not this
	// test's.
	check(t, w.clk.AwaitDue(t.Context(), longPoll))
	w.clk.Advance(longPoll)
	// The first batch after the restore is SY1 (b)'s under the new epoch: the held poll's answer
	// was not persisted.
	b := w.batchOrWait(r)
	if b.RestoreEpoch != w.roster.RestoreEpoch+1 || slices.Contains(itemIDs(b), lost) {
		t.Errorf("after the restore: %+v", b)
	}
	if resets := w.eng.resetEpochs(); len(resets) != 1 || resets[0] != w.roster.RestoreEpoch+1 {
		t.Errorf("resets %v", resets)
	}
	r.poll(t)
	id := w.say(alice, human("after the restore", user(me)))
	if got := itemIDs(w.batch()); len(got) != 1 || got[0] != id {
		t.Errorf("items %v", got)
	}
}

// SY7 case 8: a 429 to a long-poll and a restore before its retry: the retry's answer is
// checked like any other and discarded. The 429 asks for no wait, so the retry follows at once;
// the restore is made before the first try, which the fault answers whatever the database.
func TestARestoreBeforeA429Retry(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	r, release := w.held(tm)
	w.srv.Inject(fakehs.Fault{Method: http.MethodGet, Path: "/sync", Status: http.StatusTooManyRequests, Body: []byte(`{"errcode":"M_LIMIT_EXCEEDED","error":"x","retry_after_ms":0}`)})
	check(t, w.srv.RestoreTo(w.srv.Snapshot()))
	release()
	b := w.batchOrWait(r)
	if b.RestoreEpoch != w.roster.RestoreEpoch+1 {
		t.Errorf("after the restore: %+v", b)
	}
	if resets := w.eng.resetEpochs(); len(resets) != 1 {
		t.Errorf("resets %v", resets)
	}
}
