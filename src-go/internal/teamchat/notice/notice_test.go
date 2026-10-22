package notice

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/rooms"
)

const (
	server = "alpha.teamchat.internal"
	team   = "alpha"
	coord  = "myrepo.1+desk.podman"
	worker = "myrepo.2+desk.podman"
	parked = "myrepo.3+desk.podman"
)

func user(local string) string { return proto.UserID(local, server) }

type noWait struct{}

func (noWait) Sleep(ctx context.Context, _ time.Duration) error { return ctx.Err() }

// clock is fakehs's time, moved by the test.
type clock struct {
	mu sync.Mutex
	ms int64
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.UnixMilli(c.ms).UTC()
}

func (c *clock) set(ms int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ms = ms
}

type recorder struct{ ids proto.RoomIDs }

func (*recorder) Intent(string) error { return nil }

func (r *recorder) Record(purpose, id string) error {
	switch purpose {
	case proto.PurposeMain:
		r.ids.Main = id
	case proto.PurposeReports:
		r.ids.Reports = id
	case proto.PurposeQuestions:
		r.ids.Questions = id
	}
	return nil
}

// world is alpha's rooms on fakehs: human alice, coordinator coord, worker worker and a parked
// member, every agent joined to main with its own token.
type world struct {
	srv    *fakehs.Server
	clock  *clock
	main   string
	tokens map[string]string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	c := &clock{ms: 1_760_000_000_000}
	srv := fakehs.New(fakehs.Options{ServerName: server, Now: c.now, Quirks: fakehs.TuwunelQuirks()})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := srv.CheckModelled(); err != nil {
			t.Errorf("fakehs: %v", err)
		}
		if err := srv.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	srv.AddUser(proto.AdminLocalpart, "pw", true)
	w := &world{srv: srv, clock: c, tokens: map[string]string{}}
	for _, local := range []string{"alice", coord, worker, parked} {
		srv.AddUser(local, "pw", false)
	}
	admin := w.admin(t)
	rec := &recorder{}
	roster := proto.Roster{
		Team: team, Name: "Alpha", Humans: []string{user("alice")},
		Members: map[string]proto.RosterMember{
			user(coord):  {Role: proto.RoleCoordinator, HumanText: true},
			user(worker): {Role: proto.RoleWorker, HumanText: true},
			user(parked): {Role: proto.RoleWorker, HumanText: true, Parked: true},
		},
		Forge: proto.Forge{Kind: proto.ForgeGitHub, Org: "example-org"}, Limits: proto.DefaultLimits(),
	}
	if _, err := rooms.Converge(t.Context(), admin, rec, rooms.Team{Server: server, Team: team, Roster: roster}, proto.RoomIDs{}); err != nil {
		t.Fatalf("Converge: %v", err)
	}
	w.main = rec.ids.Main
	for _, local := range []string{coord, worker, parked} {
		token, err := srv.MintToken(user(local))
		if err != nil {
			t.Fatal(err)
		}
		w.tokens[local] = token
		if _, err := w.client(t, local).Join(t.Context(), w.main); err != nil {
			t.Fatalf("%s joins main: %v", local, err)
		}
	}
	return w
}

