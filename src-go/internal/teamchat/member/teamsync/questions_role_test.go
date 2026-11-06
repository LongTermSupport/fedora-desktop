package teamsync

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

func (b *fakeBuilder) stopped() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stops
}

// QS9: the roster naming another member the coordinator is a demotion: the build is stopped,
// the answer's one write drops the questions room (DropQuestions, never with the key), and the
// room's status is cleared.
func TestQS9ADemotionDropsTheRoomInTheAnswersWrite(t *testing.T) {
	w := newWorld(t)
	w.asCoordinator()
	fb := &fakeBuilder{}
	tm := w.newTeam(withBuilder(fb))
	r := w.startTeam(tm)
	w.batch()
	r.poll(t)
	w.promote(peer)
	b := w.batchWith(func(b team.Batch) bool { return b.DropQuestions })
	if b.Questions != nil {
		t.Errorf("the drop's write also holds the key %+v", b.Questions)
	}
	if fb.stopped() == 0 {
		t.Error("the build was not stopped")
	}
	if st := tm.QuestionsStatus(); st != (team.QuestionsHealth{}) {
		t.Errorf("status %+v after the demotion, want none", st)
	}
}

// QS9: a member that does not coordinate the room, starting over a store that still holds the
// room (a demotion while the link was down), drops it in its first write.
func TestQS9AStartOverAStaleStoreDropsTheRoom(t *testing.T) {
	w := newWorld(t)
	idx := proto.NewQuestionIndex(server, 0).State()
	_, err := w.eng.Load(w.team, user(me))
	check(t, err)
	w.eng.withStore(func(s *state.Store) { s.Questions = &proto.QuestionsState{Index: &idx} })
	w.start()
	if b := w.batch(); !b.DropQuestions {
		t.Errorf("first write %+v: the stale room is not dropped", b)
	}
}

// QS10: a worker promoted while running joins the room by the roster's id and starts the
// build of an index it has none of.
func TestQS10APromotionJoinsAndBuilds(t *testing.T) {
	w := newWorld(t)
	fb := &fakeBuilder{}
	tm := w.newTeam(withBuilder(fb))
	r := w.startTeam(tm)
	w.batch()
	r.poll(t)
	w.invited(me)
	w.promote(me)
	r.poll(t)
	if n := w.joinsOf(w.qroom()); n != 1 {
		t.Errorf("%d joins of the questions room, want 1", n)
	}
	if jobs := fb.jobs(); len(jobs) != 1 || jobs[0].Key != nil {
		t.Errorf("builder jobs %+v, want one build from the room's start", jobs)
	}
	if st := tm.QuestionsStatus(); st.State != team.QuestionsBuilding {
		t.Errorf("status %+v, want building", st)
	}
}
