package proto

import (
	"maps"
	"slices"
	"strconv"
	"testing"
)

// splitmix is a small deterministic generator (Knuth's 64-bit LCG, its high bits): a seed names
// one room for ever, whatever the Go release.
type splitmix struct{ state int }

func (r *splitmix) intN(n int) int {
	if n <= 0 {
		panic("splitmix: n is not positive")
	}
	r.state = r.state*6364136223846793005 + 1442695040888963407
	return (r.state >> 33 & 0x7fffffff) % n
}

// streamEvent is one event of a generated questions room: the event's id, and what folding it
// into an index does (an agent's post is fed, a human's attached, a redacted event a slot).
type streamEvent struct {
	id    string
	apply func(x *QuestionIndex)
}

type askedQuestion struct {
	n  int64
	id string
}

// roomGen generates a questions room from one seed: questions, thread posts, closes, plain
// replies, human posts of every attachment rule, edits and slots, in proportions that overflow
// every cap of QX6. It tracks the numbers the index counts, so most questions are counted.
type roomGen struct {
	rng     *splitmix
	events  []streamEvent
	highest int64
	slots   int64
	asked   []askedQuestion
	humans  []string
	posts   []string
	hot     askedQuestion
}

func (g *roomGen) id() string { return eid(20000 + len(g.events)) }

func (g *roomGen) add(id string, apply func(x *QuestionIndex)) {
	g.events = append(g.events, streamEvent{id: id, apply: apply})
}

func (g *roomGen) pick(n int) int { return g.rng.intN(n) }

// recent is one of the last 40 questions asked.
func (g *roomGen) recent() askedQuestion {
	from := max(0, len(g.asked)-40)
	return g.asked[from+g.pick(len(g.asked)-from)]
}

func (g *roomGen) ask() {
	id, n := g.id(), g.highest+1+int64(g.pick(int(g.slots)+1))
	if g.pick(30) == 0 && g.highest > 3 {
		n = g.highest - 1 // not counted: the number is taken
	} else {
		g.highest, g.slots = n, 0
		g.asked = append(g.asked, askedQuestion{n, id})
	}
	q := Question{N: n}
	if g.pick(3) == 0 {
		q = Question{N: n, AskedBy: []string{aliceID}, Re: []string{eid(1)}, RemindAfterS: 600}
	}
	g.add(id, func(x *QuestionIndex) { x.Feed(askVerdict(id, coordID, 1000+n, q, questionBody(n))) })
}

func (g *roomGen) admin() {
	id, n := g.id(), g.highest+1+int64(g.pick(4))
	g.highest, g.slots = n, 0
	g.asked = append(g.asked, askedQuestion{n, id})
	g.add(id, func(x *QuestionIndex) { x.Feed(askVerdict(id, adminID, 1000+n, Question{N: n}, questionBody(n))) })
}

func (g *roomGen) thread() {
	id, q := g.id(), g.recent()
	closes := g.pick(5) < 2
	g.posts = append(g.posts, id)
	g.add(id, func(x *QuestionIndex) { x.Feed(threadVerdict(id, coordID, q.id, q.n, closes)) })
}

func (g *roomGen) reply() {
	id, to := g.id(), g.humans[len(g.humans)-1-g.pick(min(len(g.humans), 150))]
	g.add(id, func(x *QuestionIndex) {
		x.Feed(Verdict{
			Outcome: Index, Event: Event{ID: id, Type: MessageType, Sender: coordID, OriginTs: 1},
			Content: Content{Key: KeyThread, Relation: ReplyTo(to)},
		})
	})
}

func (g *roomGen) slot() {
	id := g.id()
	g.slots++
	g.add(id, func(x *QuestionIndex) { x.Slot(id) })
}

// human is a human post that one of A 10.4's rules, or none, attaches.
func (g *roomGen) human() {
	id := g.id()
	sender := UserID("u"+strconv.Itoa(g.pick(30)), testServer)
	var h Human
	switch kind := g.pick(20); {
	case kind < 6:
		h = Human{Body: "in a thread", ThreadRoot: g.hotOrRecent().id}
	case kind < 7:
		h = Human{Body: "in an old thread", ThreadRoot: g.asked[g.pick(len(g.asked))].id}
	case kind < 9 && len(g.posts) > 0:
		h = Human{Body: "to a post", ReplyTo: g.posts[len(g.posts)-1-g.pick(min(len(g.posts), 20))]}
	case kind < 13:
		h = Human{Body: "Q" + strconv.FormatInt(g.recent().n+int64(g.pick(3)), 10) + " by name"}
	case kind < 14 && len(g.humans) > 0:
		h = Human{Body: "* fixed", Revises: g.humans[len(g.humans)-1-g.pick(min(len(g.humans), 30))]}
	default:
		h = Human{Body: "unrelated"}
	}
	g.humans = append(g.humans, id)
	g.add(id, func(x *QuestionIndex) { x.Attach(humanEvent(id, sender, 1), h) })
}

// hotOrRecent is the question that most human posts reach for a while, so that its unanswered
// list passes 16.
func (g *roomGen) hotOrRecent() askedQuestion {
	if g.pick(3) == 0 {
		return g.recent()
	}
	return g.hot
}

