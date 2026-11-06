package proto

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const codecNextBatch = "s72594_4483_1934_0_1"

// eids are n distinct event IDs from the n-th on.
func eids(from, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = eid(from + i)
	}
	return out
}

// agentIDOfLen is a distinct agent user ID of this server of exactly total bytes.
func agentIDOfLen(total, seat int) string {
	number := strconv.Itoa(seat)
	site := strings.Repeat("x", total-2-len(testServer)-57-len(number))
	return UserID(strings.Repeat("r", 48)+"."+number+"+"+site+".podman", testServer)
}

// codecState is a state folded from a small room that holds each part the codec stores: Q1 open
// with its `asked_by`, `re` and reminder and an unanswered thread post, Q2 closed by an agent's
// thread post, Q3 a number held by a redacted event, Q5 an admin's question, a slot among the
// posts and an unattached human post.
func codecState(t *testing.T) QuestionsState {
	t.Helper()
	ix := NewQuestionIndex(testServer, 0)
	ix.Feed(askVerdict(eid(1), coordID, 1000, Question{N: 1, AskedBy: []string{aliceID, workerID}, Re: []string{eid(901)}, RemindAfterS: 900}, questionBody(1)))
	askAgent(ix, 2)
	ix.Attach(humanEvent(eid(11), aliceID, 2000), Human{ThreadRoot: eid(1), Body: "MIT"})
	ix.Feed(threadVerdict(eid(21), coordID, eid(2), 2, true))
	ix.Slot(eid(31))
	ix.Feed(askVerdict(eid(5), adminID, 4000, Question{N: 5}, questionBody(5)))
	ix.Attach(humanEvent(eid(12), aliceID, 3000), Human{Body: "unrelated"})
	st := ix.State()
	return QuestionsState{Index: &st, Token: new(codecNextBatch), Last: new(eid(12))}
}

func encodeOK(t *testing.T, s QuestionsState) []byte {
	t.Helper()
	data, err := s.Encode(testServer)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return data
}

func TestQuestionsStateFixtureHoldsEachPostKind(t *testing.T) {
	s := codecState(t)
	want := []PostEntry{{eid(11), 1, postHumanThread}, {eid(12), 0, postHuman}}
	agents := []AgentPost{{eid(21), 2}, {eid(31), 0}}
	if !reflect.DeepEqual(s.Index.Posts, want) || !reflect.DeepEqual(s.Index.AgentPosts, agents) || len(s.Index.Unattached) != 1 {
		t.Errorf("posts = %+v agent posts = %+v unattached = %+v", s.Index.Posts, s.Index.AgentPosts, s.Index.Unattached)
	}
}

func TestQuestionsStateFixtureHoldsEachKindOfQuestion(t *testing.T) {
	s := codecState(t)
	qs := s.Index.Questions
	if len(qs) != 4 || qs[0].N != 1 || qs[1].N != 2 || qs[2].N != 3 || qs[3].N != 5 {
		t.Fatalf("questions = %+v, want Q1 Q2 Q3 Q5", qs)
	}
	if qs[2].EventID != "" || !qs[2].Closed || !qs[1].Closed || qs[0].Closed {
		t.Errorf("the held, closed and open entries are not as the fixture needs: %+v", qs)
	}
	if q1 := qs[0]; len(q1.AskedBy) != 2 || len(q1.Re) != 1 || q1.RemindAfterS != 900 || len(q1.Unanswered) != 1 {
		t.Errorf("Q1 holds no asked_by, re, reminder and unanswered post: %+v", q1)
	}
}

func TestQuestionsStateRoundTrip(t *testing.T) {
	s := codecState(t)
	data := encodeOK(t, s)
	back, err := DecodeQuestionsState(data, testServer)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !reflect.DeepEqual(back, s) {
		t.Fatalf("decoded %+v, want %+v", back, s)
	}
	if again := encodeOK(t, back); !bytes.Equal(again, data) {
		t.Errorf("encoding the decoded state gave other bytes:\n%s\n%s", data, again)
	}
	ix, err := LoadQuestionIndex(testServer, 0, *back.Index)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want, err := json.Marshal(*s.Index)
	if err != nil {
		t.Fatal(err)
	}
	if got := stateJSON(t, ix); got != string(want) {
		t.Errorf("a loaded index's state differs:\n%s\n%s", want, got)
	}
}

