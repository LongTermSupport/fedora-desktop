package teamsync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// fakeBuilder is QX1's seam: it records what the team asks of the builder (n3f-c2 builds it).
type fakeBuilder struct {
	mu     sync.Mutex
	begins []IndexJob
	stops  int
}

func (b *fakeBuilder) Begin(_ context.Context, job IndexJob) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.begins = append(b.begins, job)
}

func (b *fakeBuilder) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stops++
}

func (b *fakeBuilder) jobs() []IndexJob {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.begins)
}

func withBuilder(b IndexBuilder) option { return func(c *Config) { c.Builder = b } }

// filtersOf is what a team's long-polls ask for, as the team chose it before each request.
func filtersOf(tm *Team) <-chan json.RawMessage {
	ch := make(chan json.RawMessage, 64)
	tm.onFilter = func(f json.RawMessage) { ch <- f }
	return ch
}

func nextFilter(t *testing.T, ch <-chan json.RawMessage) string {
	t.Helper()
	select {
	case f := <-ch:
		return string(f)
	case <-t.Context().Done():
		t.Fatal("no request")
	}
	return ""
}

// filterFor is proto's own filter for the world's roster as the member under test reads it.
func (w *world) filterFor(ready bool) string {
	a := proto.Anchors{Server: server, Team: teamName, MainRoom: w.main, Self: user(w.self)}
	return string(proto.SyncFilter(w.readRoster(), a, ready).Compact())
}

// qroom is the team's questions room.
func (w *world) qroom() string { return w.ids.Questions }

// promote rewrites the roster naming local the coordinator and the earlier one a worker, as the
// admin does.
func (w *world) promote(local string) {
	w.t.Helper()
	r := w.readRoster()
	members := map[string]proto.RosterMember{}
	for id, m := range r.Members {
		if m.Role == proto.RoleCoordinator {
			m.Role = proto.RoleWorker
		}
		members[id] = m
	}
	m := members[user(local)]
	m.Role = proto.RoleCoordinator
	members[user(local)] = m
	r.Members = members
	w.writeRoster(r)
}

// invited is A 5.2: the admin invites the new coordinator to the questions room.
func (w *world) invited(local string) {
	w.t.Helper()
	check(w.t, w.admin().Invite(w.t.Context(), w.qroom(), user(local)))
}

// calls are the requests of method whose path names room and holds suffix.
func (w *world) calls(method, suffix, room string) int {
	w.t.Helper()
	n := 0
	for _, r := range w.srv.Requests() {
		path, err := url.PathUnescape(r.Path)
		check(w.t, err)
		if r.Method == method && strings.Contains(path, suffix) && strings.Contains(path, room) {
			n++
		}
	}
	return n
}

func (w *world) joinsOf(room string) int { return w.calls(http.MethodPost, "/join/", room) }

func (w *world) leavesOf(room string) int { return w.calls(http.MethodPost, "/leave", room) }

// order is the paths of the requests so far, unescaped, in the order they reached the server.
func (w *world) order() []string {
	w.t.Helper()
	reqs := w.srv.Requests()
	out := make([]string, 0, len(reqs))
	for _, r := range reqs {
		path, err := url.PathUnescape(r.Path)
		check(w.t, err)
		out = append(out, r.Method+" "+path)
	}
	return out
}

// atHead is an empty index at the head, as a finished build would hand it over.
func atHead(last string) (*proto.QuestionIndex, string) {
	return proto.NewQuestionIndex(server, 0), last
}

// withStore changes the engine's store as an earlier run left it.
func (e *fakeEngine) withStore(f func(*state.Store)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	f(e.store)
}

// QS2: the questions room is joined by its id from the verified roster at the team's start, with
// no invite to go by; and the member that has no index starts QX1's build.
func TestQS2TheQuestionsRoomIsJoinedByTheRostersIDAtTheStart(t *testing.T) {
	w := newWorld(t)
	w.as(coord)
	fb := &fakeBuilder{}
	w.startTeam(w.newTeam(withBuilder(fb)))
	w.batch()
	if n := w.joinsOf(w.qroom()); n != 1 {
		t.Errorf("%d joins of the questions room, want 1", n)
	}
	jobs := fb.jobs()
	if len(jobs) != 1 || jobs[0].Key != nil || jobs[0].RoomID != w.qroom() {
		t.Errorf("builder jobs %+v: a first promotion builds from the room's start", jobs)
	}
}