func (w *world) admin(t *testing.T) *matrix.Admin {
	t.Helper()
	token, err := w.srv.MintToken(proto.AdminID(server))
	if err != nil {
		t.Fatal(err)
	}
	a, err := matrix.NewAdmin(matrix.Config{BaseURL: w.srv.URL(), Token: token, Clock: noWait{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return a
}

func (w *world) client(t *testing.T, local string) *matrix.Client {
	t.Helper()
	c, err := matrix.NewMember(matrix.Config{BaseURL: w.srv.URL(), PlainHTTPHosts: []string{"127.0.0.1"}, Token: w.tokens[local], Clock: noWait{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func run(t *testing.T, s string) paths.RunID {
	t.Helper()
	r, err := paths.ParseRunID(s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (w *world) notice(t *testing.T, local string, sinceMS int64) Notice {
	t.Helper()
	return Notice{Server: server, Team: team, MainRoom: w.main, Handle: local, Token: w.tokens[local], Run: run(t, "0123456789abcdef"), SinceMS: sinceMS}
}

// post sends local's own lifecycle notice through A's composer, as a link would.
func (w *world) post(t *testing.T, local, event, reason string) {
	t.Helper()
	c := w.client(t, local)
	raw, err := c.RoomState(t.Context(), w.main)
	if err != nil {
		t.Fatal(err)
	}
	r, err := proto.VerifyMain(stateEvents(raw), proto.Anchors{Server: server, Team: team, MainRoom: w.main, Self: user(local)})
	if err != nil {
		t.Fatal(err)
	}
	to := []string{user("alice")}
	if local != coord {
		to = append(to, user(coord))
	}
	out, err := proto.Composer{Server: server, Self: user(local), Token: w.tokens[local], Roster: r}.Notice(proto.Notice{Event: event, Subject: user(local), Reason: reason, To: to}, event)
	if err != nil {
		t.Fatalf("compose %s: %v", event, err)
	}
	if _, err := c.Send(t.Context(), w.main, proto.MessageType, "tc-test-"+event+"-"+strings.ReplaceAll(local, "+", "-"), out.Content.Compact()); err != nil {
		t.Fatal(err)
	}
}

// disconnects are the disconnected notices in main, by sender.
func (w *world) disconnects(t *testing.T) []string {
	t.Helper()
	page, err := w.client(t, worker).Messages(t.Context(), w.main, matrix.MessagesRequest{Dir: matrix.Backward, Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, raw := range page.Chunk {
		if ev, ok := lifecycle(raw); ok && ev.event == proto.EventDisconnected {
			out = append(out, ev.sender)
		}
	}
	return out
}

func disconnected(ctx context.Context, t *testing.T, c Client, n Notice) Outcome {
	t.Helper()
	out, err := Disconnected(ctx, c, n)
	if err != nil {
		t.Fatalf("Disconnected: %v", err)
	}
	return out
}

// NT1: the notice is posted once as the subject, to the humans and the coordinator, with the
// run's transaction id; asked again, the goodbye is found.
func TestTheNoticeIsPostedOnce(t *testing.T) {
	w := newWorld(t)
	c := w.client(t, worker)
	n := w.notice(t, worker, 0)
	if out := disconnected(t.Context(), t, c, n); !out.Posted || out.Found || out.Why != "" {
		t.Fatalf("first: %+v", out)
	}
	if got := w.disconnects(t); len(got) != 1 || got[0] != user(worker) {
		t.Fatalf("disconnects %v", got)
	}
	var sent bool
	for _, r := range w.srv.Requests() {
		sent = sent || r.Method == http.MethodPut && strings.HasSuffix(r.Path, "/send/m.room.message/tc-disc-0123456789abcdef")
	}
	if !sent {
		t.Fatal("the notice was not sent with the run's transaction id")
	}
	if out := disconnected(t.Context(), t, c, n); !out.Posted || !out.Found {
		t.Fatalf("again: %+v", out)
	}
	if got := w.disconnects(t); len(got) != 1 {
		t.Fatalf("posted twice: %v", got)
	}
}

// NT1: a `left` since since_ms is a goodbye; one before it is not; a `back` after a
// disconnected is a later run.
func TestTheLatestOwnLifecycleNoticeDecides(t *testing.T) {
	w := newWorld(t)
	c := w.client(t, worker)
	w.clock.set(2_000)
	w.post(t, worker, proto.EventLeft, "exit")
	if out := disconnected(t.Context(), t, c, w.notice(t, worker, 2_000)); !out.Found {
		t.Fatalf("a left since since_ms: %+v", out)
	}
	if out := disconnected(t.Context(), t, c, w.notice(t, worker, 2_001)); !out.Posted || out.Found {
		t.Fatalf("a left before since_ms: %+v", out)
	}
	w.clock.set(3_000)
	w.post(t, worker, proto.EventBack, "")
	n := w.notice(t, worker, 0)
	n.Run = run(t, "fedcba9876543210")
	if out := disconnected(t.Context(), t, c, n); !out.Posted || out.Found {
		t.Fatalf("a back after the disconnected: %+v", out)
	}
	if got := w.disconnects(t); len(got) != 2 {
		t.Fatalf("disconnects %v", got)
	}
}

// NT1: another member's notice, the admin's and an event that does not decode are not the
// subject's goodbye.
func TestOnlyTheSubjectsOwnNoticeCounts(t *testing.T) {
	w := newWorld(t)
	w.post(t, coord, proto.EventDisconnected, "")
	if _, err := w.client(t, worker).Send(t.Context(), w.main, proto.MessageType, "tc-junk", json.RawMessage(`{"msgtype":"m.notice","body":"x","teamchat.notice":{"v":1}}`)); err != nil {
		t.Fatal(err)
	}
	if out := disconnected(t.Context(), t, w.client(t, worker), w.notice(t, worker, 0)); !out.Posted || out.Found {
		t.Fatalf("%+v", out)
	}
}

// NT2: what the member or its room can cause is an outcome, not an error.
func TestWhatAMemberCanCauseIsAnOutcome(t *testing.T) {
	w := newWorld(t)
	for _, c := range []struct {
		name string
		n    func() Notice
		c    func() Client
		why  string
	}{
		{"a refused token", func() Notice { return w.notice(t, worker, 0) }, func() Client {
			w.tokens["gone"] = "not-a-token"
			return w.client(t, "gone")
		}, whyToken},
		{"a parked subject", func() Notice { return w.notice(t, parked, 0) }, func() Client { return w.client(t, parked) }, whyInactive},
		{"a room that does not verify", func() Notice {
			n := w.notice(t, worker, 0)
			n.Team = "beta"
			return n
		}, func() Client { return w.client(t, worker) }, whyUnverified},
	} {
		if out := disconnected(t.Context(), t, c.c(), c.n()); out.Posted || out.Why != c.why {
			t.Fatalf("%s: %+v, want %q", c.name, out, c.why)
		}
	}
}

// NT2: a post the room forbids is an outcome.
func TestAForbiddenPostIsAnOutcome(t *testing.T) {
	w := newWorld(t)
	w.srv.Inject(fakehs.Fault{Method: http.MethodPut, Path: "/send/", Status: http.StatusForbidden, Body: []byte(`{"errcode":"M_FORBIDDEN","error":"no"}`)})
	if out := disconnected(t.Context(), t, w.client(t, worker), w.notice(t, worker, 0)); out.Posted || out.Why != whyForbidden {
		t.Fatalf("%+v", out)
	}
}

// NT2: only a transient failure, or a cancelled context, is an error.
func TestATransientFailureIsAnError(t *testing.T) {
	for _, f := range []fakehs.Fault{{Method: http.MethodGet, Path: "/state"}, {Method: http.MethodGet, Path: "/messages"}, {Method: http.MethodPut, Path: "/send/"}} {
		w := newWorld(t)
		f.Status, f.Times = http.StatusBadGateway, 100
		w.srv.Inject(f)
		path := f.Path
		if _, err := Disconnected(t.Context(), w.client(t, worker), w.notice(t, worker, 0)); err == nil {
			t.Fatalf("%s failing: no error", path)
		}
	}
	w := newWorld(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Disconnected(ctx, w.client(t, worker), w.notice(t, worker, 0)); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled context: %v", err)
	}
}

// pager answers every page with events that are not the subject's, and counts the pages.
type pager struct {
	Client

	pages int
}

func (p *pager) Messages(context.Context, string, matrix.MessagesRequest) (matrix.MessagesPage, error) {
	p.pages++
	return matrix.MessagesPage{Chunk: []json.RawMessage{json.RawMessage(`{"type":"m.room.message","sender":"@x:y","origin_server_ts":9999999999999,"content":{}}`)}, End: "more"}, nil
}

// NT1: at most 10 pages are read; the bound reached without a goodbye posts.
func TestThePageBound(t *testing.T) {
	w := newWorld(t)
	p := &pager{Client: w.client(t, worker)}
	if out := disconnected(t.Context(), t, p, w.notice(t, worker, 0)); !out.Posted || out.Found {
		t.Fatalf("%+v", out)
	}
	if p.pages != MaxPages {
		t.Fatalf("read %d pages, want %d", p.pages, MaxPages)
	}
}

// The notice's display and transaction id are built from parsed names only.
func TestTheDisplay(t *testing.T) {
	if got := display(worker); got != worker+": the member's session ended without a goodbye" {
		t.Fatalf("display %q", got)
	}
	if got := TxnID(run(t, "0123456789abcdef")); got != "tc-disc-0123456789abcdef" {
		t.Fatalf("txn %q", got)
	}
}