func TestQuestionsStateOfAFreshIndexAndOfBareQuestionsEncodesNoNull(t *testing.T) {
	fresh := NewQuestionIndex(testServer, 0)
	bare := NewQuestionIndex(testServer, 0)
	askAgent(bare, 1)
	for name, ix := range map[string]*QuestionIndex{"fresh": fresh, "bare question": bare, "copy of it": bare.Copy()} {
		st := ix.State()
		data, err := QuestionsState{Index: &st, Token: new(codecNextBatch), Last: new(eid(1))}.Encode(testServer)
		if err != nil {
			t.Errorf("%s: Encode: %v", name, err)
		} else if bytes.Count(data, []byte("null")) != 1 || !bytes.Contains(data, []byte(`"build":null`)) {
			t.Errorf("%s: the state holds a null that is not the build record: %s", name, data)
		}
	}
}

func TestQuestionsStateRefusesDocuments(t *testing.T) {
	doc := string(encodeOK(t, codecState(t)))
	cases := []struct {
		name, code string
		with       func(string) string
	}{
		{"a repeated key", CodeJSON, func(d string) string { return strings.Replace(d, `"build":null`, `"build":null,"build":null`, 1) }},
		{"an integer past 2^53", CodeJSON, func(d string) string { return strings.Replace(d, `"highest":5`, `"highest":9007199254740992`, 1) }},
		{"a fraction", CodeJSON, func(d string) string { return strings.Replace(d, `"highest":5`, `"highest":5.0`, 1) }},
		{"an exponent", CodeJSON, func(d string) string { return strings.Replace(d, `"highest":5`, `"highest":5e0`, 1) }},
		{"content after the value", CodeJSON, func(d string) string { return d + " x" }},
		{"invalid UTF-8", CodeJSON, func(d string) string { return strings.Replace(d, `"token":"s`, "\"token\":\"s\xff", 1) }},
		{"nothing", CodeJSON, func(string) string { return "" }},
		{"an array", CodeSchema, func(string) string { return "[]" }},
		{"null", CodeSchema, func(string) string { return "null" }},
		{"a key it does not have", CodeSchema, func(d string) string { return strings.Replace(d, `"build":null`, `"build":null,"extra":1`, 1) }},
		{"a key a question does not have", CodeSchema, func(d string) string { return strings.Replace(d, `"n":1,`, `"n":1,"extra":1,`, 1) }},
		{"a key missing", CodeSchema, func(d string) string { return strings.Replace(d, `,"build":null`, ``, 1) }},
		{"a key in another case", CodeSchema, func(d string) string { return strings.Replace(d, `"token":`, `"Token":`, 1) }},
		{"a string for an integer", CodeSchema, func(d string) string { return strings.Replace(d, `"highest":5`, `"highest":"5"`, 1) }},
		{"a document past its cap", CodeSchema, func(d string) string { return d + strings.Repeat(" ", MaxQuestionsStateBytes) }},
	}
	// Rules that another rule of the decoder also refuses by the same code are told apart by
	// their text: an array or null is refused by its own rule, an unknown key by the decoder
	// that has no such field and a key missing by the round trip.
	ruleOf := map[string]string{
		"an array": "not an object", "null": "not an object",
		"a key it does not have": "does not have", "a key a question does not have": "does not have",
		"a key missing": "missing or in another case", "a key in another case": "missing or in another case",
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeQuestionsState([]byte(tc.with(doc)), testServer)
			var e *QuestionsStateError
			if !errors.As(err, &e) || !errors.Is(err, ErrQuestionsState) || e.Code != tc.code {
				t.Fatalf("error = %v, want a %s refusal", err, tc.code)
			}
			if want := ruleOf[tc.name]; !strings.Contains(e.Rule, want) {
				t.Errorf("rule = %q, want one about %q", e.Rule, want)
			}
		})
	}
}

