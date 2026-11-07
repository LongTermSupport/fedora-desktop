package proto

import (
	"maps"
	"slices"
)

// The kinds of human post the index remembers by event ID (QX6's 1024 most recent): one that no
// question's thread holds, and one in a question's thread, which A 10.4 rule 2 attaches a reply
// to as it does an agent's thread post.
const (
	postHuman       = 0
	postHumanThread = 1
)

// PostEntry is one human post the index has folded: its event ID, the question it was attached
// to (0 for none) and its kind.
type PostEntry struct {
	ID string `json:"id"`
	Q  int64  `json:"q"`
	T  int    `json:"t"`
}

func (e PostEntry) eventID() string { return e.ID }

// AgentPost is one agent event the index has folded by event ID (QX6's second list, OW5 (b)):
// an agent's thread post in question Q, which A 10.4 rule 2 attaches a reply to, or, with Q 0,
// a redacted event whose number a question may have held (a slot). Each is remembered so that
// the event fed again applies once: a thread post resets its question's unanswered count and
// may close it, and a slot widens Next().
type AgentPost struct {
	ID string `json:"id"`
	Q  int64  `json:"q"`
}

func (e AgentPost) eventID() string { return e.ID }

// foldedEntry is an entry of one of the index's two lists of folded events.
type foldedEntry interface{ eventID() string }

// ring is the newest limit entries of a list, oldest evicted first, in stream order.
type ring[E foldedEntry] struct {
	limit int
	byID  map[string]E
	order []string
	head  int
}

func newRing[E foldedEntry](limit int) ring[E] { return ring[E]{limit: limit, byID: map[string]E{}} }

func (r *ring[E]) get(id string) (E, bool) {
	e, ok := r.byID[id]
	return e, ok
}

func (r *ring[E]) add(e E) {
	r.byID[e.eventID()] = e
	r.order = append(r.order, e.eventID())
	for len(r.order)-r.head > r.limit {
		delete(r.byID, r.order[r.head])
		r.head++
	}
	if r.head >= r.limit {
		r.order, r.head = slices.Clone(r.order[r.head:]), 0
	}
}

func (r *ring[E]) entries() []E {
	out := make([]E, 0, len(r.order)-r.head)
	for _, id := range r.order[r.head:] {
		out = append(out, r.byID[id])
	}
	return out
}

func (r *ring[E]) clone() ring[E] {
	c := ring[E]{limit: r.limit, byID: make(map[string]E, len(r.byID)), order: slices.Clone(r.order[r.head:])}
	maps.Copy(c.byID, r.byID)
	return c
}

// IndexState is the bounded state of a QuestionIndex that the store keeps (QX5, QX6): the
// questions by number, the human posts, the agent posts and slots and the unattached posts
// oldest first, and the three counters that make Next(). The coordinator's own placed entries
// are never in it.
type IndexState struct {
	Highest    int64             `json:"highest"`
	Slots      int64             `json:"slots"`
	Evicted    int64             `json:"evicted"`
	Questions  []IndexedQuestion `json:"questions"`
	Posts      []PostEntry       `json:"posts"`
	AgentPosts []AgentPost       `json:"agent_posts"`
	Unattached []UnattachedPost  `json:"unattached"`
}

// State is the index's bounded state, a copy.
func (x *QuestionIndex) State() IndexState {
	ns := x.foldedNumbers()
	qs := make([]IndexedQuestion, 0, len(ns))
	for _, n := range ns {
		qs = append(qs, x.questions[n].clone())
	}
	return IndexState{
		Highest: x.highest, Slots: x.slots, Evicted: x.evicted,
		Questions: qs, Posts: x.posts.entries(), AgentPosts: x.agents.entries(), Unattached: cloneList(x.unattached),
	}
}

// Copy is an independent copy of the index, for judging an answer's events before its store
// write is made (QX4's commit rule); the copy carries the coordinator's own placed entries.
func (x *QuestionIndex) Copy() *QuestionIndex {
	c := &QuestionIndex{
		server: x.server, highest: x.highest, slots: x.slots, evicted: x.evicted,
		questions: make(map[int64]*IndexedQuestion, len(x.questions)), roots: make(map[string]int64, len(x.roots)),
		posts: x.posts.clone(), agents: x.agents.clone(), unattached: slices.Clone(x.unattached), dropped: slices.Clone(x.dropped),
	}
	for n, q := range x.questions {
		cq := q.clone()
		c.questions[n] = &cq
	}
	maps.Copy(c.roots, x.roots)
	for _, p := range x.prov {
		c.prov = append(c.prov, p.clone())
	}
	return c
}

