package proto

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// The states of a question (Plan 00211 SPEC §8.4).
const (
	StateOpen     = "open"
	StateAnswered = "answered"
	StateClosed   = "closed"
)

const relatesKey = "m.relates_to"

var questionNumberPrefixRE = regexp.MustCompile(`^[Qq]([1-9][0-9]{0,5})[ :.]`)

// QuestionView is a question's state from the room (SPEC P58): Replies are the human messages
// and edits attached to it, Unanswered those after the last agent post in its thread.
type QuestionView struct {
	Question   *Question
	State      string
	Replies    []string
	Unanswered []string
}

type questionReply struct {
	pos int
	id  string
}

// QuestionIndex is the questions of a questions room, read from its messages oldest first
// (SPEC P58); it holds no session state, so any coordinator reads the same.
type QuestionIndex struct {
	byEvent  map[string]*Question
	byN      map[int64]*Question
	threadOf map[string]string
	attached map[string]string
	closed   map[int64]bool
	agentPos map[string]int
	replies  map[string][]questionReply
}

// BuildQuestionIndex is SPEC P58 over a questions room's events, oldest first. A thread root
// that is not a string is no root (D12).
func BuildQuestionIndex(events []any, ctx *Context) *QuestionIndex {
	x := &QuestionIndex{
		byEvent: map[string]*Question{}, byN: map[int64]*Question{}, threadOf: map[string]string{},
		attached: map[string]string{}, closed: map[int64]bool{}, agentPos: map[string]int{},
		replies: map[string][]questionReply{},
	}
	for pos, event := range events {
		x.add(pos, event, ctx)
	}
	return x
}

func relatesOf(content pyjson.Object) pyjson.Object {
	if rel, ok := content[relatesKey].(pyjson.Object); ok {
		return rel
	}
	return pyjson.Object{}
}

// threadRoot is the question a thread relation names, or "".
// closesQuestion is the number a valid closing state names (v == 1 by Python's equality).
func closesQuestion(content pyjson.Object) (int64, bool) {
	obj, ok := content[KeyQuestionState].(pyjson.Object)
	if !ok || !hasExactKeys(obj, "v", "n", "state") || !pyjson.EqualsInt(obj["v"], 1) ||
		obj["state"] != StateClosed {
		return 0, false
	}
	num, isNum := obj["n"].(pyjson.Number)
	n, fits := num.Int64()
	return n, isNum && fits
}

func messageContent(event any) (pyjson.Object, bool) {
	obj, ok := event.(pyjson.Object)
	if !ok {
		return nil, false
	}
	id, isID := obj[eventIDKey].(string)
	if !isID || !IsEventID(id) || !isString(obj["sender"]) || obj["type"] != roomMessageType {
		return nil, false
	}
	content, isObj := obj["content"].(pyjson.Object)
	return content, isObj
}

// Attach is SPEC P58's attachment of a human message or edit, by the first rule that holds:
// an edit by its new body's Q<n> prefix, else its original's question; its thread; the
// question or thread post it replies to; a Q<n> prefix of its body, the reply fallback removed.
func (x *QuestionIndex) Attach(event any) (*Question, bool) {
	content, ok := messageContent(event)
	if !ok {
		return nil, false
	}
	rel := relatesOf(content)
	if rel["rel_type"] == "m.replace" {
		return x.attachEdit(content, rel)
	}
	if root, isStr := rel[eventIDKey].(string); isStr && rel["rel_type"] == threadRelation && x.byEvent[root] != nil {
		return x.byEvent[root], true
	}
	if q, found := x.attachReply(rel); found {
		return q, true
	}
	body := content[bodyKey]
	if text, isStr := body.(string); isStr {
		if _, reply := rel["m.in_reply_to"]; reply {
			body = withoutReplyFallback(text)
		}
	}
	return x.byPrefix(body)
}

// withoutReplyFallback removes a reply's leading block of > lines and the one blank line after
// it (SPEC P37).
func withoutReplyFallback(text string) string {
	lines := strings.Split(text, "\n")
	i := 0
	for i < len(lines) && strings.HasPrefix(lines[i], ">") {
		i++
	}
	if i == 0 {
		return text
	}
	if i < len(lines) && lines[i] == "" {
		i++
	}
	return strings.Join(lines[i:], "\n")
}

// AttachedTo is the question a human message read by the index is attached to.
func (x *QuestionIndex) AttachedTo(eventID string) (*Question, bool) {
	q := x.byEvent[x.attached[eventID]]
	return q, q != nil
}

// Attached is the question event a human message read by the index is attached to, "" for
// none, and whether the index read it at all.
func (x *QuestionIndex) Attached(eventID string) (string, bool) {
	root, seen := x.attached[eventID]
	return root, seen
}

