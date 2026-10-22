package link

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/engine"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
)

const mainRoom = "!mmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmm"

// fakeNotices is a team's client: it records the presence set.
type fakeNotices struct {
	self     string
	mu       sync.Mutex
	presence []string
}

func (*fakeNotices) RoomState(context.Context, string) ([]json.RawMessage, error) { return nil, nil }

func (*fakeNotices) Messages(context.Context, string, matrix.MessagesRequest) (matrix.MessagesPage, error) {
	return matrix.MessagesPage{}, nil
}

func (*fakeNotices) Send(context.Context, string, string, string, json.RawMessage) (string, error) {
	return "", nil
}

func (n *fakeNotices) SetPresence(_ context.Context, userID, presence string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.presence = append(n.presence, userID+" "+presence)
	return nil
}

func (n *fakeNotices) Anchors() proto.Anchors            { return proto.Anchors{Self: n.self} }
func (*fakeNotices) RecordOwnSend(string, proto.Related) {}
func (*fakeNotices) Token() string                       { return "" }
func (*fakeNotices) Roster() (proto.Roster, bool)        { return proto.Roster{}, false }

func (n *fakeNotices) presences() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.presence)
}

// fakeTeam runs until cancelled.
type fakeTeam struct {
	notices *fakeNotices
	mu      sync.Mutex
	ran     bool
}

func (f *fakeTeam) Run(ctx context.Context) error {
	f.mu.Lock()
	f.ran = true
	f.mu.Unlock()
	<-ctx.Done()
	return ctx.Err()
}

func (*fakeTeam) Refetch(context.Context, string, string) (proto.Verdict, error) {
	return proto.Verdict{}, errors.New("no refetch")
}

func (*fakeTeam) Verify(context.Context, string) (records.Verified, error) {
	return records.Verified{}, errors.New("no verify")
}

func (*fakeTeam) History(context.Context, string, int) ([]records.History, error) { return nil, nil }
func (f *fakeTeam) Notices() team.Notices                                       { return f.notices }
func (*fakeTeam) Wake()                                                          {}
func (*fakeTeam) FlushFloor(context.Context) error                               { return nil }

func (f *fakeTeam) didRun() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ran
}

// fakeEnds records the ends' calls.
type fakeEnds struct {
	mu    sync.Mutex
	ends  []string
	leave []string
}

func (*fakeEnds) Close(context.Context, paths.TeamID, team.Notices, state.StoreRun) error { return nil }
func (*fakeEnds) Back(context.Context, paths.TeamID, team.Notices, state.StoreRun) error  { return nil }

func (e *fakeEnds) End(_ context.Context, how team.End, teams []team.EndTeam) []error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, t := range teams {
		e.ends = append(e.ends, how.How+" "+how.Kind+" "+t.Team.String())
	}
	return []error{errors.New("one end failed")}
}

func (e *fakeEnds) Leave(_ context.Context, t paths.TeamID, _ team.Notices, txn string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.leave = append(e.leave, t.String()+" "+txn)
	return nil
}

func (e *fakeEnds) endCalls() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.ends)
}

func (e *fakeEnds) leaveCalls() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.leave)
}

// fakeParent is the keeper's pidfd.
type fakeParent struct {
	exited chan struct{}
	mu     sync.Mutex
	closed bool
}

func (*fakeParent) PID() int                  { return 50 }
func (p *fakeParent) Exited() <-chan struct{} { return p.exited }

func (p *fakeParent) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

func (p *fakeParent) wasClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

type got struct {
	m   wire.Msg
	err error
}

// linkWorld is a home, a keeper holding its run and talking to a link over the control socket.
type linkWorld struct {
	*home

	t      *testing.T
	keeper *lock.KeeperLock
	conn   *wire.Conn
	in     chan got
	ends   *fakeEnds
	parent *fakeParent
	proc   *fakeProc
	cfg    Config
	exit   chan int
	mu     sync.Mutex
	teams  map[paths.TeamID]*fakeTeam
	ln     *link
}