func (x *QuestionIndex) foldedNumbers() []int64 {
	ns := make([]int64, 0, len(x.questions))
	for n := range x.questions {
		ns = append(ns, n)
	}
	slices.Sort(ns)
	return ns
}

// foldResult is what one agent event did to the index.
type foldResult int

const (
	notCounted foldResult = iota
	counted
	repeated
)

// Feed takes a verdict of outcome Index: a question that is counted, or a thread post that
// applies to a counted question. It says whether the index changed; an event already folded
// changes nothing (G1.1).
func (x *QuestionIndex) Feed(v Verdict) bool {
	if v.Outcome != Index {
		return false
	}
	var r foldResult
	if v.Content.Key == KeyQuestion {
		r = x.count(v)
	} else {
		r = x.threadPost(v)
	}
	x.echoed(v, r)
	return r == counted
}

// count is A 6.5's numbering: an agent's question counts only from 1 plus the highest counted
// to Next() (the redacted slots widen that window, QX7), the admin's (a carried open v1
// question) whatever its number, raising the highest; a number already counted, or a body that
// fails the form, is not counted.
func (x *QuestionIndex) count(v Verdict) foldResult {
	c := v.Content.Question
	if held := x.questions[c.N]; held != nil {
		if held.EventID == v.Event.ID {
			return repeated
		}
		return notCounted
	}
	headline, section, err := QuestionForm(c.N, v.Content.Body)
	admin := ClassifyUserID(v.Event.Sender, x.server) == IDAdmin
	if err != nil || !admin && (c.N <= x.highest || c.N > x.foldedNext()) {
		return notCounted
	}
	x.raise(c.N)
	x.questions[c.N] = &IndexedQuestion{
		N: c.N, EventID: v.Event.ID, Latest: v.Event.ID, Headline: headline, Hash: sectionHash(section),
		AskedBy: x.indexUsers(c.AskedBy), Re: cloneList(c.Re), AskedMs: v.Event.OriginTs,
		RemindAfterS: c.RemindAfterS, Unanswered: []string{},
	}
	x.roots[v.Event.ID] = c.N
	x.trim(false, MaxOpenQuestions)
	return counted
}

// indexUsers are the user IDs the index keeps of an `asked_by`: those of this server that pass
// the grammar within 128 bytes; another entry is not kept.
func (x *QuestionIndex) indexUsers(ids []string) []string {
	kept := []string{}
	for _, id := range ids {
		if x.validIndexUser(id) {
			kept = append(kept, id)
		}
	}
	return kept
}

// raise lifts the highest counted to n. Each number a counted question skips within the slots
// the redacted events made is held by an entry of its own: closed, with no event, headline or
// hash (QX7); the slots then start again at 0.
func (x *QuestionIndex) raise(n int64) {
	if n <= x.highest {
		return
	}
	for held := x.highest + 1; held < n && held <= x.highest+x.slots; held++ {
		x.questions[held] = &IndexedQuestion{N: held, Closed: true, AskedBy: []string{}, Re: []string{}, Unanswered: []string{}}
	}
	x.highest, x.slots = n, 0
	x.trim(true, MaxClosedQuestions)
}

// trim evicts the lowest-numbered questions that are (or are not) closed past limit; an
// evicted open question is counted (QX6).
func (x *QuestionIndex) trim(closed bool, limit int) {
	var ns []int64
	for _, n := range x.foldedNumbers() {
		if x.questions[n].Closed == closed {
			ns = append(ns, n)
		}
	}
	for ; len(ns) > limit; ns = ns[1:] {
		q := x.questions[ns[0]]
		delete(x.roots, q.EventID)
		delete(x.questions, ns[0])
		if !closed {
			x.evicted++
		}
	}
}

