package engine

import (
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/rooms"
)

// mainRecorder keeps the main room's id as rooms.Converge makes it.
type mainRecorder struct{ main string }

func (*mainRecorder) Intent(string) error { return nil }

func (r *mainRecorder) Record(purpose, id string) error {
	if purpose == proto.PurposeMain {
		r.main = id
	}
	return nil
}

// hsTeam is team id's rooms on fakehs, its clock the world's wall clock, with this member
// (agent 1) joined to main through client.
type hsTeam struct {
	srv    *fakehs.Server
	client *matrix.Client
	main   string
}

func (w *world) onHS(id paths.TeamID) hsTeam {
	w.t.Helper()
	server := paths.ServerName(id)
	srv := fakehs.New(fakehs.Options{
		ServerName: server, Now: func() time.Time { return time.UnixMilli(w.clk.WallMS()) }, Quirks: fakehs.TuwunelQuirks(),
	})
	check(w.t, srv.Start())
	w.t.Cleanup(func() {
		if err := srv.CheckModelled(); err != nil {
			w.t.Errorf("fakehs: %v", err)
		}
		check(w.t, srv.Close())
	})
	srv.AddUser(proto.AdminLocalpart, "pw", true)
	r := sendRoster(id)
	for _, user := range append(slices.Collect(maps.Keys(r.Members)), alice(id)) {
		local, ok := proto.Localpart(user, server)
		if !ok {
			w.t.Fatalf("%s is not of %s", user, server)
		}
		srv.AddUser(local, "pw", false)
	}
	adminToken, err := srv.MintToken(proto.AdminID(server))
	check(w.t, err)
	admin, err := matrix.NewAdmin(matrix.Config{BaseURL: srv.URL(), Token: adminToken, Clock: noSleep{}})
	check(w.t, err)
	w.t.Cleanup(admin.Close)
	rec := &mainRecorder{}
	r.Rooms = proto.SideRooms{}
	if _, convErr := rooms.Converge(w.t.Context(), admin, rec, rooms.Team{Server: server, Team: id.String(), Roster: r}, proto.RoomIDs{}); convErr != nil {
		w.t.Fatalf("Converge: %v", convErr)
	}
	token, err := srv.MintToken(agent(id, 1))
	check(w.t, err)
	c, err := matrix.NewMember(matrix.Config{BaseURL: srv.URL(), PlainHTTPHosts: []string{"127.0.0.1"}, Token: token, Clock: noSleep{}})
	check(w.t, err)
	w.t.Cleanup(c.Close)
	if _, err := c.Join(w.t.Context(), rec.main); err != nil {
		w.t.Fatalf("join main: %v", err)
	}
	w.mu.Lock()
	w.mains[id] = rec.main
	w.mu.Unlock()
	f := w.fake(id)
	f.notices.mu.Lock()
	f.notices.hs = c
	f.notices.mu.Unlock()
	return hsTeam{srv: srv, client: c, main: rec.main}
}

// own are this member's message events in main, oldest first: each event's id and content.
func (h hsTeam) own(t *testing.T, self string) []proto.Value {
	t.Helper()
	page, err := h.client.Messages(t.Context(), h.main, matrix.MessagesRequest{Dir: matrix.Backward, Limit: 1000})
	check(t, err)
	var out []proto.Value
	for _, raw := range slices.Backward(page.Chunk) {
		v, err := proto.Parse(raw)
		check(t, err)
		typ, okType := v.Get("type")
		sender, okSender := v.Get("sender")
		if okType && okSender && typ.Str() == proto.MessageType && sender.Str() == self {
			out = append(out, v)
		}
	}
	return out
}

// requests counts fakehs's requests of method whose path holds part.
func (h hsTeam) requests(method, part string) int {
	n := 0
	for _, r := range h.srv.Requests() {
		if r.Method == method && strings.Contains(r.Path, part) {
			n++
		}
	}
	return n
}

func eventID(t *testing.T, v proto.Value) string {
	t.Helper()
	id, ok := v.Get("event_id")
	if !ok {
		t.Fatal("an event with no id")
	}
	return id.Str()
}

// SD3-2: an answer lost after the server stored the event is retried by the same txn: one
// event, and the send answers its id.
func TestSD3_2ALostAnswerIsRetriedByItsTxn(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	h := w.onHS(ops)
	w.sending(ops)
	h.srv.Inject(fakehs.Fault{Method: http.MethodPut, Path: "/send/", DropAfter: true})
	id := posted(t, w.say(&ops, "hello", "repo.2"))
	got := h.own(t, agent(ops, 1))
	if len(got) != 1 || eventID(t, got[0]) != id {
		t.Fatalf("main holds %d own events, want one with id %s", len(got), id)
	}
	if n := h.requests(http.MethodPut, "/send/"); n != 2 {
		t.Errorf("%d PUTs, want the lost one and its retry", n)
	}
}