func TestQuestionsStateRefusalsQuoteNoValue(t *testing.T) {
	doc := string(encodeOK(t, codecState(t)))
	for name, bad := range map[string]string{
		"a token":      strings.Replace(doc, codecNextBatch, "SECRETMARK<", 1),
		"a key":        strings.Replace(doc, `"build":null`, `"SECRETMARK":1`, 1),
		"a user id":    strings.Replace(doc, aliceID, "@SECRETMARK:elsewhere", 1),
		"a syntax key": strings.Replace(doc, `"build":null`, `"SECRETMARK":1,"SECRETMARK":2`, 1),
	} {
		_, err := DecodeQuestionsState([]byte(bad), testServer)
		if err == nil || strings.Contains(err.Error(), "SECRETMARK") {
			t.Errorf("%s: error = %v, want a refusal that quotes nothing of the file", name, err)
		}
	}
}

// openQuestion and closedQuestion are well-formed entries of number n.
func openQuestion(n int64) IndexedQuestion {
	return IndexedQuestion{
		N: n, EventID: eid(1000 + int(n)), Latest: eid(1000 + int(n)), Headline: "h", Hash: sectionHash("x"),
		AskedBy: []string{}, Re: []string{}, Unanswered: []string{},
	}
}

func closedQuestion(n int64) IndexedQuestion {
	q := openQuestion(n)
	q.Closed = true
	return q
}

func manyQuestions(from int64, n int, entry func(int64) IndexedQuestion) []IndexedQuestion {
	out := make([]IndexedQuestion, n)
	for i := range out {
		out[i] = entry(from + int64(i))
	}
	return out
}

func manyUsers(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = UserID("u"+strconv.Itoa(i), testServer)
	}
	return out
}

func manyPosts(n int) []PostEntry {
	out := make([]PostEntry, n)
	for i := range out {
		out[i] = PostEntry{ID: eid(5000 + i)}
	}
	return out
}

func manyAgentPosts(n int) []AgentPost {
	out := make([]AgentPost, n)
	for i := range out {
		out[i] = AgentPost{ID: eid(6000 + i)}
	}
	return out
}

// ruleRows are the states each codec rule refuses: a mutation of codecState and the text of the
// rule it breaks. Q1 is Questions[0], Q2 [1], the held Q3 [2]; the posts are, in order, a human
// post in Q1's thread and a human post of no question, and the agent posts an agent's thread
// post in Q2 and a slot.
func ruleRows() []ruleRow {
	return slices.Concat(structureRows(), postRows(), tokenRows())
}

type ruleRow struct {
	name, want string
	mutate     func(s *QuestionsState)
}

func questionAt(s *QuestionsState, i int) *IndexedQuestion { return &s.Index.Questions[i] }

