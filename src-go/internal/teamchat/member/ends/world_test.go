package ends

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/rooms"
)

const (
	server = "alpha.teamchat.internal"
	teamID = "alpha"
	coord  = "myrepo.1+desk.podman"
	worker = "myrepo.2+desk.podman"
	other  = "myrepo.3+desk.podman"
)

func user(local string) string { return proto.UserID(local, server) }

type noWait struct{}

func (noWait) Sleep(ctx context.Context, _ time.Duration) error { return ctx.Err() }

// hsClock is fakehs's time, moved by the test.
type hsClock struct {
	mu sync.Mutex
	ms int64
}

func (c *hsClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.UnixMilli(c.ms).UTC()
}

func (c *hsClock) set(ms int64) {
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

// world is alpha's rooms on fakehs: human alice, coordinator coord and workers worker and other,
// every agent joined to main with its own token.
type world struct {
	srv    *fakehs.Server
	clock  *hsClock
	main   string
	tokens map[string]string
	roster proto.Roster
}

func newWorld(t *testing.T) *world {
	t.Helper()
	c := &hsClock{ms: 1_760_000_000_000}
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
	for _, local := range []string{"alice", coord, worker, other} {
		srv.AddUser(local, "pw", false)
	}
	rec := &recorder{}
	w.roster = proto.Roster{
		Team: teamID, Name: "Alpha", Humans: []string{user("alice")},
		Members: map[string]proto.RosterMember{
			user(coord):  {Role: proto.RoleCoordinator, HumanText: true},
			user(worker): {Role: proto.RoleWorker, HumanText: true},
			user(other):  {Role: proto.RoleWorker, HumanText: true},
		},
		Forge: proto.Forge{Kind: proto.ForgeGitHub, Org: "example-org"}, Limits: proto.DefaultLimits(),
	}
	if _, err := rooms.Converge(t.Context(), w.admin(t), rec, rooms.Team{Server: server, Team: teamID, Roster: w.roster}, proto.RoomIDs{}); err != nil {
		t.Fatalf("Converge: %v", err)
	}
	w.main = rec.ids.Main
	for _, local := range []string{coord, worker, other} {
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

// notices is local's team.Notices over its fakehs client; verified says whether the roster is
// already known (the team goroutine verified main).
func (w *world) notices(t *testing.T, local string, verified bool) *fakeNotices {
	t.Helper()
	return &fakeNotices{
		Client:   w.client(t, local),
		anchors:  proto.Anchors{Server: server, Team: teamID, MainRoom: w.main, Self: user(local)},
		token:    w.tokens[local],
		roster:   w.roster,
		verified: verified,
	}
}

// lifecycle is one lifecycle notice in main, newest first.
type lifecycle struct{ sender, event, reason string }

// notices in main, newest first.
func (w *world) lifecycles(t *testing.T) []lifecycle {
	t.Helper()
	page, err := w.client(t, worker).Messages(t.Context(), w.main, matrix.MessagesRequest{Dir: matrix.Backward, Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	var out []lifecycle
	for _, raw := range page.Chunk {
		if l, ok := lifecycleOf(t, raw); ok {
			out = append(out, l)
		}
	}
	return out
}

// lifecycleOf is a member's lifecycle notice as main shows it.
func lifecycleOf(t *testing.T, raw []byte) (lifecycle, bool) {
	t.Helper()
	v, err := proto.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	typ, okType := v.Get("type")
	sender, okSender := v.Get("sender")
	content, okContent := v.Get("content")
	if !okType || !okSender || !okContent || typ.Str() != proto.MessageType || sender.Str() == proto.AdminID(server) {
		return lifecycle{}, false
	}
	dc, reason := proto.DecodeContent(content, []string{proto.KeyNotice})
	if reason != "" || !proto.MemberNotice(dc.Notice.Event) {
		return lifecycle{}, false
	}
	return lifecycle{sender.Str(), dc.Notice.Event, dc.Notice.Reason}, true
}

// sentTxns are the transaction ids of every message send, in order.
func (w *world) sentTxns() []string {
	var out []string
	const prefix = "/send/m.room.message/"
	for _, r := range w.srv.Requests() {
		if i := strings.Index(r.Path, prefix); r.Method == http.MethodPut && i >= 0 {
			out = append(out, r.Path[i+len(prefix):])
		}
	}
	return out
}

// fakeNotices is team.Notices over a matrix client.
type fakeNotices struct {
	*matrix.Client

	anchors  proto.Anchors
	token    string
	roster   proto.Roster
	verified bool

	sendErr, messagesErr, stateErr error

	mu   sync.Mutex
	own  map[string]proto.Related
	sent int
}

var _ team.Notices = (*fakeNotices)(nil)

func (n *fakeNotices) Send(ctx context.Context, roomID, eventType, txnID string, content json.RawMessage) (string, error) {
	if n.sendErr != nil {
		return "", n.sendErr
	}
	n.mu.Lock()
	n.sent++
	n.mu.Unlock()
	return n.Client.Send(ctx, roomID, eventType, txnID, content)
}

func (n *fakeNotices) Messages(ctx context.Context, roomID string, req matrix.MessagesRequest) (matrix.MessagesPage, error) {
	if n.messagesErr != nil {
		return matrix.MessagesPage{}, n.messagesErr
	}
	return n.Client.Messages(ctx, roomID, req)
}

func (n *fakeNotices) RoomState(ctx context.Context, roomID string) ([]json.RawMessage, error) {
	if n.stateErr != nil {
		return nil, n.stateErr
	}
	return n.Client.RoomState(ctx, roomID)
}

func (n *fakeNotices) Anchors() proto.Anchors { return n.anchors }

func (n *fakeNotices) Roster() (proto.Roster, bool) { return n.roster, n.verified }

func (n *fakeNotices) RecordOwnSend(id string, r proto.Related) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.own == nil {
		n.own = map[string]proto.Related{}
	}
	n.own[id] = r
}

func (n *fakeNotices) Token() string { return n.token }

func (n *fakeNotices) owns() map[string]proto.Related {
	n.mu.Lock()
	defer n.mu.Unlock()
	return maps.Clone(n.own)
}

// fakeEngine records SetEnded; the rest of team.Engine is not the ends'.
type fakeEngine struct {
	team.Engine

	mu    sync.Mutex
	ended map[paths.TeamID][]state.RunEnd
	err   error
}

func (e *fakeEngine) SetEnded(_ context.Context, t paths.TeamID, end state.RunEnd) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return e.err
	}
	if e.ended == nil {
		e.ended = map[paths.TeamID][]state.RunEnd{}
	}
	e.ended[t] = append(e.ended[t], end)
	return nil
}

func (e *fakeEngine) of(t paths.TeamID) []state.RunEnd {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]state.RunEnd(nil), e.ended[t]...)
}

func runID(t *testing.T, s string) paths.RunID {
	t.Helper()
	r, err := paths.ParseRunID(s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func teamOf(t *testing.T, s string) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

const (
	thisRun  = "0123456789abcdef"
	priorRun = "fedcba9876543210"
)

// fixture is the ends under test, their fake engine and their log.
type fixture struct {
	e   *ends
	eng *fakeEngine
	log *bytes.Buffer
}

// newEnds is the ends under test with a fake engine, at the fake clock's wall time.
func newEnds(t *testing.T, switchOn *bool) fixture {
	t.Helper()
	eng := &fakeEngine{}
	log := &bytes.Buffer{}
	e := New(Config{Engine: eng, Run: runID(t, thisRun), Clock: clock.NewFake(time.Hour, 1_770_000_000_000), Log: &syncWriter{w: log}, switchOverride: switchOn})
	got, ok := e.(*ends)
	if !ok {
		t.Fatalf("New is a %T", e)
	}
	return fixture{got, eng, log}
}

// syncWriter is a log the parallel sends may write at once.
type syncWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}