func newLinkWorld(t *testing.T) *linkWorld {
	t.Helper()
	w := &linkWorld{
		home: newHome(t), t: t, in: make(chan got, 64), ends: &fakeEnds{},
		parent: &fakeParent{exited: make(chan struct{})}, proc: newFakeProc(),
		exit: make(chan int, 1), teams: map[paths.TeamID]*fakeTeam{},
	}
	if n := len(w.layout.Run(w.run).LinkSock()); n > 100 {
		t.Fatalf("the test's link.sock is %d bytes; shorten the test's name", n)
	}
	k, err := lock.TakeKeeper(t.Context(), w.clk, w.root, w.layout.Run(w.run))
	check(t, err)
	t.Cleanup(func() { check(t, k.Release()) })
	w.keeper = k
	keeperEnd, linkEnd, err := wire.Pair()
	check(t, err)
	w.conn, err = wire.NewConn(keeperEnd)
	check(t, err)
	linkConn, err := wire.NewConn(linkEnd)
	check(t, err)
	kit, err := paths.ParseDigest(kitHex)
	check(t, err)
	w.cfg = Config{
		StateRoot: w.root, RunRoot: w.root, Layout: w.layout, Run: w.run, Control: linkConn,
		Clock: w.clk, Proc: w.proc, Parent: func() (Parent, error) { return w.parent, nil },
		UID: os.Getuid(), Kit: kit, RuntimeDir: w.dir + "/xdg", Home: w.dir, Log: w.log,
		Engine: engine.New, NewTeam: w.newTeam, NewEnds: func(team.Engine) team.Ends { return w.ends },
		started: func(l *link) {
			w.mu.Lock()
			defer w.mu.Unlock()
			w.ln = l
		},
	}
	go func() {
		for {
			m, fd, err := w.conn.Recv()
			if fd != nil {
				check(t, fd.Close())
			}
			w.in <- got{m, err}
			if err != nil {
				return
			}
		}
	}()
	return w
}

func (w *linkWorld) newTeam(cfg engine.TeamConfig) (team.Team, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	f := &fakeTeam{notices: &fakeNotices{self: agent(cfg.Team, 1)}}
	w.teams[cfg.Team] = f
	return f, nil
}

func (w *linkWorld) team(id paths.TeamID) *fakeTeam {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.teams[id]
}

func (w *linkWorld) link() *link {
	w.t.Helper()
	until(w.t, func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.ln != nil
	})
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ln
}

// start runs the link; its exit code arrives on w.exit.
func (w *linkWorld) start() {
	go func() { w.exit <- int(Run(context.Background(), w.cfg)) }()
	w.t.Cleanup(func() {
		if err := w.conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			w.t.Error(err)
		}
	})
}

func (w *linkWorld) send(m wire.Msg, fd *os.File) {
	w.t.Helper()
	check(w.t, w.conn.Send(m, fd))
}

func (w *linkWorld) launch(seats int) {
	w.t.Helper()
	w.send(wire.Msg{Kind: wire.KindLaunch, Launch: &wire.Launch{
		Run: w.run, Kind: state.KindCCY, Launch: state.LaunchClaude, Inbound: state.InboundAccept,
		Cwd: "/work", Seats: seats,
	}}, nil)
}

// next is the next message to the keeper of kind, skipping state reports.
func (w *linkWorld) next(kind string) wire.Msg {
	w.t.Helper()
	for {
		select {
		case g := <-w.in:
			if g.err != nil {
				w.t.Fatalf("waiting for %s: %v", kind, g.err)
			}
			if g.m.Kind == wire.KindState && kind != wire.KindState {
				continue
			}
			if g.m.Kind != kind {
				w.t.Fatalf("got %s, want %s", g.m, kind)
			}
			return g.m
		case <-w.t.Context().Done():
			w.t.Fatalf("no %s", kind)
		}
	}
}