func structureRows() []ruleRow {
	q := questionAt
	return []ruleRow{
		{"negative highest", "highest is negative", func(s *QuestionsState) { s.Index.Highest = -1 }},
		{"negative slots", "slots is negative", func(s *QuestionsState) { s.Index.Slots = -1 }},
		{"negative evicted", "evicted is negative", func(s *QuestionsState) { s.Index.Evicted = -1 }},
		{"counters past 2^53", "past 2^53", func(s *QuestionsState) { s.Index.Evicted = maxSafeInt }},
		{"null questions", "questions is null", func(s *QuestionsState) { s.Index.Questions = nil }},
		{"questions descending", "not ascending", func(s *QuestionsState) {
			s.Index.Questions[0], s.Index.Questions[1] = s.Index.Questions[1], s.Index.Questions[0]
		}},
		{"a repeated number", "not ascending", func(s *QuestionsState) { q(s, 1).N = 1 }},
		{"number 0", "not ascending", func(s *QuestionsState) { q(s, 0).N = 0 }},
		{"a number past the highest", "past the highest", func(s *QuestionsState) { s.Index.Highest = 4 }},
		{"65 not closed", "not closed", func(s *QuestionsState) {
			s.Index.Questions = append(s.Index.Questions, manyQuestions(6, 63, openQuestion)...)
			s.Index.Highest = 68
		}},
		{"129 closed", "questions are closed", func(s *QuestionsState) {
			s.Index.Questions = append(s.Index.Questions, manyQuestions(6, 127, closedQuestion)...)
			s.Index.Highest = 132
		}},
		{"null asked_by", "a list of a question is null", func(s *QuestionsState) { q(s, 0).AskedBy = nil }},
		{"null re", "a list of a question is null", func(s *QuestionsState) { q(s, 0).Re = nil }},
		{"null unanswered", "a list of a question is null", func(s *QuestionsState) { q(s, 0).Unanswered = nil }},
		{"a question id that is not an event id", "a question's id", func(s *QuestionsState) { q(s, 0).EventID = "x" }},
		{"a latest that is not an event id", "latest", func(s *QuestionsState) { q(s, 0).Latest = "x" }},
		{"a hash of upper-case hex", "hash", func(s *QuestionsState) { q(s, 0).Hash = strings.ToUpper(q(s, 0).Hash) }},
		{"a short hash", "hash", func(s *QuestionsState) { q(s, 0).Hash = "abc" }},
		{"an empty headline", "headline", func(s *QuestionsState) { q(s, 0).Headline = "" }},
		{"a 121 code point headline", "headline", func(s *QuestionsState) { q(s, 0).Headline = strings.Repeat("é", 121) }},
		{"17 asked_by", "asked_by", func(s *QuestionsState) { q(s, 0).AskedBy = manyUsers(17) }},
		{"a repeated asked_by", "asked_by", func(s *QuestionsState) { q(s, 0).AskedBy = []string{aliceID, aliceID} }},
		{"an asked_by of another server", "asked_by", func(s *QuestionsState) { q(s, 0).AskedBy = []string{UserID("alice", "other.example")} }},
		{"an asked_by of 129 bytes", "asked_by", func(s *QuestionsState) { q(s, 0).AskedBy = []string{agentIDOfLen(129, 1)} }},
		{"17 re", "re is not", func(s *QuestionsState) { q(s, 0).Re = eids(100, 17) }},
		{"a repeated re", "re is not", func(s *QuestionsState) { q(s, 0).Re = []string{eid(9), eid(9)} }},
		{"a re that is not an event id", "re is not", func(s *QuestionsState) { q(s, 0).Re = []string{"x"} }},
		{"negative asked_ms", "asked_ms", func(s *QuestionsState) { q(s, 0).AskedMs = -1 }},
		{"remind_after_s 1", "remind_after_s", func(s *QuestionsState) { q(s, 0).RemindAfterS = 1 }},
		{"remind_after_s 599", "remind_after_s", func(s *QuestionsState) { q(s, 0).RemindAfterS = 599 }},
		{"remind_after_s 604801", "remind_after_s", func(s *QuestionsState) { q(s, 0).RemindAfterS = 604801 }},
		{"negative remind_after_s", "remind_after_s", func(s *QuestionsState) { q(s, 0).RemindAfterS = -1 }},
		{"negative attached", "attached is negative", func(s *QuestionsState) { q(s, 0).Attached = -1 }},
		{"17 unanswered", "unanswered is not", func(s *QuestionsState) { q(s, 0).Unanswered, q(s, 0).Attached = eids(100, 17), 17 }},
		{"a repeated unanswered", "unanswered is not", func(s *QuestionsState) { q(s, 0).Unanswered, q(s, 0).Attached = []string{eid(9), eid(9)}, 2 }},
		{"an unanswered that is not an event id", "unanswered is not", func(s *QuestionsState) { q(s, 0).Unanswered = []string{"x"} }},
		{"negative unanswered_more", "unanswered_more is negative", func(s *QuestionsState) { q(s, 0).UnansweredMore = -1 }},
		{"unanswered_more below 16", "fewer than 16", func(s *QuestionsState) { q(s, 0).UnansweredMore = 1 }},
		{"attached below the unanswered", "fewer than its unanswered", func(s *QuestionsState) { q(s, 0).Attached = 0 }},
	}
}

