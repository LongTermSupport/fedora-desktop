package proto

import (
	"regexp"
	"slices"
	"strconv"
)

// The states of a question (A 10.4).
const (
	QuestionOpen     = "open"
	QuestionAnswered = "answered"
	QuestionClosed   = "closed"
)

// questionPrefixRe is rule 3 of A 10.4: `Q<n>` in any case, then a space, `:` or `.`.
var questionPrefixRe = regexp.MustCompile(`^[Qq]([1-9][0-9]{0,5})[ :.]`)

// IndexedQuestion is one counted question: its post, its form, and what its thread holds so
// far. Latest is the newest event in its thread (the question itself while the thread is
// empty), which a thread post's fallback reply names (A 10.3); Attached counts the human
// posts attached to it, and Unanswered those after the last agent post in its thread (P58).
type IndexedQuestion struct {
	N                    int64
	EventID, Sender      string
	OriginTs             int64
	Headline, Question   string
	RemindAfterS         int64
	Closed               bool
	Attached, Unanswered int
	Latest               string
}

// State is A 10.4's: closed once a closing post exists, else answered once a human post is
// attached, else open.
func (q IndexedQuestion) State() string {
	switch {
	case q.Closed:
		return QuestionClosed
	case q.Attached > 0:
		return QuestionAnswered
	}
	return QuestionOpen
}

// Reminder is the one TIMEOUT an open question with `remind_after_s` gives the coordinator
// (A 10.5), keyed ReminderKey.
type Reminder struct {
	Key, QuestionID string
	N               int64
	Headline        string
}

// ReminderKey is a question's reminder key, `<question event ID>~humans`.
func ReminderKey(questionID string) string { return TimeoutKey(questionID, HumansWord) }

// QuestionIndex is the questions room read in stream order (A 6.5, 10.4): it numbers the
// questions, tracks their threads and states, and attaches each human post to a question or
// to none. It holds no session state, so a replacement coordinator reading the same room
// builds the same index.
type QuestionIndex struct {
	server    string
	floor     int64
	highest   int64
	questions map[int64]*IndexedQuestion
	roots     map[string]int64
	threads   map[string]int64
	humans    map[string]int64
}

// NewQuestionIndex is an empty index counting from the roster's `question_floor`.
func NewQuestionIndex(server string, floor int64) *QuestionIndex {
	return &QuestionIndex{
		server: server, floor: floor, highest: floor,
		questions: map[int64]*IndexedQuestion{}, roots: map[string]int64{},
		threads: map[string]int64{}, humans: map[string]int64{},
	}
}

// Next is the number the coordinator's next question takes: 1 plus the higher of the floor
// and the highest counted.
func (x *QuestionIndex) Next() int64 { return x.highest + 1 }

// Question is a counted question by its number.
func (x *QuestionIndex) Question(n int64) (IndexedQuestion, bool) {
	if q, ok := x.questions[n]; ok {
		return *q, true
	}
	return IndexedQuestion{}, false
}

// Attachment is the question a human post was attached to (0 for none), and whether the
// index has seen the post.
func (x *QuestionIndex) Attachment(post string) (int64, bool) {
	n, ok := x.humans[post]
	return n, ok
}

// Feed takes a verdict of outcome Index: a question that is counted, or a thread post that
// applies to a counted question. It says whether the index changed.
func (x *QuestionIndex) Feed(v Verdict) bool {
	if v.Outcome != Index {
		return false
	}
	if v.Content.Key == KeyQuestion {
		return x.count(v)
	}
	return x.threadPost(v)
}

// Attach takes a human post delivered in questions and attaches it by the first rule of A
// 10.4 that holds; the question it is attached to, or false for none.
func (x *QuestionIndex) Attach(v Verdict) (IndexedQuestion, bool) {
	if v.Outcome != Deliver || v.Kind != KindHuman {
		return IndexedQuestion{}, false
	}
	h := v.Human
	n := x.attachment(h)
	x.humans[v.Event.ID] = n
	q, ok := x.questions[n]
	if !ok {
		return IndexedQuestion{}, false
	}
	q.Attached++
	q.Unanswered++
	if h.ThreadRoot == q.EventID {
		x.threads[v.Event.ID] = n
		q.Latest = v.Event.ID
	}
	return *q, true
}