// QS2: a join refused 403 is "not invited yet", and the invite that then arrives is the signal
// to try again, answered at once and not held back by the 30 s between tries.
func TestQS2ARefusedJoinAwaitsTheInviteAndTheInviteIsTheSignal(t *testing.T) {
	w := newWorld(t)
	w.promote(me)
	tm := w.newTeam(withBuilder(&fakeBuilder{}))
	r := w.startTeam(tm)
	w.batch()
	r.poll(t)
	if st := tm.QuestionsStatus(); st.State != team.QuestionsAwaitingInvite {
		t.Fatalf("status %+v, want awaiting-invite", st)
	}
	if n := w.joinsOf(w.qroom()); n != 1 {
		t.Errorf("%d joins before the invite, want 1", n)
	}
	w.invited(me)
	w.batch()
	if n := w.joinsOf(w.qroom()); n != 2 {
		t.Errorf("%d joins after the invite, want 2", n)
	}
	if st := tm.QuestionsStatus(); st.State != team.QuestionsBuilding {
		t.Errorf("status %+v, want building", st)
	}
}

// QS2: no invite for 299 s is a passing state, for 301 s it is `untrusted` with the check
// `invite`; the clock starts when this link first saw the roster name the member.
func TestQS2NoInviteFor300sIsUntrusted(t *testing.T) {
	w := newWorld(t)
	w.promote(me)
	tm := w.newTeam(withBuilder(&fakeBuilder{}))
	r := w.startTeam(tm)
	w.batch()
	r.poll(t)
	w.clk.Advance(299 * time.Second)
	if st := tm.QuestionsStatus(); st.State != team.QuestionsAwaitingInvite {
		t.Errorf("at 299 s: %+v, want awaiting-invite", st)
	}
	w.clk.Advance(2 * time.Second)
	if st := tm.QuestionsStatus(); st.State != team.QuestionsUntrusted || st.Check != team.CheckInvite {
		t.Errorf("at 301 s: %+v, want untrusted with the check invite", st)
	}
}

// directSess is a session over the test's client with the roster as it was before an answer, for
// driving one answer by hand.
func (w *world) directSess(tm *Team, old proto.Roster) *sess {
	w.t.Helper()
	c, pair, err := tm.current()
	check(w.t, err)
	s := &sess{t: tm, c: c, pair: pair, a: anchorsOf(tm.cfg.Team, pair), roster: old, epoch: old.RestoreEpoch}
	_, err = tm.cfg.Engine.Load(tm.cfg.Team, s.a.Self)
	check(w.t, err)
	return s
}

// QS1 and QS2: a roster that names this member the coordinator and the invite that follows it
// (A 5.2) in one answer join the room: the invite is judged against the roster the answer
// brought, so it is not left, and the room is joined by the roster's id.
func TestQS1TheRosterAndTheInviteInOneAnswerJoinTheRoom(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam(withBuilder(&fakeBuilder{}))
	old := w.readRoster()
	s := w.directSess(tm, old)
	w.promote(me)
	w.invited(me)
	raw, err := w.admin().GetState(t.Context(), w.main, proto.RosterType, "")
	check(t, err)
	rosterEvent := json.RawMessage(`{"type":"` + proto.RosterType + `","state_key":"","sender":"` + proto.AdminID(server) + `","event_id":"$roster","content":` + string(raw) + `}`)
	resp := matrix.SyncResponse{
		NextBatch: "n",
		Join:      map[string]matrix.RoomUpdate{w.main: {Timeline: matrix.Timeline{Events: []json.RawMessage{rosterEvent}}}},
		Invite:    map[string]matrix.InvitedRoom{w.qroom(): invite(proto.AdminID(server), proto.AdminID(server), user(me))},
	}
	check(t, s.handle(t.Context(), "", answer{resp: resp}))
	if n := w.joinsOf(w.qroom()); n != 1 {
		t.Errorf("%d joins of the questions room, want 1", n)
	}
	if n := w.leavesOf(w.qroom()); n != 0 {
		t.Errorf("the invite was left %d times: it was judged against the roster from before the answer", n)
	}
}

// QS2: an invite to the questions room that proto.Invite rejects is left (a worker's), through
// the team goroutine's answer, and the room is not joined.
func TestQS2ARejectedInviteIsLeftThroughTheAnswer(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	s := w.directSess(tm, w.readRoster())
	admin := proto.AdminID(server)
	w.invited(me)
	resp := matrix.SyncResponse{NextBatch: "n", Invite: map[string]matrix.InvitedRoom{w.qroom(): invite(admin, admin, user(me))}}
	check(t, s.handle(t.Context(), "", answer{resp: resp}))
	if n := w.leavesOf(w.qroom()); n != 1 {
		t.Errorf("%d leaves of the room a worker was invited to, want 1", n)
	}
	if n := w.joinsOf(w.qroom()); n != 0 {
		t.Errorf("a worker joined the questions room %d times", n)
	}
}