func postRows() []ruleRow {
	q := questionAt
	return []ruleRow{
		{"a held number not closed", "held number is not closed", func(s *QuestionsState) { q(s, 2).Closed = false }},
		{"a held number with a latest", "held number has text", func(s *QuestionsState) { q(s, 2).Latest = eid(9) }},
		{"a held number with a headline", "held number has text", func(s *QuestionsState) { q(s, 2).Headline = "h" }},
		{"a held number with a hash", "held number has text", func(s *QuestionsState) { q(s, 2).Hash = sectionHash("x") }},
		{"a held number with asked_by", "held number has a list", func(s *QuestionsState) { q(s, 2).AskedBy = []string{aliceID} }},
		{"a held number with re", "held number has a list", func(s *QuestionsState) { q(s, 2).Re = []string{eid(9)} }},
		{"a held number with unanswered", "held number has a list", func(s *QuestionsState) { q(s, 2).Unanswered = []string{eid(9)} }},
		{"a held number with asked_ms", "held number has a count", func(s *QuestionsState) { q(s, 2).AskedMs = 1 }},
		{"a held number with a reminder", "held number has a count", func(s *QuestionsState) { q(s, 2).RemindAfterS = 600 }},
		{"a held number with attached", "held number has a count", func(s *QuestionsState) { q(s, 2).Attached = 1 }},
		{"a held number with unanswered_more", "held number has a count", func(s *QuestionsState) { q(s, 2).UnansweredMore = 1 }},
		{"null posts", "posts is null", func(s *QuestionsState) { s.Index.Posts = nil }},
		{"1025 posts", "1024 posts", func(s *QuestionsState) { s.Index.Posts = manyPosts(1025) }},
		{"a post id that is not an event id", "post's id", func(s *QuestionsState) { s.Index.Posts[0].ID = "x" }},
		{"a repeated post id", "post id is repeated", func(s *QuestionsState) { s.Index.Posts[1].ID = s.Index.Posts[0].ID }},
		{"post kind 2", "kind", func(s *QuestionsState) { s.Index.Posts[0].T = 2 }},
		{"post kind -1", "kind", func(s *QuestionsState) { s.Index.Posts[1].T = -1 }},
		{"a human thread post of no question", "has no question", func(s *QuestionsState) { s.Index.Posts[0].Q = 0 }},
		{"a post of a negative question", "post's question is negative", func(s *QuestionsState) { s.Index.Posts[1].Q = -1 }},
		{"null agent posts", "agent_posts is null", func(s *QuestionsState) { s.Index.AgentPosts = nil }},
		{"257 agent posts", "256 agent posts", func(s *QuestionsState) { s.Index.AgentPosts = manyAgentPosts(257) }},
		{"an agent post id that is not an event id", "agent post's id", func(s *QuestionsState) { s.Index.AgentPosts[0].ID = "x" }},
		{"a repeated agent post id", "agent post id is repeated", func(s *QuestionsState) { s.Index.AgentPosts[1].ID = s.Index.AgentPosts[0].ID }},
		{"an agent post id that is a post's", "agent post id is repeated", func(s *QuestionsState) { s.Index.AgentPosts[1].ID = s.Index.Posts[1].ID }},
		{"an agent post of a negative question", "agent post's question is negative", func(s *QuestionsState) { s.Index.AgentPosts[0].Q = -1 }},
		{"null unattached", "unattached is null", func(s *QuestionsState) { s.Index.Unattached = nil }},
		{"65 unattached", "64 unattached", func(s *QuestionsState) {
			s.Index.Unattached = nil
			for _, p := range manyPosts(65) {
				s.Index.Unattached = append(s.Index.Unattached, UnattachedPost{ID: p.ID, From: aliceID})
			}
		}},
		{"an unattached id that is not an event id", "unattached post's id", func(s *QuestionsState) { s.Index.Unattached[0].ID = "x" }},
		{"a repeated unattached id", "unattached id is repeated", func(s *QuestionsState) {
			s.Index.Unattached = append(s.Index.Unattached, s.Index.Unattached[0])
		}},
		{"an unattached from_id of nothing", "from_id", func(s *QuestionsState) { s.Index.Unattached[0].From = "x" }},
		{"an unattached from_id of another server", "from_id", func(s *QuestionsState) { s.Index.Unattached[0].From = UserID("alice", "other.example") }},
		{"a negative sent_ms", "sent_ms", func(s *QuestionsState) { s.Index.Unattached[0].SentMs = -1 }},
	}
}

