package teamsync

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

const askText = "Which licence for the docs?\nContext: the site goes public.\nQuestion: MIT or CC-BY?\nWhy a human: decision on the licence"

// askPlacement is the placement of the coordinator's question numbered by the index it reads.
func askPlacement(txn string, v team.QuestionsView) team.Placement {
	n := v.Index.Next()
	return team.Placement{Txn: txn, Question: &proto.Question{N: n, Why: "decision"}, Body: proto.QuestionName(int(n)) + " " + askText}
}

// asks composes one question under txn and gives the number it took.
func asks(t *testing.T, tm *Team, txn string) int64 {
	t.Helper()
	var n int64
	_, err := tm.Questions(t.Context(), func(v team.QuestionsView) ([]team.Placement, error) {
		p := askPlacement(txn, v)
		n = p.Question.N
		return []team.Placement{p}, nil
	})
	check(t, err)
	return n
}

// ownQuestion is the coordinator's question content numbered n, as its composer builds it.
func (w *world) ownQuestion(n int64) proto.Value {
	w.t.Helper()
	idx := proto.NewQuestionIndex(server, 0)
	for range n - 1 {
		check(w.t, idx.PlaceQuestion("tcfill"+proto.QuestionName(int(idx.Next())), user(w.self),
			proto.Question{N: idx.Next(), Why: "decision"}, proto.QuestionName(int(idx.Next()))+" "+askText))
	}
	out, err := proto.Composer{Server: server, Self: user(w.self), Token: w.tokens[w.self], Roster: w.readRoster(), Questions: idx}.
		Question(proto.QuestionPost{Text: askText})
	check(w.t, err)
	return out.Content
}

// QX4: composes are one at a time per team: a second compose numbers above the first one's
// placed question, and never runs while the first is inside its compose.
func TestQX4ComposesAreOneAtATimeAndNumberAboveThePlacedOne(t *testing.T) {
	w := newWorld(t)
	rt := w.readyWith(w.main, nil)
	var inside atomic.Bool
	entered, release := make(chan struct{}), make(chan struct{})
	first := make(chan int64, 1)
	go func() {
		var n int64
		_, err := rt.tm.Questions(t.Context(), func(v team.QuestionsView) ([]team.Placement, error) {
			inside.Store(true)
			close(entered)
			<-release
			p := askPlacement("tcfirst", v)
			n = p.Question.N
			inside.Store(false)
			return []team.Placement{p}, nil
		})
		if err != nil {
			t.Errorf("the first compose: %v", err)
		}
		first <- n
	}()
	<-entered
	second := make(chan int64, 1)
	go func() {
		var n int64
		_, err := rt.tm.Questions(t.Context(), func(v team.QuestionsView) ([]team.Placement, error) {
			if inside.Load() {
				t.Error("the second compose ran inside the first")
			}
			p := askPlacement("tcsecond", v)
			n = p.Question.N
			return []team.Placement{p}, nil
		})
		if err != nil {
			t.Errorf("the second compose: %v", err)
		}
		second <- n
	}()
	close(release)
	if a, b := <-first, <-second; a != 1 || b != 2 {
		t.Errorf("the composes numbered Q%d and Q%d, want Q1 then Q2", a, b)
	}
}

// QX3: a compose's placement is in the view it returns, provisional, and counts as open.
func TestQX3APlacedQuestionIsProvisionalInTheIndex(t *testing.T) {
	w := newWorld(t)
	rt := w.readyWith(w.main, nil)
	v, err := rt.tm.Questions(t.Context(), func(v team.QuestionsView) ([]team.Placement, error) {
		return []team.Placement{askPlacement("tcone", v)}, nil
	})
	check(t, err)
	q, ok := v.Index.Question(1)
	if !ok || !q.Provisional || v.Index.Open() != 1 || v.Index.Next() != 2 {
		t.Errorf("Q1 %+v held %v, open %d, next %d: want a provisional open Q1 and Q2 next", q, ok, v.Index.Open(), v.Index.Next())
	}
}

// A compose that fails places nothing.
func TestAFailedComposePlacesNothing(t *testing.T) {
	w := newWorld(t)
	rt := w.readyWith(w.main, nil)
	refused := errors.New("refused")
	_, err := rt.tm.Questions(t.Context(), func(team.QuestionsView) ([]team.Placement, error) { return nil, refused })
	if !errors.Is(err, refused) {
		t.Fatalf("err %v, want the compose's", err)
	}
	if n := asks(t, rt.tm, "tcnext"); n != 1 {
		t.Errorf("the next ask took Q%d, want Q1", n)
	}
}