// reordered is content with its members in reverse order: the same JSON value, other bytes.
func reordered(t *testing.T, content []byte) []byte {
	t.Helper()
	v, err := proto.Parse(content)
	check(t, err)
	members := slices.Clone(v.Members())
	slices.Reverse(members)
	return proto.Obj(members...).Compact()
}

// SD3-10: the recovery: a tried send the server already holds (re-serialised with another key
// order) is recorded without a PUT, a tried send it lacks is posted by its txn, and an untried
// one is posted without a search.
func TestSD3_10TheRecoveryFindsTriedSends(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops, dev := tid(t, "ops"), tid(t, "dev")
	h := w.onHS(ops)
	c := proto.Composer{Server: paths.ServerName(ops), Self: agent(ops, 1), Roster: sendRoster(ops)}
	out, err := c.Ping(proto.Ping{Verb: "review", To: []string{agent(ops, 3)}, Ref: reviewRef}, "", "review")
	check(t, err)
	held, err := h.client.Send(t.Context(), h.main, proto.MessageType, "tc"+strings.Repeat("9", 32), out.Content.Compact())
	check(t, err)
	origin := w.clk.WallMS()
	w.clk.Advance(5 * time.Second)

	s := w.newStore(ops)
	found := state.Sending{
		Txn: "tc" + strings.Repeat("a", 32), Room: proto.PurposeMain, Kind: state.SendPing, SinceMS: origin, Tried: true,
		Type: proto.MessageType, Content: reordered(t, out.Content.Compact()),
		Ping: &state.SendingPing{Verb: "review", Ref: new(reviewRef), To: []string{agent(ops, 3)}, TimeoutMS: 600_000},
	}
	lacking := queuedText(w, ops, 1)
	lacking.Tried = true
	untried := queuedText(w, ops, 2)
	s.Outbox.Sending = []state.Sending{found, lacking, untried}
	f := w.sendingWith(ops, s)
	w.until(w.sendsLeft(ops))
	if got := f.notices.sent(); !slices.Equal(got, []string{lacking.Txn, untried.Txn}) {
		t.Errorf("posted %v, want only the lacking and the untried sends", got)
	}
	pings := w.store(ops).Outbox.Pings
	if len(pings) != 1 || pings[0].ID != held || pings[0].SentMS != origin || pings[0].DueMS != origin+600_000 {
		t.Errorf("the recovered ping = %+v, want %s sent at %d", pings, held, origin)
	}
	if r := f.notices.owned()[held]; r.Verb != "review" || r.Sender != agent(ops, 1) {
		t.Errorf("the recovered ping's own send = %+v", r)
	}
	if n := len(h.own(t, agent(ops, 1))); n != 3 {
		t.Errorf("main holds %d own events, want 3", n)
	}

	hd := w.onHS(dev)
	d := w.newStore(dev)
	d.Outbox.Sending = []state.Sending{queuedText(w, dev, 3)}
	w.sendingWith(dev, d)
	w.until(w.sendsLeft(dev))
	if n := hd.requests(http.MethodGet, "/messages"); n != 0 {
		t.Errorf("an untried send was searched for: %d reads", n)
	}
}

// A recovery whose read fails is the team's to retry: the sends wait, untouched.
func TestARecoveryThatFailsLeavesTheSendsWaiting(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	s := w.newStore(ops)
	tried := queuedText(w, ops, 1)
	tried.Tried = true
	s.Outbox.Sending = []state.Sending{tried}
	f := w.add(ops, encodeStore(t, s))
	<-f.running
	f.notices.set(sendRoster(ops))
	f.notices.mu.Lock()
	f.notices.messagesErr = matrix.StatusError("GET", "/messages", 502, "")
	f.notices.mu.Unlock()
	if err := w.e.RecoverSends(t.Context(), ops, f.notices); err == nil {
		t.Fatal("a failed read recovered")
	}
	w.e.SetHealth(ops, up(startWall))
	if len(f.notices.sent()) != 0 || len(w.store(ops).Outbox.Sending) != 1 {
		t.Fatal("a send was posted before its recovery")
	}
	f.notices.mu.Lock()
	f.notices.messagesErr = nil
	f.notices.mu.Unlock()
	w.recover(ops, f)
	w.until(w.sendsLeft(ops))
	if got := f.notices.sent(); !slices.Equal(got, []string{tried.Txn}) {
		t.Errorf("posted %v", got)
	}
}