func tokenRows() []ruleRow {
	return []ruleRow{
		{"an empty token", "token is empty", func(s *QuestionsState) { s.Token = new("") }},
		{"a 1025 byte token", "token is longer", func(s *QuestionsState) { s.Token = new(strings.Repeat("a", 1025)) }},
		{"a token with a quote", "token has a character", func(s *QuestionsState) { s.Token = new(`a"b`) }},
		{"a token with a backslash", "token has a character", func(s *QuestionsState) { s.Token = new(`a\b`) }},
		{"a token with <", "token has a character", func(s *QuestionsState) { s.Token = new("a<b") }},
		{"a token with >", "token has a character", func(s *QuestionsState) { s.Token = new("a>b") }},
		{"a token with &", "token has a character", func(s *QuestionsState) { s.Token = new("a&b") }},
		{"a token with a space", "token has a character", func(s *QuestionsState) { s.Token = new("a b") }},
		{"a token with a control character", "token has a character", func(s *QuestionsState) { s.Token = new("a\x01b") }},
		{"a token with a non-ASCII character", "token has a character", func(s *QuestionsState) { s.Token = new("aéb") }},
		{"a last that is not an event id", "last is not", func(s *QuestionsState) { s.Last = new("x") }},
	}
}

func TestQuestionsStateRefusesEachRule(t *testing.T) {
	for _, tc := range ruleRows() {
		t.Run(tc.name, func(t *testing.T) {
			s := codecState(t)
			tc.mutate(&s)
			assertRefused(t, s, tc.want)
		})
	}
}

// assertRefused holds that Check, Encode and the decoder of the state's own JSON all refuse it
// by the rule whose text holds want.
func assertRefused(t *testing.T, s QuestionsState, want string) {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	_, decErr := DecodeQuestionsState(data, testServer)
	_, encErr := s.Encode(testServer)
	for name, err := range map[string]error{"Check": s.Check(testServer), "Decode": decErr, "Encode": encErr} {
		var e *QuestionsStateError
		if !errors.As(err, &e) || !strings.Contains(e.Rule, want) {
			t.Errorf("%s = %v, want the rule about %q", name, err, want)
		}
	}
}

