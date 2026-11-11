package teamsync

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// withIndexBuilder gives the team QX1's own builder.
func withIndexBuilder() option { return func(c *Config) { c.Builder = NewBuilder() } }

// builds is what a team's builder tells the test: each job's end, and each backoff wait.
type builds struct {
	ends  chan struct{}
	waits chan time.Duration
}

func watchBuilds(tm *Team) builds {
	b := builds{ends: make(chan struct{}, 16), waits: make(chan time.Duration, 16)}
	tm.onBuildEnd = func() { b.ends <- struct{}{} }
	tm.onBuildWait = func(d time.Duration) { b.waits <- d }
	return b
}

func (b builds) ended(t *testing.T) {
	t.Helper()
	select {
	case <-b.ends:
	case <-t.Context().Done():
		t.Fatal("the build never ended")
	}
}

func (b builds) waited(t *testing.T) time.Duration {
	t.Helper()
	select {
	case d := <-b.waits:
		return d
	case <-b.ends:
		t.Fatal("the build ended with no backoff wait")
	case <-t.Context().Done():
		t.Fatal("no backoff wait")
	}
	return 0
}

// seeded numbers the transactions seed uses, so that no two seeds share one.
var seeded atomic.Int64

// seed posts n of local's human posts in room through fakehs in process, which is quicker than
// HTTP for a room of thousands, and gives their ids oldest first.
func (w *world) seed(room, local string, n int) []string {
	w.t.Helper()
	ids := make([]string, 0, n)
	path := "/_matrix/client/v3/rooms/" + url.PathEscape(room) + "/send/" + proto.MessageType + "/"
	for i := range n {
		body, err := json.Marshal(human(fmt.Sprintf("history %d", i)))
		check(w.t, err)
		status, out := w.srv.Handle(w.t.Context(), http.MethodPut, path+fmt.Sprintf("seed%d", seeded.Add(1)), nil, body, w.tokens[local])
		id := eventID(out)
		if status != http.StatusOK || id == "" {
			w.t.Fatalf("seed %d: %d %s", i, status, out)
		}
		ids = append(ids, id)
	}
	return ids
}

// roomEvents counts every event of room, as alice reads it.
func (w *world) roomEvents(room string) int {
	w.t.Helper()
	page, err := w.client(alice).Messages(w.t.Context(), room, matrix.MessagesRequest{Dir: matrix.Backward, Limit: 1 << 20})
	check(w.t, err)
	return len(page.Chunk)
}

// pagesBound is QX1's bound on a build's requests over e events, 2 x ceil(e/100) + 1.
func pagesBound(e int) int { return 2*((e+pageSize-1)/pageSize) + 1 }

// teamReads are the member's /messages requests of room, oldest first.
func (w *world) teamReads(room string) []fakehs.Request {
	var out []fakehs.Request
	for _, r := range w.srv.Requests() {
		path, err := url.PathUnescape(r.Path)
		check(w.t, err)
		if strings.Contains(path, room) && strings.HasSuffix(path, "/messages") && r.Header.Get("Authorization") == "Bearer "+w.tokens[w.self] {
			out = append(out, r)
		}
	}
	return out
}

func readsIn(reads []fakehs.Request, dir string) int {
	n := 0
	for _, r := range reads {
		if r.Query.Get("dir") == dir {
			n++
		}
	}
	return n
}

// gateQuestions holds the questions room's /messages requests.
func (w *world) gateQuestions() *fakehs.Gate {
	g := w.srv.Gate(http.MethodGet, url.PathEscape(w.qroom())+"/messages")
	w.t.Cleanup(g.Open)
	return g
}

// storeNow is the engine's store as it is.
func (w *world) storeNow() state.Store {
	w.t.Helper()
	s, err := w.eng.Load(w.team, user(w.self))
	check(w.t, err)
	return s
}

// atTheHead is the stored key of an index at the head: no build record, its state, token and
// last event, the last being last unless last is "".
func (w *world) atTheHead(last string) *proto.QuestionIndex {
	w.t.Helper()
	k := w.storeNow().Questions
	if k == nil || k.Build != nil || k.Index == nil || k.Token == nil || k.Last == nil {
		w.t.Fatalf("the key %+v is not an index at the head", k)
	}
	if last != "" && *k.Last != last {
		w.t.Errorf("the key's last folded event %s, want %s", *k.Last, last)
	}
	idx, err := proto.LoadQuestionIndex(server, 0, *k.Index)
	check(w.t, err)
	return idx
}

// historyRoom is a questions room holding n of alice's posts made before the coordinator
// joined, so none of them is the coordinator's item (QS5), and gives their ids and the count of
// the room's events (counted before the posts: a read of a large room is slow in fakehs).
func (w *world) historyRoom(n int) ([]string, int) {
	w.t.Helper()
	w.asCoordinator()
	w.inQuestions(alice, false)
	before := w.roomEvents(w.qroom())
	posts := w.seed(w.qroom(), alice, n)
	w.inQuestions(coord, false)
	w.clk.StepWall(1000)
	return posts, before + n + 1
}