// QX3: after a restart the questions sends still in `sending` are placed again before any new
// compose, so a new question numbers above a queued one.
func TestQX3QueuedQuestionsArePlacedAgainBeforeACompose(t *testing.T) {
	w := newWorld(t)
	w.asCoordinator()
	content := w.ownQuestion(1)
	w.preStore(func(s *state.Store) {
		s.Outbox.Sending = append(s.Outbox.Sending, state.Sending{
			Txn: "tc0123456789abcdef0123456789abcdef", Room: proto.PurposeQuestions, Kind: state.SendQuestion,
			Type: proto.MessageType, Content: content.Compact(), SinceMS: startMS,
		})
	})
	rt := w.readyWith(w.main, nil)
	if n := asks(t, rt.tm, "tcnew"); n != 2 {
		t.Errorf("the ask took Q%d, want Q2 above the queued Q1", n)
	}
}

// QX3: while an own post of an earlier run is posted and not yet echoed, a compose waits, and
// at its deadline is 20 not-ready; a post this run made is no reason to wait.
func TestQX3AnUnechoedPostOfAnEarlierRunHoldsTheCompose(t *testing.T) {
	w := newWorld(t)
	w.asCoordinator()
	unechoed := "$unechoedunechoedunechoedunechoedunechoed00"
	w.preStore(func(s *state.Store) { s.Outbox.QuestionsUnechoed = &unechoed })
	rt := w.readyWith(w.main, nil)
	waiting := make(chan struct{}, 1)
	rt.tm.onQWait = func() {
		select {
		case waiting <- struct{}{}:
		default:
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := rt.tm.Questions(ctx, func(team.QuestionsView) ([]team.Placement, error) {
			t.Error("composed while an earlier post is unechoed")
			return nil, nil
		})
		done <- err
	}()
	<-waiting
	cancel()
	if xe, ok := errors.AsType[*exits.Error](<-done); !ok || xe.Code != exits.CodeNotReady {
		t.Fatalf("a compose past its deadline: want 20 not-ready")
	}
	rt.tm.QuestionPosted("tcthisrun", unechoed)
	if n := asks(t, rt.tm, "tcafter"); n != 1 {
		t.Errorf("the ask took Q%d, want Q1", n)
	}
}

// A member that does not coordinate the team composes nothing in questions: 11 role.
func TestAWorkerComposesNoQuestion(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	r := w.startTeam(tm)
	w.batch()
	r.poll(t)
	_, err := tm.Questions(t.Context(), func(team.QuestionsView) ([]team.Placement, error) {
		t.Error("a worker composed in questions")
		return nil, nil
	})
	if xe, ok := errors.AsType[*exits.Error](err); !ok || xe.Code != exits.CodeRole {
		t.Fatalf("err %v, want 11 role", err)
	}
}

// QX3: the echo of the placed question replaces it in the index, the batch that folds it names
// it, and an echo another's question beat to its number is counted uncounted in that batch.
func TestQX3TheEchoReplacesThePlacementAndAnUncountedEchoIsCounted(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(alice, false)
	rt := w.readyWith(w.main, nil)
	asks(t, rt.tm, "tcmine")
	admins := w.adminQuestion(1)
	w.batchWith(lastIs(admins))
	echo := w.sayIn(w.qroom(), w.self, rawContent(t, w.ownQuestion(1)))
	b := w.batchWith(lastIs(echo))
	if b.QuestionsUncounted != 1 || !slices.Contains(b.QuestionsFolded, echo) {
		t.Errorf("uncounted %d, folded %v: want the echo folded and counted uncounted once", b.QuestionsUncounted, b.QuestionsFolded)
	}
	if n := asks(t, rt.tm, "tcnext"); n != 2 {
		t.Errorf("the next ask took Q%d, want Q2: the dropped placement still holds a number", n)
	}
}

// QX3: the sender's check holds a question whose number a folded event counts.
func TestQX3APostWhoseNumberIsTakenIsNotPostable(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(alice, false)
	rt := w.readyWith(w.main, nil)
	content := w.ownQuestion(1)
	check(t, rt.tm.QuestionsPostable(content))
	w.batchWith(lastIs(w.adminQuestion(1)))
	if err := rt.tm.QuestionsPostable(content); err == nil || errors.Is(err, team.ErrQuestionsNotReady) {
		t.Errorf("err %v, want a refusal: Q1 is counted", err)
	}
}

// rawContent is content as sayIn takes it.
func rawContent(t *testing.T, v proto.Value) map[string]any {
	t.Helper()
	var m map[string]any
	check(t, json.Unmarshal(v.Compact(), &m))
	return m
}

// preStore changes the store an earlier run left, before the team starts.
func (w *world) preStore(f func(*state.Store)) {
	w.t.Helper()
	_, err := w.eng.Load(w.eng.tm, user(w.self))
	check(w.t, err)
	w.eng.withStore(f)
}