func TestQuestionsStateBuildRecord(t *testing.T) {
	head := func(mutate func(*QuestionsState)) QuestionsState {
		s := codecState(t)
		mutate(&s)
		return s
	}
	owed := func(b BuildRecord) QuestionsState { return QuestionsState{Build: &b} }
	under := func(b BuildRecord) QuestionsState {
		return head(func(s *QuestionsState) { s.Build = &b })
	}
	refused := []struct {
		name, want string
		s          QuestionsState
	}{
		{"no build and no index", "an index at the head", head(func(s *QuestionsState) { s.Index = nil })},
		{"no build and no token", "an index at the head", head(func(s *QuestionsState) { s.Token = nil })},
		{"no build and no last", "an index at the head", head(func(s *QuestionsState) { s.Last = nil })},
		{"a build owed with an index", "owed holds no", head(func(s *QuestionsState) { s.Build = &BuildRecord{After: new(eid(1))} })},
		{"a build owed with a token", "owed holds no", QuestionsState{Token: new("t"), Build: &BuildRecord{After: new(eid(1))}}},
		{"a build owed with a last", "owed holds no", QuestionsState{Last: new(eid(2)), Build: &BuildRecord{After: new(eid(1))}}},
		{"a build owed with no after", "owed names", owed(BuildRecord{})},
		{"a build owed with a bad after", "after is not", owed(BuildRecord{After: new("x")})},
		{"a build owed with late_from 0", "late_from", owed(BuildRecord{After: new(eid(1)), LateFrom: new(int64(0))})},
		{"a build under way with no index", "under way holds", func() QuestionsState {
			s := under(BuildRecord{Start: new(eid(3))})
			s.Index = nil
			return s
		}()},
		{"a build under way with no token", "under way holds", func() QuestionsState {
			s := under(BuildRecord{Start: new(eid(3))})
			s.Token = nil
			return s
		}()},
		{"a build under way with no last", "under way holds", func() QuestionsState {
			s := under(BuildRecord{Start: new(eid(3))})
			s.Last = nil
			return s
		}()},
		{"a bad start", "start is not", under(BuildRecord{Start: new("x")})},
		{"a bad after under way", "after is not", under(BuildRecord{Start: new(eid(3)), After: new("x")})},
		{"late_from -1 under way", "late_from", under(BuildRecord{Start: new(eid(3)), LateFrom: new(int64(-1))})},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) { assertRefused(t, tc.s, tc.want) })
	}
	// Past 2^53 the strict decoder refuses the document before Check reads it: Check alone.
	past := under(BuildRecord{Start: new(eid(3)), LateFrom: new(int64(maxSafeInt + 1))})
	if err := past.Check(testServer); err == nil || !strings.Contains(err.Error(), "late_from") {
		t.Errorf("Check of a late_from past 2^53 = %v", err)
	}
	accepted := map[string]QuestionsState{
		"at the head":                   head(func(*QuestionsState) {}),
		"owed with after":               owed(BuildRecord{After: new(eid(1))}),
		"owed with after and late_from": owed(BuildRecord{After: new(eid(1)), LateFrom: new(int64(7))}),
		"under way, all set":            under(BuildRecord{Start: new(eid(3)), After: new(eid(1)), LateFrom: new(int64(7))}),
		"under way, begun with no key":  under(BuildRecord{Start: new(eid(3))}),
		"under way, after passed":       under(BuildRecord{Start: new(eid(3)), LateFrom: new(int64(7))}),
		"under way, late_from null":     under(BuildRecord{Start: new(eid(3)), After: new(eid(1))}),
	}
	for name, s := range accepted {
		data, err := s.Encode(testServer)
		if err != nil {
			t.Errorf("%s: Encode: %v", name, err)
			continue
		}
		back, err := DecodeQuestionsState(data, testServer)
		if err != nil || !reflect.DeepEqual(back, s) {
			t.Errorf("%s: decoded %+v (%v), want %+v", name, back, err, s)
		}
	}
}