// OpenDuplicate is the question not yet closed whose `Question:` section equals question,
// white space collapsed (A 10.2's `duplicate`).
func (x *QuestionIndex) OpenDuplicate(question string) (int64, bool) {
	want := collapseSpace(question)
	for _, n := range x.numbers() {
		if q := x.questions[n]; !q.Closed && q.Question == want {
			return n, true
		}
	}
	return 0, false
}

// Reminders are the reminders due at now (A 10.5): each open question with `remind_after_s`
// whose bound and the 30 s grace have passed, in question order. The caller delivers each
// once, by its key.
func (x *QuestionIndex) Reminders(nowMs int64) []Reminder {
	var due []Reminder
	for _, n := range x.numbers() {
		q := x.questions[n]
		if q.RemindAfterS > 0 && q.State() == QuestionOpen && TimeoutDue(q.OriginTs+q.RemindAfterS*1000, nowMs) {
			due = append(due, Reminder{Key: ReminderKey(q.EventID), QuestionID: q.EventID, N: n, Headline: q.Headline})
		}
	}
	return due
}

// count is A 6.5's numbering: an agent's question counts only at the next number, the admin's
// (a carried open v1 question) whatever its number, raising the highest; a number already
// counted, or a body that fails the form, is not counted.
func (x *QuestionIndex) count(v Verdict) bool {
	c := v.Content.Question
	headline, question, err := QuestionForm(c.N, v.Content.Body)
	_, taken := x.questions[c.N]
	admin := ClassifyUserID(v.Event.Sender, x.server) == IDAdmin
	if err != nil || taken || !admin && c.N != x.Next() {
		return false
	}
	x.highest = max(x.highest, c.N)
	x.questions[c.N] = &IndexedQuestion{
		N: c.N, EventID: v.Event.ID, Sender: v.Event.Sender, OriginTs: v.Event.OriginTs,
		Headline: headline, Question: collapseSpace(question), RemindAfterS: c.RemindAfterS, Latest: v.Event.ID,
	}
	x.roots[v.Event.ID] = c.N
	return true
}

// threadPost is an agent's post in a counted question's thread: it is the thread's latest,
// leaves no human post unanswered, and may close the question. A reply to an unattached post
// (no `n`) and a post rooted elsewhere change nothing.
func (x *QuestionIndex) threadPost(v Verdict) bool {
	t := v.Content.Thread
	q, ok := x.questions[t.N]
	if !ok || v.Content.Relation.Root != q.EventID {
		return false
	}
	x.threads[v.Event.ID] = t.N
	q.Latest, q.Unanswered = v.Event.ID, 0
	q.Closed = q.Closed || t.Close
	return true
}

// attachment is A 10.4's rules 1-5. An edit is attached by rule 4 alone: its original's
// question, else its new body's `Q<n>`.
func (x *QuestionIndex) attachment(h Human) int64 {
	if h.Revises != "" {
		if n := x.humans[h.Revises]; n != 0 {
			return n
		}
		return x.byPrefix(h.Body)
	}
	if n, ok := x.roots[h.ThreadRoot]; ok {
		return n
	}
	if n, ok := x.roots[h.ReplyTo]; ok {
		return n
	}
	if n, ok := x.threads[h.ReplyTo]; ok {
		return n
	}
	return x.byPrefix(h.Body)
}

func (x *QuestionIndex) byPrefix(body string) int64 {
	m := questionPrefixRe.FindStringSubmatch(body)
	if m == nil {
		return 0
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if _, counted := x.questions[n]; err != nil || !counted {
		return 0
	}
	return n
}

func (x *QuestionIndex) numbers() []int64 {
	ns := make([]int64, 0, len(x.questions))
	for n := range x.questions {
		ns = append(ns, n)
	}
	slices.Sort(ns)
	return ns
}