func (w *linkWorld) exited() int {
	w.t.Helper()
	select {
	case code := <-w.exit:
		return code
	case <-w.t.Context().Done():
		w.t.Fatal("the link never exited")
	}
	return 0
}

// seat makes id's seat with its bundle and a fresh store, takes it, and is the add.
func (w *linkWorld) seat(id paths.TeamID) (wire.Msg, *os.File) {
	w.t.Helper()
	s := w.layout.Seat(id)
	check(w.t, w.root.MkdirAll(s.Dir()))
	issuer, err := paths.ParseDigest(kitHex)
	check(w.t, err)
	server := paths.ServerName(id)
	b, err := seat.Bundle{
		V: 1, Team: id, ServerName: server, UserID: agent(id, 1), AdminID: "@admin:" + server,
		MainRoom: mainRoom, BaseURL: "http://127.0.0.1:8448", PlainHTTPHosts: []string{"127.0.0.1"},
		DeviceID: paths.SeatDevice, IssuedMS: startWall, Issuer: issuer,
	}.Encode()
	check(w.t, err)
	check(w.t, w.root.Create(s.Bundle(), b, 0o600))
	store, err := json.Marshal(state.NewStore(id, agent(id, 1), state.StoreRun{ID: w.run, StartedMS: startWall}, state.Gate{}))
	check(w.t, err)
	check(w.t, w.root.Create(s.Store(), store, 0o600))
	l, err := w.keeper.TakeSeat(w.t.Context(), w.root, s)
	check(w.t, err)
	w.t.Cleanup(func() { check(w.t, l.Close()) })
	fd, err := l.Dup()
	check(w.t, err)
	return wire.Msg{Kind: wire.KindAdd, Add: &wire.Add{Team: id, Seat: "main", RemovedUserIDs: []string{}}}, fd
}

func (w *linkWorld) add(id paths.TeamID) {
	w.t.Helper()
	m, fd := w.seat(id)
	w.send(m, fd)
	check(w.t, fd.Close())
}

// ready is a link started with the teams added and its ready seen.
func (w *linkWorld) ready(ids ...paths.TeamID) {
	w.t.Helper()
	w.start()
	w.launch(len(ids))
	for _, id := range ids {
		w.add(id)
	}
	w.next(wire.KindReady)
}

// status is a status request over link.sock, answered by the engine.
func (w *linkWorld) status() string {
	w.t.Helper()
	c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: w.layout.Run(w.run).LinkSock(), Net: "unix"})
	check(w.t, err)
	defer func() { check(w.t, c.Close()) }()
	line, err := wire.Request{
		V: 1, Op: wire.OpStatus, Kit: kitHex, DeadlineMS: (w.clk.Boot() + 2*time.Second).Milliseconds(),
		Args: json.RawMessage(`{"team":null}`),
	}.Encode()
	check(w.t, err)
	_, err = c.Write(append(line, '\n'))
	check(w.t, err)
	data, err := io.ReadAll(c)
	check(w.t, err)
	return string(data)
}

func (w *linkWorld) linkLockFree() bool {
	w.t.Helper()
	l, err := lock.TakeLink(w.t.Context(), w.clk, w.root, w.layout.Run(w.run))
	if errors.Is(err, lock.ErrBusy) {
		return false
	}
	check(w.t, err)
	check(w.t, l.Release())
	return true
}