// QS3: a coordinator with `human_text: false` is no roster (A 5.1): main fails trust, the team
// stops 22 untrusted, and nothing of the questions room is reached.
func TestQS3ACoordinatorWithoutHumanTextStopsTheTeamAndJoinsNothing(t *testing.T) {
	w := newWorld(t)
	w.as(coord)
	r := w.readRoster()
	m := r.Members[user(coord)]
	m.HumanText = false
	r.Members[user(coord)] = m
	w.writeRoster(r)
	w.startTeam(w.newTeam(withBuilder(&fakeBuilder{})))
	for w.eng.lastHealth().Stopped == "" {
		<-w.eng.healthed
	}
	if got := w.eng.lastHealth().Stopped; got != exits.CodeUntrusted {
		t.Errorf("stopped %q, want %q", got, exits.CodeUntrusted)
	}
	if n := w.joinsOf(w.qroom()); n != 0 {
		t.Errorf("%d joins of the questions room after a roster that is invalid", n)
	}
}

// QS4 (G5): the long-poll's filter is the coordinator form only while this member is the
// non-parked coordinator and the questions room is joined, verified and its index at the head;
// each change ends the request in flight and the next one carries the new filter, byte for byte
// proto's.
func TestQS4TheFilterFollowsTheQuestionsRoomsReadiness(t *testing.T) {
	w := newWorld(t)
	w.as(coord)
	fb := &fakeBuilder{}
	tm := w.newTeam(withBuilder(fb))
	filters := filtersOf(tm)
	r := w.startTeam(tm)
	w.batch()
	r.poll(t)
	worker, coordinator := w.filterFor(false), w.filterFor(true)
	if worker == coordinator {
		t.Fatal("the two forms are the same filter: the test judges nothing")
	}
	// The resume's sync and the first long-poll: no index yet, so the worker form.
	for range 2 {
		if got := nextFilter(t, filters); got != worker {
			t.Fatalf("filter before the index is at the head:\n%s\nwant the worker form\n%s", got, worker)
		}
	}
	idx, last := atHead(w.main)
	tm.IndexAtHead(idx, last)
	r.poll(t)
	if got := nextFilter(t, filters); got != coordinator {
		t.Fatalf("filter once the index is at the head:\n%s\nwant\n%s", got, coordinator)
	}
	// A demotion: the roster no longer names this member.
	w.promote(peer)
	r.poll(t)
	if got := nextFilter(t, filters); got != w.filterFor(false) {
		t.Fatalf("filter after the demotion:\n%s\nwant the worker form", got)
	}
}

// QS5: the room is verified by VerifySide before any of its events is judged: its state is read
// before the first sync; and an index loaded from the store at the position starts no build
// and gives the coordinator filter from the first request.
func TestQS5ALoadedIndexAtThePositionNeedsNoBuild(t *testing.T) {
	w := newWorld(t)
	w.as(coord)
	first := w.startTeam(w.newTeam(withBuilder(&fakeBuilder{})))
	w.batch()
	first.poll(t)
	first.stop()
	pos := w.eng.position()
	idx := proto.NewQuestionIndex(server, 0).State()
	lastID := w.main
	w.eng.withStore(func(s *state.Store) {
		s.Questions = &proto.QuestionsState{Index: &idx, Token: &pos, Last: &lastID}
	})
	fb := &fakeBuilder{}
	tm := w.newTeam(withBuilder(fb))
	filters := filtersOf(tm)
	before := len(w.order())
	w.startTeam(tm)
	if got := nextFilter(t, filters); got != w.filterFor(true) {
		t.Errorf("the first filter of a seat with its index at the position:\n%s\nwant the coordinator form", got)
	}
	if jobs := fb.jobs(); len(jobs) != 0 {
		t.Errorf("builder jobs %+v: the index was at the position", jobs)
	}
	if st := tm.QuestionsStatus(); st.State != team.QuestionsReady {
		t.Errorf("status %+v, want ready", st)
	}
	var sawState bool
	for _, req := range w.order()[before:] {
		if strings.HasPrefix(req, "GET ") && strings.Contains(req, w.qroom()) && strings.HasSuffix(req, "/state") {
			sawState = true
		}
		if strings.HasSuffix(req, "/sync") && !sawState {
			t.Error("a sync was made before the questions room was verified")
		}
	}
	if !sawState {
		t.Error("the questions room's state was never read")
	}
}