// threadPost is an agent's post in a counted question's thread: it is the thread's latest,
// leaves no human post unanswered, and may close the question. A reply with no `n` (to an
// unattached human post) marks that post replied to, and a post rooted elsewhere changes
// nothing.
func (x *QuestionIndex) threadPost(v Verdict) foldResult {
	t := v.Content.Thread
	if t.N == 0 {
		return x.plainReply(v.Content.Relation)
	}
	if _, seen := x.folded(v.Event.ID); seen {
		return repeated
	}
	q, ok := x.questions[t.N]
	if !ok || q.Redacted() || v.Content.Relation.Root != q.EventID {
		return notCounted
	}
	x.agents.add(AgentPost{ID: v.Event.ID, Q: t.N})
	q.Latest, q.Unanswered, q.UnansweredMore = v.Event.ID, []string{}, 0
	if t.Close && !q.Closed {
		q.Closed = true
		x.trim(true, MaxClosedQuestions)
	}
	return counted
}

// plainReply is an agent's reply to a human post (A 10.3): the post, if it is listed
// unattached, is not any more. A reply to a post that is attached, unknown or replied to
// already counts for nothing.
func (x *QuestionIndex) plainReply(r Relation) foldResult {
	i := slices.IndexFunc(x.unattached, func(u UnattachedPost) bool { return r.Kind == RelReply && u.ID == r.InReplyTo })
	if i < 0 {
		return notCounted
	}
	x.unattached = slices.Delete(x.unattached, i, i+1)
	return counted
}

// Attach takes a human post delivered in questions and attaches it by the first rule of A
// 10.4 that holds, whatever its delivery verdict (n3f-D18): the question it is attached to, or
// false for none. An event already folded, in either list, changes nothing and gives the same
// answer.
func (x *QuestionIndex) Attach(ev Event, h Human) (IndexedQuestion, bool) {
	if n, seen := x.folded(ev.ID); seen {
		return x.attached(n)
	}
	n := x.attachment(h)
	q := x.questions[n]
	entry := PostEntry{ID: ev.ID, Q: n}
	if q != nil && h.ThreadRoot == q.EventID {
		entry.T = postHumanThread
	}
	x.posts.add(entry)
	if q == nil {
		if x.validIndexUser(ev.Sender) {
			x.unattached = append(x.unattached, UnattachedPost{ID: ev.ID, From: ev.Sender, SentMs: ev.OriginTs})
			x.unattached = x.unattached[max(0, len(x.unattached)-MaxUnattached):]
		}
		return IndexedQuestion{}, false
	}
	q.Attached++
	if len(q.Unanswered) < MaxUnanswered {
		q.Unanswered = append(q.Unanswered, ev.ID)
	} else {
		q.UnansweredMore++
	}
	if entry.T == postHumanThread {
		q.Latest = ev.ID
	}
	return q.clone(), true
}

func (x *QuestionIndex) attached(n int64) (IndexedQuestion, bool) {
	if q := x.questions[n]; q != nil {
		return q.clone(), true
	}
	return IndexedQuestion{}, false
}

// attachment is A 10.4's rules 1-5. An edit is attached by rule 4 alone: its original's
// question, else its new body's `Q<n>`. A question that is no longer kept, or whose number a
// redacted event holds, takes no post: the post is attached to none.
func (x *QuestionIndex) attachment(h Human) int64 {
	if h.Revises != "" {
		if e, ok := x.posts.get(h.Revises); ok && x.live(e.Q) != 0 {
			return e.Q
		}
		return x.live(questionNumber(h.Body))
	}
	if n, ok := x.roots[h.ThreadRoot]; ok {
		return n
	}
	if n, ok := x.roots[h.ReplyTo]; ok {
		return n
	}
	if e, ok := x.posts.get(h.ReplyTo); ok && e.T == postHumanThread && x.live(e.Q) != 0 {
		return e.Q
	}
	if e, ok := x.agents.get(h.ReplyTo); ok && x.live(e.Q) != 0 {
		return e.Q
	}
	return x.live(questionNumber(h.Body))
}

// folded is whether the event is in either list of folded events (an event is in at most one),
// and the question its entry names.
func (x *QuestionIndex) folded(id string) (int64, bool) {
	if e, ok := x.posts.get(id); ok {
		return e.Q, true
	}
	e, ok := x.agents.get(id)
	return e.Q, ok
}

// live is n if a question that an event holds is kept at that number, else 0.
func (x *QuestionIndex) live(n int64) int64 {
	if q := x.questions[n]; q != nil && !q.Redacted() {
		return n
	}
	return 0
}
