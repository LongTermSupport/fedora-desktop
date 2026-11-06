package teamsync

import (
	"errors"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
)

// backedOff waits for the team's next backoff, failing fast when the team
// reaches a long-poll first (the failure was not taken as one).
func (r *running) backedOff(t *testing.T) {
	t.Helper()
	select {
	case <-r.waits:
	case since := <-r.polls:
		t.Fatalf("a long-poll from %q before any backoff: the failed write went on", since)
	case err := <-r.done:
		t.Fatalf("Run ended: %v", err)
	case <-t.Context().Done():
		t.Fatal("no backoff")
	}
}

// QX4's commit rule: an answer whose store write fails leaves the live index unfolded. The
// answer is read again from the stored position, its post delivered, and the re-read's write
// attaches each post once.
func TestQX4AFailedWriteLeavesTheLiveIndexUnfolded(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(alice, false)
	rt := w.readyWith(w.main, nil)
	first := w.sayIn(w.qroom(), alice, human("is the build green?"))
	rt.drainPolls()
	w.batchWith(lastIs(first))
	rt.r.poll(t)

	// The next write that holds a questions item, the second post's, fails once.
	var failed atomic.Bool
	w.eng.setPersistErr(func(b team.Batch) error {
		if len(questionsItems(b)) > 0 && failed.CompareAndSwap(false, true) {
			return errors.New("the disk is full")
		}
		return nil
	})
	w.clk.StepWall(1000)
	second := w.sayIn(w.qroom(), alice, human("and the tests?"))
	rt.r.backedOff(t)
	if !failed.Load() {
		t.Fatal("the backoff was not the failed write's")
	}
	w.clk.Advance(firstBackoff)

	b := w.batchWith(lastIs(second))
	if got := questionsItems(b); !slices.Equal(got, []string{second}) {
		t.Errorf("questions items %v, want the re-read post once", got)
	}
	posts := keyIndex(t, b).Unattached()
	ids := make([]string, 0, len(posts))
	for _, u := range posts {
		ids = append(ids, u.ID)
	}
	if !slices.Equal(ids, []string{first, second}) {
		t.Errorf("unattached %v, want [%s %s]: each post attached once", ids, first, second)
	}
}
