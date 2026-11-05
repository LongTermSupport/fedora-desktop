package proto

import (
	"crypto/sha256"
	"encoding/hex"
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

// The caps of the index, applied by the fold in stream order so that every seat folding the same
// room holds the same state (SPEC-n3f QX6, n3f-D17).
const (
	MaxOpenQuestions   = 64
	MaxClosedQuestions = 128
	MaxUnanswered      = 16
	MaxAttachments     = 1024
	MaxUnattached      = 64
	maxIndexedUserID   = 128
)

// questionPrefixRe is rule 3 of A 10.4: `Q<n>` in any case, then a space, `:` or `.`.
var questionPrefixRe = regexp.MustCompile(`^[Qq]([1-9][0-9]{0,5})[ :.]`)

// IndexedQuestion is one counted question: its post, its form, and what its thread holds so
// far. Latest is the newest event in its thread (the question itself while the thread is
// empty), which a thread post's fallback reply names (A 10.3); Attached counts the human
// posts attached to it, and Unanswered holds the ids of those after the last agent post in its
// thread (P58), oldest first, the 16 oldest, UnansweredMore counting the rest. Hash is the
// SHA-256 of the `Question:` section, white space collapsed, never its text. A question no
// event holds, whose number a redacted event held (QX7), has no id, text or hash and is closed.
// Provisional is the coordinator's own question placed before its echo is folded (QX3); it is
// never stored. The JSON keys are the ones QX6's byte sums count.
type IndexedQuestion struct {
	N              int64    `json:"n"`
	EventID        string   `json:"id"`
	Latest         string   `json:"latest"`
	Headline       string   `json:"headline"`
	Hash           string   `json:"hash"`
	AskedBy        []string `json:"asked_by"`
	Re             []string `json:"re"`
	AskedMs        int64    `json:"asked_ms"`
	RemindAfterS   int64    `json:"remind_after_s"`
	Closed         bool     `json:"closed"`
	Attached       int64    `json:"attached"`
	Unanswered     []string `json:"unanswered"`
	UnansweredMore int64    `json:"unanswered_more"`
	Provisional    bool     `json:"-"`
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

// UnansweredCount is the human posts after the coordinator's last post in the thread.
func (q IndexedQuestion) UnansweredCount() int64 { return int64(len(q.Unanswered)) + q.UnansweredMore }

// Redacted is whether an admin's redaction holds this number: no event, no text.
func (q IndexedQuestion) Redacted() bool { return !q.Provisional && q.EventID == "" }

func (q IndexedQuestion) clone() IndexedQuestion {
	q.AskedBy, q.Re, q.Unanswered = cloneList(q.AskedBy), cloneList(q.Re), cloneList(q.Unanswered)
	return q
}

// cloneList is a copy of list that is never nil: the stored state writes an empty list as `[]`,
// and slices.Clone of a nil list is nil (null in JSON), which the codec refuses.
func cloneList[T any](list []T) []T { return append(make([]T, 0, len(list)), list...) }

// Reminder is the one TIMEOUT an open question with `remind_after_s` gives the coordinator
// (A 10.5), keyed ReminderKey; DueMs is the time it fell due, which the reminded watermark
// (n3f-D11) is a latest of.
type Reminder struct {
	Key, QuestionID string
	N               int64
	Headline        string
	DueMs           int64
}

// ReminderKey is a question's reminder key, `<question event ID>~humans`.
func ReminderKey(questionID string) string { return TimeoutKey(questionID, HumansWord) }

// UnattachedPost is a human post attached to no question (A 10.4 rule 5) that no agent post
// has replied to yet (A 10.3's index check).
type UnattachedPost struct {
	ID     string `json:"id"`
	From   string `json:"from_id"`
	SentMs int64  `json:"sent_ms"`
}

// QuestionIndex is the questions room read in stream order (A 6.5, 10.4): it numbers the
// questions, tracks their threads and states, and attaches each human post to a question or
// to none. It holds no session state, so a replacement coordinator reading the same room
// builds the same index. Every fold is idempotent by event id, so a page that overlaps the
// sync, an answer read again and an echo of an own post change nothing the second time.
type QuestionIndex struct {
	server     string
	highest    int64
	slots      int64
	evicted    int64
	questions  map[int64]*IndexedQuestion
	roots      map[string]int64
	posts      postRing
	unattached []UnattachedPost
	prov       []provisional
	dropped    []string
}

// NewQuestionIndex is an empty index counting from the roster's `question_floor`.
func NewQuestionIndex(server string, floor int64) *QuestionIndex {
	return &QuestionIndex{
		server: server, highest: floor,
		questions: map[int64]*IndexedQuestion{}, roots: map[string]int64{}, posts: newPostRing(),
	}
}

// Next is the number the coordinator's next question takes: past everything folded, every
// redacted slot, and every question of its own placed and not yet echoed.
func (x *QuestionIndex) Next() int64 {
	next := x.foldedNext()
	for _, p := range x.prov {
		if p.isQuestion {
			next = max(next, p.q.N+1)
		}
	}
	return next
}

// Open is the questions not closed, the coordinator's own placed ones included (QX6).
func (x *QuestionIndex) Open() int {
	open := 0
	for _, n := range x.numbers() {
		if q, held := x.Question(n); held && !q.Closed {
			open++
		}
	}
	return open
}

// Evicted is the questions not closed that an admin's carried question evicted past the cap.
func (x *QuestionIndex) Evicted() int64 { return x.evicted }

// Question is a counted question by its number, or the coordinator's own placed one; the
// coordinator's own placed thread posts are applied to it (QX3).
func (x *QuestionIndex) Question(n int64) (IndexedQuestion, bool) {
	if q, ok := x.questions[n]; ok {
		return x.overlay(q.clone()), true
	}
	for _, p := range x.prov {
		if p.isQuestion && p.q.N == n {
			return x.overlay(p.q.clone()), true
		}
	}
	return IndexedQuestion{}, false
}

// Attachment is the question a human post was attached to (0 for none), and whether the
// index has seen the post (A 10.3's index check). Only a human post is known by it, never an
// agent's thread post or a slot.
func (x *QuestionIndex) Attachment(post string) (int64, bool) {
	if e, ok := x.posts.get(post); ok && e.human() {
		return e.Q, true
	}
	return 0, false
}

// AttachmentOf is the question the rules of A 10.4 would attach the post to (0 for none),
// changing nothing.
func (x *QuestionIndex) AttachmentOf(h Human) int64 { return x.attachment(h) }

// Unattached are the human posts attached to no question that no agent post replied to,
// oldest first, at most the newest 64 (QX6); the coordinator's own placed replies are applied.
func (x *QuestionIndex) Unattached() []UnattachedPost {
	out := make([]UnattachedPost, 0, len(x.unattached))
	for _, u := range x.unattached {
		if !x.repliedProvisionally(u.ID) {
			out = append(out, u)
		}
	}
	return out
}

// OpenDuplicate is the question not yet closed whose `Question:` section equals question,
// white space collapsed (A 10.2's `duplicate`): the section is hashed, as the index keeps it.
func (x *QuestionIndex) OpenDuplicate(question string) (int64, bool) {
	want := sectionHash(question)
	for _, n := range x.numbers() {
		if q, held := x.Question(n); held && !q.Closed && q.Hash == want {
			return n, true
		}
	}
	return 0, false
}

// Reminders are the reminders due at now (A 10.5): each open question with `remind_after_s`
// whose bound and the 30 s grace have passed, in question order. The caller delivers each
// once, by its key. A question the coordinator placed and no echo has been folded for is not
// reminded.
func (x *QuestionIndex) Reminders(nowMs int64) []Reminder {
	var due []Reminder
	for _, n := range x.numbers() {
		q := x.questions[n]
		if q == nil || q.RemindAfterS <= 0 || q.State() != QuestionOpen {
			continue
		}
		deadline := q.AskedMs + q.RemindAfterS*1000
		if TimeoutDue(deadline, nowMs) {
			due = append(due, Reminder{
				Key: ReminderKey(q.EventID), QuestionID: q.EventID, N: n, Headline: q.Headline, DueMs: deadline + timeoutGraceMs,
			})
		}
	}
	return due
}

// Slot folds an event of an agent account or the admin that an admin's redaction emptied
// (RedactedSlot, QX7): a number a question may have held, which Next() now leaves out. An
// event already folded is none.
func (x *QuestionIndex) Slot(id string) bool {
	if _, folded := x.roots[id]; folded {
		return false
	}
	if _, seen := x.posts.get(id); seen {
		return false
	}
	x.posts.add(PostEntry{ID: id, T: postSlot})
	x.slots++
	return true
}

// foldedNext is 1 plus the highest counted plus the redacted slots since it was last raised
// (QX7): the top of the window an agent's question is counted in.
func (x *QuestionIndex) foldedNext() int64 { return x.highest + 1 + x.slots }

// numbers is every number the index holds, folded or placed, ascending.
func (x *QuestionIndex) numbers() []int64 {
	ns := make([]int64, 0, len(x.questions)+len(x.prov))
	for n := range x.questions {
		ns = append(ns, n)
	}
	for _, p := range x.prov {
		if _, folded := x.questions[p.q.N]; p.isQuestion && !folded {
			ns = append(ns, p.q.N)
		}
	}
	slices.Sort(ns)
	return ns
}

// sectionHash is the SHA-256 of a `Question:` section with its white space collapsed, in hex.
func sectionHash(section string) string {
	sum := sha256.Sum256([]byte(collapseSpace(section)))
	return hex.EncodeToString(sum[:])
}

// validIndexUser is a user ID of this server that is at most 128 bytes and passes the grammar,
// so the store never needs to escape it (QX6).
func (x *QuestionIndex) validIndexUser(id string) bool {
	return len(id) <= maxIndexedUserID && ClassifyUserID(id, x.server) != IDNone
}

// questionNumber is the number in a prefix `Q<n>`, 0 when the text has none.
func questionNumber(body string) int64 {
	m := questionPrefixRe.FindStringSubmatch(body)
	if m == nil {
		return 0
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0
	}
	return n
}
