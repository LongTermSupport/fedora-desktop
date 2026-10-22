package teamsync

import (
	"testing"
)

// SY1 (a): a store with a position and the roster's epoch resumes from it, with no step 01.
func TestResumesFromItsPosition(t *testing.T) {
	w := newWorld(t)
	w.eng.preload(user(me), runID(t, linkRun), "", w.roster.RestoreEpoch)
	r := w.start()
	since := w.batch().NextBatch
	if got := r.poll(t); got != since {
		t.Fatalf("since %q", got)
	}
	r.stop()
	<-r.done
	r.done <- nil

	r2 := w.start()
	if got := r2.poll(t); got != since {
		t.Errorf("a resumed team polls from %q, want %q", got, since)
	}
	w.noBatch()
	if n := w.requests("/messages"); n != 0 {
		t.Errorf("a resume read /messages %d times", n)
	}
}

// SY1 (a) into (b): a position of another epoch is dropped, the new epoch stored, and step 01
// runs again.
func TestAnotherEpochDropsThePosition(t *testing.T) {
	w := newWorld(t)
	w.eng.preload(user(me), runID(t, linkRun), "s_stale", w.roster.RestoreEpoch+3)
	w.start()
	b := w.batch()
	if b.NextBatch == "s_stale" || b.Fresh || b.RestoreEpoch != w.roster.RestoreEpoch {
		t.Errorf("batch = %+v", b)
	}
	if resets := w.eng.resetEpochs(); len(resets) != 1 || resets[0] != w.roster.RestoreEpoch {
		t.Errorf("resets = %v", resets)
	}
	for _, s := range w.syncs() {
		if s == "s_stale" {
			t.Error("synced from a position of another epoch")
		}
	}
}

// SY1 (c): a fresh store of a removed account takes no history (timeline limit 0), and back is
// not posted; minor b: an account not removed before runs (b).
func TestReAddTakesNoHistory(t *testing.T) {
	w := newWorld(t)
	w.say(alice, human("before the re-add", user(me)))
	r := w.start(func(c *Config) { c.RemovedUserIDs = []string{user(me)} })
	b := w.batch()
	if len(b.Items) != 0 || b.Fresh || b.NextBatch == "" {
		t.Fatalf("re-add batch = %+v", b)
	}
	r.poll(t)
	if _, backs := w.ends.calls(); backs != 0 {
		t.Error("back after a re-add")
	}

	w2 := newWorld(t)
	w2.say(alice, human("before the start", user(me)))
	w2.start(func(c *Config) { c.RemovedUserIDs = []string{user(peer)} })
	if b := w2.batch(); len(b.Items) != 1 {
		t.Errorf("minor b: %+v", b)
	}
}