// ByEvent is the question whose root is the event.
func (x *QuestionIndex) ByEvent(eventID string) (*Question, bool) {
	q := x.byEvent[eventID]
	return q, q != nil
}

// ByNumber is the first question of the number.
func (x *QuestionIndex) ByNumber(n int64) (*Question, bool) {
	q := x.byN[n]
	return q, q != nil
}

// ThreadOf is the question event an agent or human post in a question's thread belongs to.
func (x *QuestionIndex) ThreadOf(eventID string) (string, bool) {
	root, ok := x.threadOf[eventID]
	return root, ok
}

// NextNumber is one more than the highest question number.
func (x *QuestionIndex) NextNumber() int64 {
	var highest int64
	for n := range x.byN {
		highest = max(highest, n)
	}
	return highest + 1
}

// Views is every question's view in number order.
func (x *QuestionIndex) Views() []QuestionView {
	numbers := make([]int64, 0, len(x.byN))
	for n := range x.byN {
		numbers = append(numbers, n)
	}
	slices.Sort(numbers)
	views := make([]QuestionView, 0, len(numbers))
	for _, n := range numbers {
		q := x.byN[n]
		last, posted := x.agentPos[q.EventID]
		if !posted {
			last = -1
		}
		view := QuestionView{Question: q, State: StateOpen, Replies: []string{}, Unanswered: []string{}}
		for _, r := range x.replies[q.EventID] {
			view.Replies = append(view.Replies, r.id)
			if r.pos > last {
				view.Unanswered = append(view.Unanswered, r.id)
			}
		}
		switch {
		case x.closed[n]:
			view.State = StateClosed
		case len(view.Replies) > 0:
			view.State = StateAnswered
		}
		views = append(views, view)
	}
	return views
}

// OpenDuplicate is the first open or answered question whose Question: section is the text,
// whitespace collapsed as Python's str.split() does.
func (x *QuestionIndex) OpenDuplicate(question string) (*Question, bool) {
	wanted := collapseSpace(question)
	for _, view := range x.Views() {
		if view.State != StateClosed && collapseSpace(view.Question.Question) == wanted {
			return view.Question, true
		}
	}
	return nil, false
}

func collapseSpace(s string) string { return strings.Join(strings.FieldsFunc(s, pySpace), " ") }

func (x *QuestionIndex) add(pos int, event any, ctx *Context) {
	id, sender, content, ok := messageParts(event)
	if !ok {
		return
	}
	if q, isQ := ParseQuestion(event, ctx.ServerName); isQ && x.byN[q.N] == nil {
		x.byEvent[id] = q
		x.byN[q.N] = q
		x.agentPos[id] = pos
		return
	}
	root := x.threadRoot(content)
	switch {
	case IsAgentUserID(sender, ctx.ServerName):
		if root != "" {
			x.threadOf[id] = root
			x.agentPos[root] = pos
			if n, closes := closesQuestion(content); closes && n == x.byEvent[root].N {
				x.closed[n] = true
			}
		}
	case ctx.IsHuman(sender):
		if root != "" {
			x.threadOf[id] = root
		}
		found, attached := x.Attach(event)
		x.attached[id] = ""
		if attached {
			x.attached[id] = found.EventID
			x.replies[found.EventID] = append(x.replies[found.EventID], questionReply{pos: pos, id: id})
		}
	}
}

func (x *QuestionIndex) threadRoot(content pyjson.Object) string {
	rel := relatesOf(content)
	root, ok := rel[eventIDKey].(string)
	if rel["rel_type"] != threadRelation || !ok || x.byEvent[root] == nil {
		return ""
	}
	return root
}

func (x *QuestionIndex) attachEdit(content, rel pyjson.Object) (*Question, bool) {
	var body any
	if replacement, ok := content["m.new_content"].(pyjson.Object); ok {
		body = replacement[bodyKey]
	}
	if q, found := x.byPrefix(body); found {
		return q, true
	}
	original, isStr := rel[eventIDKey].(string)
	if !isStr {
		return nil, false
	}
	return x.AttachedTo(original)
}

func (x *QuestionIndex) attachReply(rel pyjson.Object) (*Question, bool) {
	reply, ok := rel["m.in_reply_to"].(pyjson.Object)
	if !ok {
		return nil, false
	}
	target, isStr := reply[eventIDKey].(string)
	if !isStr {
		return nil, false
	}
	if q := x.byEvent[target]; q != nil {
		return q, true
	}
	q := x.byEvent[x.threadOf[target]]
	return q, q != nil
}

func (x *QuestionIndex) byPrefix(body any) (*Question, bool) {
	text, ok := body.(string)
	if !ok {
		return nil, false
	}
	m := questionNumberPrefixRE.FindStringSubmatch(text)
	if m == nil {
		return nil, false
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return nil, false
	}
	q := x.byN[n]
	return q, q != nil
}