func generateRoom(seed, size int) []streamEvent {
	g := &roomGen{rng: &splitmix{state: seed}}
	g.ask()
	g.hot = g.asked[0]
	g.human()
	for len(g.events) < size {
		if len(g.events)%300 == 0 {
			g.hot = g.asked[len(g.asked)-1]
		}
		switch r := g.pick(100); {
		case r < 11:
			g.ask()
		case r < 12:
			g.admin()
		case r < 25:
			g.thread()
		case r < 33:
			g.reply()
		case r < 36:
			g.slot()
		default:
			g.human()
		}
	}
	return g.events
}

// summary is what a cap leaves visible in an index's state.
type summary struct {
	open, closed, unattached, posts int
	evicted                         int64
	lowestClosed                    int64
	firstPost, lastUnattached       string
	more                            bool
}

func summarize(x *QuestionIndex) summary {
	st := x.State()
	s := summary{evicted: st.Evicted, unattached: len(st.Unattached), posts: len(st.Posts)}
	for _, q := range st.Questions {
		if q.Closed {
			s.closed++
			if s.lowestClosed == 0 {
				s.lowestClosed = q.N
			}
		} else {
			s.open++
		}
		s.more = s.more || q.UnansweredMore > 0
	}
	if len(st.Posts) > 0 {
		s.firstPost = st.Posts[0].ID
	}
	if len(st.Unattached) > 0 {
		s.lastUnattached = st.Unattached[len(st.Unattached)-1].ID
	}
	return s
}

// firedCaps are the caps of QX6 that the step from before to after applied: an open question
// evicted, the lowest closed one evicted, the 17th unanswered post counted, the oldest post of
// 1024 forgotten, the oldest of 64 unattached posts forgotten.
func firedCaps(before, after summary) []string {
	var fired []string
	if after.evicted > before.evicted {
		fired = append(fired, "open")
	}
	if before.closed == MaxClosedQuestions && after.lowestClosed > before.lowestClosed {
		fired = append(fired, "closed")
	}
	if after.more {
		fired = append(fired, "unanswered")
	}
	if before.posts == MaxAttachments && after.posts == MaxAttachments && after.firstPost != before.firstPost {
		fired = append(fired, "posts")
	}
	if before.unattached == MaxUnattached && after.unattached == MaxUnattached && after.lastUnattached != before.lastUnattached {
		fired = append(fired, "unattached")
	}
	return fired
}

// pageSizes cuts n events into pages of 60 to 459.
func pageSizes(rng *splitmix, n int) []int {
	var sizes []int
	for n > 0 {
		size := min(n, 60+rng.intN(400))
		sizes = append(sizes, size)
		n -= size
	}
	return sizes
}

// restart is what a seat does between two pages: the index's state is stored in the questions
// key and read back by a link that starts again.
func restart(t *testing.T, x *QuestionIndex, last string) *QuestionIndex {
	t.Helper()
	st := x.State()
	data, err := QuestionsState{Index: &st, Token: new(codecNextBatch), Last: &last}.Encode(testServer)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	back, err := DecodeQuestionsState(data, testServer)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	loaded, err := LoadQuestionIndex(testServer, 0, *back.Index)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return loaded
}

// TestQuestionIndexLiveEqualsRebuilt is QX6's and QX7's property: one index that folds a room
// as it comes, and one that is stored, read back and loaded between pages, with the last few
// events of every page folded again as a catch-up's overlap is, hold the same state after
// every page. Every seed's room fires every cap.
func TestQuestionIndexLiveEqualsRebuilt(t *testing.T) {
	for _, seed := range []int{1, 2, 3} {
		t.Run("seed "+strconv.Itoa(seed), func(t *testing.T) {
			fired := foldLiveAndRestarted(t, generateRoom(seed, 2600), &splitmix{state: seed + 1000})
			for _, name := range []string{"open", "closed", "unanswered", "posts", "unattached"} {
				if !fired[name] {
					t.Errorf("the %s cap never fired: the room does not test it (fired %v)", name, slices.Sorted(maps.Keys(fired)))
				}
			}
		})
	}
}

// foldLiveAndRestarted folds the room into a live index and into one that restarts after every
// page, and holds their states equal after each; it gives the caps the live fold fired.
func foldLiveAndRestarted(t *testing.T, room []streamEvent, rng *splitmix) map[string]bool {
	t.Helper()
	live, restarted := NewQuestionIndex(testServer, 0), NewQuestionIndex(testServer, 0)
	fired := map[string]bool{}
	before, from := summarize(live), 0
	for _, size := range pageSizes(rng, len(room)) {
		page := room[from : from+size]
		for _, ev := range page {
			ev.apply(live)
			after := summarize(live)
			for _, name := range firedCaps(before, after) {
				fired[name] = true
			}
			before = after
			ev.apply(restarted)
		}
		restarted = restart(t, restarted, page[len(page)-1].id)
		for _, ev := range page[max(0, len(page)-rng.intN(7)):] {
			ev.apply(restarted)
		}
		if want, got := stateJSON(t, live), stateJSON(t, restarted); got != want {
			t.Fatalf("after events %d to %d the restarted index differs from the live one:\nlive      %s\nrestarted %s", from, from+size, want, got)
		}
		from += size
	}
	return fired
}