// QX1: a room of 250 events is built backwards in three pages of events, the last holding the
// room's start, and forwards in three to the head; the requests are within 2 x ceil(E/100) + 1,
// the last write holds an index at the head with no build record, the room is ready, and a
// build begun with no key owes the ready line.
func TestQX1ARoomOf250EventsIsBuiltInThreePagesEachWay(t *testing.T) {
	w := newWorld(t)
	w.asCoordinator()
	w.inQuestions(alice, false)
	w.inQuestions(coord, false)
	posts := w.seed(w.qroom(), alice, 250-w.roomEvents(w.qroom()))
	w.clk.StepWall(1000)
	if e := w.roomEvents(w.qroom()); e != 250 {
		t.Fatalf("the room holds %d events, want 250", e)
	}
	tm := w.newTeam(withIndexBuilder())
	b := watchBuilds(tm)
	w.startTeam(tm)
	b.ended(t)
	reads := w.teamReads(w.qroom())
	// fakehs, as Tuwunel, ends a backward page with no `end` only when it holds no event: the
	// fourth backward read finds the start, the third forward one the head.
	if back, fwd := readsIn(reads, "b"), readsIn(reads, "f"); back != 4 || fwd != 3 || back+fwd != pagesBound(250) {
		t.Errorf("%d backward and %d forward reads, want 4 and 3 (three pages of events each way), %d in all", back, fwd, pagesBound(250))
	}
	idx := w.atTheHead(posts[len(posts)-1])
	for _, id := range []string{posts[0], posts[100], posts[len(posts)-1]} {
		if _, ok := idx.Attachment(id); !ok {
			t.Errorf("the post %s is not in the built index", id)
		}
	}
	if !w.storeNow().Outbox.QuestionsHandover {
		t.Error("a build begun with no key did not owe the ready line")
	}
	if st := tm.QuestionsStatus(); st.State != team.QuestionsReady {
		t.Errorf("status %+v after the build, want ready", st)
	}
}

// QX1: a build is never on the delivery path: over 10 500 events, each questions page held by
// fakehs until a main item posted meanwhile is in the store, every one of them is delivered while
// the build runs, and the build keeps within its request bound.
func TestQX1ABuildOver10500EventsDeliversMainsItemsDuringIt(t *testing.T) {
	w := newWorld(t)
	posts, e := w.historyRoom(10500)
	g := w.gateQuestions()
	tm := w.newTeam(withIndexBuilder())
	b := watchBuilds(tm)
	r := w.startTeam(tm)
	reads := 0
	for done := false; !done; {
		select {
		case <-g.Arrived():
			reads++
			if st := tm.QuestionsStatus(); st.State != team.QuestionsBuilding {
				t.Fatalf("status %+v while a page is held, want building", st)
			}
			w.clk.StepWall(61_000)
			w.mainItemIn(r, w.say(alice, human(fmt.Sprintf("status %d?", reads), user(coord))))
			g.Pass()
		case <-r.polls:
		case <-w.eng.batches:
		case <-b.ends:
			done = true
		case <-t.Context().Done():
			t.Fatal("the build never ended")
		}
	}
	if reads > pagesBound(e) {
		t.Errorf("%d reads over %d events, past the bound %d", reads, e, pagesBound(e))
	}
	if st := tm.QuestionsStatus(); st.State != team.QuestionsReady {
		t.Errorf("status %+v after the build, want ready", st)
	}
	// The room's newest event is not read back here: fakehs reads a room of this size slowly.
	idx := w.atTheHead("")
	if _, ok := idx.Attachment(posts[len(posts)-1]); !ok {
		t.Error("the newest post is not in the index")
	}
}

// mainItemIn waits for the store write that holds main's item id, the builder's page writes and
// the team's long-polls passing meanwhile.
func (w *world) mainItemIn(r *running, id string) {
	w.t.Helper()
	for {
		select {
		case b := <-w.eng.batches:
			if slices.Contains(itemIDs(b), id) {
				return
			}
		case <-r.polls:
		case <-w.t.Context().Done():
			w.t.Fatalf("main's item %s never reached the store", id)
		}
	}
}

// QX1: a page that fails transiently is tried again under the backoff (1 s, then 2 s), and the
// pages already folded are kept: no backward page is read twice.
func TestQX1APageFailingTransientlyTwiceIsTriedAgainWithThePagesKept(t *testing.T) {
	w := newWorld(t)
	posts, e := w.historyRoom(150)
	if got := w.roomEvents(w.qroom()); got != e {
		t.Fatalf("the room holds %d events, historyRoom counted %d", got, e)
	}
	g := w.gateQuestions()
	tm := w.newTeam(withIndexBuilder())
	b := watchBuilds(tm)
	w.startTeam(tm)
	// The backward read (two pages and the start) passes; the first forward page fails.
	back := (e+pageSize-1)/pageSize + 1
	for range back {
		check(t, g.Held(t.Context()))
		g.Pass()
	}
	check(t, g.Held(t.Context()))
	w.srv.Inject(fakehs.Fault{Method: http.MethodGet, Path: "/messages", Status: http.StatusBadGateway, Body: []byte(`{}`), Times: 2})
	g.Pass()
	for _, want := range []time.Duration{time.Second, 2 * time.Second} {
		if d := b.waited(t); d != want {
			t.Errorf("backoff %v, want %v", d, want)
		}
		w.clk.Advance(want)
		check(t, g.Held(t.Context()))
		g.Pass()
	}
	g.Open()
	b.ended(t)
	reads := w.teamReads(w.qroom())
	if got := readsIn(reads, "b"); got != back {
		t.Errorf("%d backward reads over %d events, want %d: a page already folded was read again", got, e, back)
	}
	if len(reads) != pagesBound(e)+2 {
		t.Errorf("%d reads, want the bound %d and the two that failed", len(reads), pagesBound(e))
	}
	idx := w.atTheHead(w.newest(w.qroom()))
	if _, ok := idx.Attachment(posts[len(posts)-1]); !ok {
		t.Error("the newest post is not in the index")
	}
}

// newest is room's newest event.
func (w *world) newest(room string) string {
	w.t.Helper()
	page, err := w.client(alice).Messages(w.t.Context(), room, matrix.MessagesRequest{Dir: matrix.Backward, Limit: 1})
	check(w.t, err)
	if len(page.Chunk) != 1 {
		w.t.Fatal("the room holds no event")
	}
	return eventID(page.Chunk[0])
}