func TestQuestionsStateAcceptsTheBoundaries(t *testing.T) {
	for name, mutate := range map[string]func(*QuestionsState){
		"remind_after_s 600":    func(s *QuestionsState) { s.Index.Questions[0].RemindAfterS = 600 },
		"remind_after_s 604800": func(s *QuestionsState) { s.Index.Questions[0].RemindAfterS = 604800 },
		"remind_after_s 0":      func(s *QuestionsState) { s.Index.Questions[0].RemindAfterS = 0 },
		"a 120 code point head": func(s *QuestionsState) { s.Index.Questions[0].Headline = strings.Repeat("é", 120) },
		"every allowed byte":    func(s *QuestionsState) { s.Token = new("!#$%'()*+,-./09:;=?@AZ[]^_`az{|}~") },
		"counters just below":   func(s *QuestionsState) { s.Index.Slots, s.Index.Highest = maxSafeInt-10, 5 },
		"a 1024 byte token":     func(s *QuestionsState) { s.Token = new(strings.Repeat("a", 1024)) },
		"128 byte user ids": func(s *QuestionsState) {
			s.Index.Questions[0].AskedBy = []string{agentIDOfLen(128, 1), agentIDOfLen(128, 2)}
		},
		"16 unanswered, 3 extra": func(s *QuestionsState) {
			s.Index.Questions[0].Unanswered, s.Index.Questions[0].Attached, s.Index.Questions[0].UnansweredMore = eids(100, 16), 19, 3
		},
	} {
		s := codecState(t)
		mutate(&s)
		if _, err := s.Encode(testServer); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestLoadQuestionIndexRebuildsWhatTheFoldLooksUp(t *testing.T) {
	s := codecState(t)
	ix, err := LoadQuestionIndex(testServer, 0, *s.Index)
	if err != nil {
		t.Fatal(err)
	}
	if got := ix.Next(); got != 6 {
		t.Errorf("Next() = %d, want 6", got)
	}
	if q, ok := ix.Attach(humanEvent(eid(40), aliceID, 1), Human{ThreadRoot: eid(1), Body: "x"}); !ok || q.N != 1 {
		t.Errorf("a post in Q1's thread attached to Q%d (%v): the roots were not rebuilt", q.N, ok)
	}
	if q, ok := ix.Attach(humanEvent(eid(11), aliceID, 1), Human{ThreadRoot: eid(1), Body: "x"}); !ok || q.N != 1 || q.Attached != 2 {
		t.Errorf("a post folded before attached again: %+v (%v), want Q1 unchanged at 2 attached", q, ok)
	}
	if _, ok := ix.Attach(humanEvent(eid(41), aliceID, 1), Human{Body: "plain"}); ok {
		t.Error("a human post with no thread root attached to a question: a held number holds the empty root")
	}
	if _, ok := ix.Attach(humanEvent(eid(42), aliceID, 1), Human{Body: "Q3 late"}); ok {
		t.Error("a post naming a held number attached to it")
	}
	reply := Verdict{
		Outcome: Index, Event: Event{ID: eid(50), Type: MessageType, Sender: coordID, OriginTs: 1},
		Content: Content{Key: KeyThread, Relation: ReplyTo(eid(12))},
	}
	if !ix.Feed(reply) || len(ix.Unattached()) != 2 {
		t.Errorf("a plain reply to a listed post did not count: the unattached list was not loaded (%+v)", ix.Unattached())
	}
	if ix.Feed(threadVerdict(eid(21), coordID, eid(2), 2, true)) {
		t.Error("a thread post folded before counted again: the posts were not loaded")
	}
}

func TestLoadQuestionIndexHoldsTheFloorAndTheSlots(t *testing.T) {
	s := codecState(t)
	s.Index.Slots, s.Index.Evicted = 2, 3
	ix, err := LoadQuestionIndex(testServer, 0, *s.Index)
	if err != nil || ix.Next() != 8 || ix.Evicted() != 3 {
		t.Fatalf("Next() = %d, Evicted() = %d (%v), want 1 + highest 5 + 2 slots and 3", ix.Next(), ix.Evicted(), err)
	}
	ix, err = LoadQuestionIndex(testServer, 50, *s.Index)
	if err != nil || ix.Next() != 53 || ix.State().Highest != 50 {
		t.Errorf("with a floor of 50: Next() = %d, highest %d (%v), want 53 and 50", ix.Next(), ix.State().Highest, err)
	}
	ix, err = LoadQuestionIndex(testServer, 3, *s.Index)
	if err != nil || ix.State().Highest != 5 {
		t.Errorf("a floor below the highest changed it: %d (%v)", ix.State().Highest, err)
	}
}

func TestLoadQuestionIndexRefusesWhatCheckRefusesAndSharesNothing(t *testing.T) {
	s := codecState(t)
	bad := *s.Index
	bad.Posts = nil
	if ix, err := LoadQuestionIndex(testServer, 0, bad); ix != nil || !errors.Is(err, ErrQuestionsState) {
		t.Errorf("Load of an invalid state = %v, %v", ix, err)
	}
	ix, err := LoadQuestionIndex(testServer, 0, *s.Index)
	if err != nil {
		t.Fatal(err)
	}
	before := stateJSON(t, ix)
	s.Index.Questions[0].Unanswered[0] = "x"
	s.Index.Questions[0].AskedBy[0] = "x"
	s.Index.Posts[0].ID = "x"
	s.Index.AgentPosts[0].ID = "x"
	s.Index.Unattached[0].ID = "x"
	if after := stateJSON(t, ix); after != before {
		t.Errorf("the loaded index changed with the state it was loaded from:\n%s\n%s", before, after)
	}
}