func TestReadyComesAfterTheSeatsAdds(t *testing.T) {
	w := newLinkWorld(t)
	a, b := tid(t, "alpha.example.com"), tid(t, "beta.example.com")
	w.ready(a, b)
	if s := w.status(); !strings.Contains(s, a.String()) || !strings.Contains(s, b.String()) {
		t.Fatalf("status %q", s)
	}
	w.send(wire.Msg{Kind: wire.KindEnd, End: &wire.End{How: wire.EndStop}}, nil)
	if code := w.exited(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	ends := w.ends.endCalls()
	slices.Sort(ends)
	if len(ends) != 2 || ends[0] != "stop ccy "+a.String() {
		t.Fatalf("ends %q", ends)
	}
	if !w.linkLockFree() || !w.parent.wasClosed() {
		t.Fatal("the link left link.lock or its pidfd")
	}
	if !strings.Contains(w.log.String(), "one end failed") {
		t.Fatalf("log %q", w.log.String())
	}
}

func TestTheLinkHoldsLinkLock(t *testing.T) {
	w := newLinkWorld(t)
	w.ready()
	if w.linkLockFree() {
		t.Fatal("link.lock is free while the link runs")
	}
}

func TestAFailedAddStillCounts(t *testing.T) {
	w := newLinkWorld(t)
	a := tid(t, "alpha.example.com")
	w.start()
	w.launch(2)
	// A descriptor that is not the seat's lock: the adopt fails, and the add counts.
	m, fd := w.seat(a)
	other, err := os.Open(w.dir)
	check(t, err)
	w.send(m, other)
	check(t, other.Close())
	w.send(m, fd)
	check(t, fd.Close())
	w.next(wire.KindReady)
	if !strings.Contains(w.log.String(), "adopt") {
		t.Fatalf("log %q", w.log.String())
	}
}

func TestReadyWaitsTenSecondsForTheAdds(t *testing.T) {
	w := newLinkWorld(t)
	a, b := tid(t, "alpha.example.com"), tid(t, "beta.example.com")
	w.start()
	w.launch(2)
	w.add(a)
	until(t, func() bool { return w.team(a) != nil })
	check(t, w.clk.BlockUntil(t.Context(), 1))
	w.clk.Advance(addsWait)
	w.next(wire.KindReady)
	if !strings.Contains(w.log.String(), "1 of 2 adds") {
		t.Fatalf("log %q", w.log.String())
	}
	// The add that comes late is a hot add.
	w.add(b)
	until(t, func() bool { return w.team(b) != nil })
}

func TestAnotherKindEndsTheAddsWait(t *testing.T) {
	w := newLinkWorld(t)
	w.start()
	w.launch(1)
	w.send(wire.Msg{Kind: wire.KindPID, PID: &state.Process{PID: 100, StartTicks: 1}}, nil)
	w.next(wire.KindReady)
	if !strings.Contains(w.log.String(), "0 of 1 adds") {
		t.Fatalf("log %q", w.log.String())
	}
}

func TestTheFirstMessageIsTheLaunch(t *testing.T) {
	w := newLinkWorld(t)
	w.start()
	w.send(wire.Msg{Kind: wire.KindPID, PID: &state.Process{PID: 100, StartTicks: 1}}, nil)
	if code := w.exited(); code != int(exits.ExitConfig) {
		t.Fatalf("exit %d", code)
	}
}

func TestNoLaunchInTenSeconds(t *testing.T) {
	w := newLinkWorld(t)
	w.start()
	check(t, w.clk.BlockUntil(t.Context(), 1))
	w.clk.Advance(addsWait)
	if code := w.exited(); code != int(exits.ExitConfig) {
		t.Fatalf("exit %d", code)
	}
}

func TestAKeeperGoneBeforeTheLaunch(t *testing.T) {
	w := newLinkWorld(t)
	w.start()
	check(t, w.conn.Close())
	if code := w.exited(); code != 0 {
		t.Fatalf("exit %d", code)
	}
}

func TestAHeldLinkLockIs78(t *testing.T) {
	w := newLinkWorld(t)
	l, err := lock.TakeLink(t.Context(), w.clk, w.root, w.layout.Run(w.run))
	check(t, err)
	defer func() { check(t, l.Release()) }()
	go func() {
		// The take is tried three times, 50 ms apart on the fake clock.
		for range 2 {
			if w.clk.BlockUntil(t.Context(), 1) != nil {
				return
			}
			w.clk.Advance(time.Second)
		}
	}()
	w.start()
	if code := w.exited(); code != int(exits.ExitConfig) {
		t.Fatalf("exit %d", code)
	}
}

func TestAParentThatCannotBeWatchedIs78(t *testing.T) {
	w := newLinkWorld(t)
	w.cfg.Parent = func() (Parent, error) { return nil, errors.New("the parent changed") }
	w.start()
	if code := w.exited(); code != int(exits.ExitConfig) {
		t.Fatalf("exit %d", code)
	}
	if !w.linkLockFree() {
		t.Fatal("link.lock kept")
	}
}

func TestEOFIsTheKeeperDying(t *testing.T) {
	w := newLinkWorld(t)
	a := tid(t, "alpha.example.com")
	w.ready(a)
	check(t, w.conn.Close())
	if code := w.exited(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if ends := w.ends.endCalls(); len(ends) != 1 || ends[0] != "keeper-died ccy "+a.String() {
		t.Fatalf("ends %q", ends)
	}
	if p := w.team(a).notices.presences(); len(p) != 1 || p[0] != agent(a, 1)+" offline" {
		t.Fatalf("presence %q", p)
	}
}

func TestTheParentsExitIsTheKeeperDying(t *testing.T) {
	w := newLinkWorld(t)
	a := tid(t, "alpha.example.com")
	w.ready(a)
	close(w.parent.exited)
	if code := w.exited(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if ends := w.ends.endCalls(); len(ends) != 1 || !strings.HasPrefix(ends[0], "keeper-died") {
		t.Fatalf("ends %q", ends)
	}
}

func TestRemoveAnswersThenLeaves(t *testing.T) {
	for _, farewell := range []string{state.FarewellReleased, state.FarewellRemoved, state.FarewellParked, ""} {
		w := newLinkWorld(t)
		a := tid(t, "alpha.example.com")
		w.ready(a)
		txn := "tc" + strings.Repeat("ab", 16)
		rm := &wire.Remove{Team: a, Txn: txn}
		if farewell != "" {
			rm.Farewell = &farewell
		}
		w.send(wire.Msg{Kind: wire.KindRemove, Remove: rm}, nil)
		if r := w.next(wire.KindRemoved); r.Removed.Team != a || r.Removed.UserID != agent(a, 1) {
			t.Fatalf("removed %+v", r.Removed)
		}
		if l := w.next(wire.KindLeft); l.Left.Team != a {
			t.Fatalf("left %+v", l.Left)
		}
		leave := w.ends.leaveCalls()
		if want := farewell == state.FarewellReleased; (len(leave) == 1 && leave[0] == a.String()+" "+txn) != want {
			t.Fatalf("%s: leave %q", farewell, leave)
		}
		if p := w.team(a).notices.presences(); len(p) != 1 || !strings.HasSuffix(p[0], " offline") {
			t.Fatalf("presence %q", p)
		}
	}
}

func TestARemoveOfNoTeamIsLogged(t *testing.T) {
	w := newLinkWorld(t)
	w.ready()
	a := tid(t, "alpha.example.com")
	w.send(wire.Msg{Kind: wire.KindRemove, Remove: &wire.Remove{Team: a, Txn: "tc" + strings.Repeat("0", 32)}}, nil)
	until(t, func() bool { return strings.Contains(w.log.String(), "remove "+a.String()) })
}

func TestOwedLeavesWithoutRunning(t *testing.T) {
	w := newLinkWorld(t)
	a := tid(t, "alpha.example.com")
	w.ready()
	txn := "tc" + strings.Repeat("cd", 16)
	w.send(wire.Msg{Kind: wire.KindOwed, Owed: &wire.Owed{Team: a, Txn: txn}}, nil)
	if l := w.next(wire.KindLeft); l.Left.Team != a {
		t.Fatalf("left %+v", l.Left)
	}
	if leave := w.ends.leaveCalls(); len(leave) != 1 || leave[0] != a.String()+" "+txn {
		t.Fatalf("leave %q", leave)
	}
	if w.team(a).didRun() {
		t.Fatal("an owed team was run")
	}
}

func TestTheSessionMessagesAreKept(t *testing.T) {
	w := newLinkWorld(t)
	a := tid(t, "alpha.example.com")
	w.ready()
	secret := "sekrit-token-value"
	w.send(wire.Msg{Kind: wire.KindToken, Token: &wire.Token{Token: secret}}, nil)
	w.send(wire.Msg{Kind: wire.KindDesired, Desired: &state.ReportDesired{Generation: 2, State: state.DesiredApplied}}, nil)
	w.send(wire.Msg{Kind: wire.KindRefused, Refused: &wire.Refused{Team: a, HolderRun: w.run}}, nil)
	w.send(wire.Msg{Kind: wire.KindReady, Ready: &wire.Ready{}}, nil)
	w.send(wire.Msg{Kind: wire.KindLaunch, Launch: &wire.Launch{
		Run: w.run, Kind: state.KindCCY, Launch: state.LaunchClaude, Inbound: state.InboundAccept, Cwd: "/work",
	}}, nil)
	until(t, func() bool { return strings.Count(w.log.String(), "not the keeper's to send") == 2 })
	if s := w.status(); !strings.Contains(s, `"down":true`) {
		t.Fatalf("status %q", s)
	}
	l := w.link()
	if l.heldToken() != secret || strings.Contains(w.log.String(), secret) {
		t.Fatalf("token kept %q, log %q", l.heldToken(), w.log.String())
	}
}

func TestTheFactsReachTheKeeper(t *testing.T) {
	w := newLinkWorld(t)
	w.ready()
	w.proc.add(100, 50, 1000)
	w.proc.add(101, 100, 1001)
	w.send(wire.Msg{Kind: wire.KindPID, PID: &state.Process{PID: 100, StartTicks: 1000}}, nil)
	l := w.link()
	f := Facts{
		Op: wire.OpHookSessionStart, Peer: 101, SessionID: sessA, Source: state.SourceStartup,
		Socket: w.dir + "/xdg/cc-socks/101.sock", Transcript: w.dir + "/.claude/projects/-work/" + sessA + ".jsonl",
	}
	until(t, func() bool { return l.session.AcceptFacts(f) == nil })
	if c := w.next(wire.KindClaude); c.Claude.PID != 101 {
		t.Fatalf("claude %+v", c.Claude)
	}
	w.next(wire.KindStarted)
	select {
	case <-l.first:
	default:
		t.Fatal("FirstSessionStart not closed")
	}
	// A restarted link is told Claude Code's process by the keeper.
	w.send(wire.Msg{Kind: wire.KindClaude, Claude: &state.Process{PID: 100, StartTicks: 1000}}, nil)
	f.Op = wire.OpHookPrompt
	until(t, func() bool { return l.session.AcceptFacts(f) != nil })
}

func TestAPlainLaunchHasStartedAtOnce(t *testing.T) {
	w := newLinkWorld(t)
	w.start()
	w.send(wire.Msg{Kind: wire.KindLaunch, Launch: &wire.Launch{
		Run: w.run, Kind: state.KindHost, Launch: state.LaunchPlain, Inbound: state.InboundRefuse, Cwd: "/work",
	}}, nil)
	w.next(wire.KindReady)
	l := w.link()
	select {
	case <-l.first:
	default:
		t.Fatal("a plain launch's FirstSessionStart is open")
	}
	if l.delivery != state.DeliveryNone {
		t.Fatalf("delivery %s", l.delivery)
	}
}

func TestStatusCarriesTheRuntime(t *testing.T) {
	w := newLinkWorld(t)
	w.ready()
	if s := w.status(); !strings.Contains(s, `"kind":"runtime"`) {
		t.Fatalf("status %q", s)
	}
	w.proc.mu.Lock()
	w.proc.costErr = errors.New("no proc")
	w.proc.mu.Unlock()
	if s := w.status(); strings.Contains(s, `"kind":"runtime"`) {
		t.Fatalf("status %q", s)
	}
}
