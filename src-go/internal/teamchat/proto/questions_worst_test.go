package proto

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// worstQuestion is a stored question of the longest entry QX6 sizes: 16 `asked_by` of 128
// bytes, 16 `re`, 16 unanswered with a count past them, a headline of 120 code points that
// encoding/json escapes to 6 bytes each, and every integer at its widest.
func worstQuestion(i int, closed bool, highest int64) IndexedQuestion {
	asked := make([]string, MaxAskedBy)
	for j := range asked {
		asked[j] = agentIDOfLen(128, j+1)
	}
	return IndexedQuestion{
		N: highest - 191 + int64(i), EventID: eid(100000 + i), Latest: eid(200000 + i),
		Headline: strings.Repeat("<", MaxHeadline), Hash: sectionHash(strings.Repeat("x", i+1)),
		AskedBy: asked, Re: eids(300000+i*16, MaxQuestionRe), AskedMs: maxSafeInt, RemindAfterS: MaxRemindS,
		Closed: closed, Attached: maxSafeInt, Unanswered: eids(400000+i*16, MaxUnanswered), UnansweredMore: maxSafeInt - MaxUnanswered,
	}
}

// worstState is the questions key with every cap full at its widest (QX6): 64 questions not
// closed and 128 closed, 1024 posts, 64 unattached posts, a 1024 byte token and a build under way.
func worstState() QuestionsState {
	const wide = 3_000_000_000_000_000
	st := IndexState{Highest: wide, Slots: wide, Evicted: wide}
	for i := range MaxOpenQuestions + MaxClosedQuestions {
		st.Questions = append(st.Questions, worstQuestion(i, i >= MaxOpenQuestions, wide))
	}
	for i := range MaxAttachments {
		st.Posts = append(st.Posts, PostEntry{ID: eid(500000 + i), Q: wide, T: postHumanThread})
	}
	for i := range MaxUnattached {
		st.Unattached = append(st.Unattached, UnattachedPost{ID: eid(600000 + i), From: agentIDOfLen(128, i+1), SentMs: maxSafeInt})
	}
	token := strings.Repeat("~", maxStateToken)
	return QuestionsState{
		Index: &st, Token: &token, Last: new(eid(700000)),
		Build: &BuildRecord{Start: new(eid(700001)), After: new(eid(700002)), LateFrom: new(int64(maxSafeInt))},
	}
}

func TestQuestionsStateWorstCaseFitsItsSizeAndRoundTrips(t *testing.T) {
	s := worstState()
	plain, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) > MaxQuestionsStateBytes {
		t.Fatalf("the full state is %d bytes, QX6 sizes it at most %d", len(plain), MaxQuestionsStateBytes)
	}
	data, err := s.Encode(testServer)
	if err != nil || !bytes.Equal(data, plain) {
		t.Fatalf("Encode of the full state: %v (equals encoding/json: %v)", err, bytes.Equal(data, plain))
	}
	t.Logf("the full state is %d bytes of at most %d", len(data), MaxQuestionsStateBytes)
	back, err := DecodeQuestionsState(data, testServer)
	if err != nil || !reflect.DeepEqual(back, s) {
		t.Fatalf("the strict decoder of the full state: %v (equal: %v)", err, reflect.DeepEqual(back, s))
	}
	ix, err := LoadQuestionIndex(testServer, 0, *back.Index)
	if err != nil {
		t.Fatal(err)
	}
	if got := stateJSON(t, ix); got != string(mustMarshal(t, *s.Index)) {
		t.Error("the loaded index's state is not the stored one")
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestQuestionsStateWorstCaseIsTheWorstCase(t *testing.T) {
	s := worstState()
	q := s.Index.Questions[0]
	if len(q.AskedBy) != 16 || len(q.AskedBy[0]) != 128 || len(q.Re) != 16 || len(q.Unanswered) != 16 || q.UnansweredMore == 0 {
		t.Errorf("a question is not the longest: %d asked_by of %d bytes, %d re, %d unanswered", len(q.AskedBy), len(q.AskedBy[0]), len(q.Re), len(q.Unanswered))
	}
	if string(mustMarshal(t, q.Headline)) != `"`+strings.Repeat(`\`+"u003c", 120)+`"` {
		t.Error("the headline is not 120 code points of 6 escaped bytes each")
	}
	open := 0
	for _, q := range s.Index.Questions {
		if !q.Closed {
			open++
		}
	}
	if open != MaxOpenQuestions || len(s.Index.Questions) != 192 || len(s.Index.Posts) != MaxAttachments || len(s.Index.Unattached) != MaxUnattached || len(*s.Token) != 1024 {
		t.Errorf("a cap is not full: %d open of %d, %d posts, %d unattached, token %d", open, len(s.Index.Questions), len(s.Index.Posts), len(s.Index.Unattached), len(*s.Token))
	}
}
