package teamsync

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
)

// owedBuild is a batch whose questions key is a build owed (QX5): no state, token or last event.
func owedBuild(b team.Batch) bool {
	k := b.Questions
	return k != nil && k.Build != nil && k.Build.Start == nil && k.Index == nil && k.Token == nil && k.Last == nil
}

// QX7 (n3f-D21): an admin redaction in questions that the sync folds writes the key as a build
// owed, `after` the answer's last questions event and `late_from` the index's next number, and
// begins the build with that key; the room is out of the filter until the build is at the head.
func TestQX7ARedactionTheSyncFoldsOwesARebuild(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(alice, false)
	rt := w.readyWith(w.main, nil)
	post := w.sayIn(w.qroom(), alice, map[string]any{"msgtype": "m.text", "body": "the key is hunter2"})
	w.batchWith(lastIs(post))
	jobs := len(rt.fb.jobs())
	redaction, err := w.admin().Redact(t.Context(), w.qroom(), post, "", "secret")
	check(t, err)
	b := w.batchWith(owedBuild)
	rec := b.Questions.Build
	if rec.After == nil || *rec.After != redaction {
		t.Errorf("the owed build's after %v, want the redaction %s", rec.After, redaction)
	}
	if rec.LateFrom == nil || *rec.LateFrom != 1 {
		t.Errorf("the owed build's late_from %v, want 1", rec.LateFrom)
	}
	got := rt.fb.jobs()[jobs:]
	if len(got) != 1 || got[0].Key == nil || got[0].Key.Build == nil || *got[0].Key.Build.After != redaction {
		t.Fatalf("jobs begun after the redaction %+v, want one build with the owed key", got)
	}
	if st := rt.tm.QuestionsStatus(); st.State != team.QuestionsBuilding {
		t.Errorf("status %+v after the redaction, want building", st)
	}
}

// QX7: a redaction the build's forward read meets after the build's start begins the build again,
// and its result equals a build begun after the redaction: the redacted post is in no attachment.
// A redaction already rebuilt for is history to the build that follows it, so it ends.
func TestQX7ARedactionMetAfterTheBuildsStartBuildsAgain(t *testing.T) {
	w := newWorld(t)
	posts, events := w.historyRoom(3)
	g := w.gateQuestions()
	tm := w.newTeam(withIndexBuilder())
	b := watchBuilds(tm)
	w.startTeam(tm)
	// The backward read: one page of events, then the room's start.
	for range 2 {
		check(t, g.Held(t.Context()))
		g.Pass()
	}
	check(t, g.Held(t.Context()))
	if _, err := w.admin().Redact(t.Context(), w.qroom(), posts[1], "", "secret"); err != nil {
		t.Fatal(err)
	}
	g.Open()
	b.ended(t)
	if back := readsIn(w.teamReads(w.qroom()), "b"); back != 4 {
		t.Errorf("%d backward reads over %d events, want 4: the build was not begun again", back, events)
	}
	idx := w.atTheHead("")
	if _, ok := idx.Attachment(posts[1]); ok {
		t.Error("the redacted post is still in the index")
	}
	if _, ok := idx.Attachment(posts[2]); !ok {
		t.Error("a post not redacted is missing from the rebuilt index")
	}
}
