package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
)

const (
	startWall = int64(1_700_000_000_000)
	mainRoom  = "!mmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmm"
	qRoom     = "!qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq"
)

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func tid(t *testing.T, s string) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID(s)
	check(t, err)
	return id
}

func agent(tm paths.TeamID, n int) string {
	return fmt.Sprintf("@repo.%d+site.podman:%s", n, paths.ServerName(tm))
}

func alice(tm paths.TeamID) string { return "@alice:" + paths.ServerName(tm) }

func evID(n int) string { return fmt.Sprintf("$%043d", n) }

type noSleep struct{}

func (noSleep) Sleep(context.Context, time.Duration) error { return nil }

// logBuf is link.log; fail makes every write fail.
type logBuf struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	fail bool
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fail {
		return 0, errors.New("log full")
	}
	return l.buf.Write(p)
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func (l *logBuf) setFail(f bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fail = f
}

// fakeNotices is a team's client as the ends and status see it.
type fakeNotices struct {
	mu       sync.Mutex
	roster   proto.Roster
	verified bool
}

func (*fakeNotices) RoomState(context.Context, string) ([]json.RawMessage, error) { return nil, nil }

func (*fakeNotices) Messages(context.Context, string, matrix.MessagesRequest) (matrix.MessagesPage, error) {
	return matrix.MessagesPage{}, nil
}

func (*fakeNotices) Send(context.Context, string, string, string, json.RawMessage) (string, error) {
	return "", nil
}

func (*fakeNotices) SetPresence(context.Context, string, string) error { return nil }
func (*fakeNotices) Anchors() proto.Anchors                            { return proto.Anchors{} }
func (*fakeNotices) RecordOwnSend(string, proto.Related)               {}
func (*fakeNotices) Token() string                                     { return "" }

func (n *fakeNotices) Roster() (proto.Roster, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.roster, n.verified
}

func (n *fakeNotices) set(r proto.Roster) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.roster, n.verified = r, true
}

// fakeTeam is a team goroutine: it runs until cancelled (or, with ignore set, until ignore is
// closed), and answers the engine's calls from what the test set.
type fakeTeam struct {
	mu      sync.Mutex
	cfg     TeamConfig
	notices *fakeNotices
	running chan struct{}
	ignore  chan struct{}
	ran     bool

	verdicts   map[string]proto.Verdict
	refetchErr error
	block      bool
	refetched  []string
	verified   records.Verified
	verifyErr  error
	history    []records.History
	historyErr error
	flushes    int
	flushErr   error
}

func (f *fakeTeam) Run(ctx context.Context) error {
	f.mu.Lock()
	f.ran = true
	close(f.running)
	ignore := f.ignore
	f.mu.Unlock()
	if ignore != nil {
		<-ignore
		return errors.New("the team ended on its own")
	}
	<-ctx.Done()
	return ctx.Err()
}

func (f *fakeTeam) Refetch(ctx context.Context, _, id string) (proto.Verdict, error) {
	f.mu.Lock()
	f.refetched = append(f.refetched, id)
	block, v, err := f.block, f.verdicts[id], f.refetchErr
	f.mu.Unlock()
	if block {
		<-ctx.Done()
		return proto.Verdict{}, ctx.Err()
	}
	return v, err
}

func (f *fakeTeam) Verify(context.Context, string) (records.Verified, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.verified, f.verifyErr
}

func (f *fakeTeam) History(context.Context, string, int) ([]records.History, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.history, f.historyErr
}

func (f *fakeTeam) Notices() team.Notices { return f.notices }
func (*fakeTeam) Wake()                   {}

func (f *fakeTeam) FlushFloor(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flushes++
	return f.flushErr
}

func (f *fakeTeam) setVerdict(id string, v proto.Verdict) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.verdicts[id] = v
}

type fakeEnds struct{}

func (fakeEnds) Close(context.Context, paths.TeamID, team.Notices, state.StoreRun) error { return nil }

func (fakeEnds) Back(context.Context, paths.TeamID, team.Notices, state.StoreRun) error { return nil }

func (fakeEnds) End(context.Context, team.End, []team.EndTeam) []error { return nil }

func (fakeEnds) Leave(context.Context, paths.TeamID, team.Notices, string) error { return nil }

// recorder is a request's record writer.
type recorder struct {
	recs     []records.Record
	flushes  int
	err      error
	flushErr error
}

func (r *recorder) Record(rec records.Record) error {
	if r.err != nil {
		return r.err
	}
	r.recs = append(r.recs, rec)
	return nil
}

func (r *recorder) Flush() error {
	r.flushes++
	return r.flushErr
}

func (r *recorder) kinds() string {
	k := make([]string, 0, len(r.recs))
	for _, rec := range r.recs {
		k = append(k, rec.Kind())
	}
	return strings.Join(k, ",")
}

// world is a member home, a keeper holding its run, and an engine running over them.
type world struct {
	t      *testing.T
	root   *homefs.Root
	layout paths.Layout
	run    paths.RunID
	keeper *lock.KeeperLock
	clk    *clock.Fake
	log    *logBuf
	e      *Engine

	mu         sync.Mutex
	fakes      map[paths.TeamID]*fakeTeam
	states     []wire.State
	newTeamErr error
	runtimeErr error
}

func newWorld(t *testing.T, delivery string) *world {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	check(t, os.Mkdir(home, 0o700))
	root, err := homefs.Open(home, homefs.Private)
	check(t, err)
	t.Cleanup(func() { check(t, root.Close()) })
	layout, err := paths.HomeLayout(home, home+"/runtime")
	check(t, err)
	run, err := paths.ParseRunID("0123456789abcdef")
	check(t, err)
	check(t, root.MkdirAll(layout.Run(run).Dir()))
	check(t, root.MkdirAll(layout.State()))
	k, err := lock.TakeKeeper(t.Context(), noSleep{}, root, layout.Run(run))
	check(t, err)
	t.Cleanup(func() { check(t, k.Release()) })
	w := &world{
		t: t, root: root, layout: layout, run: run, keeper: k, clk: clock.NewFake(time.Hour, startWall),
		log: &logBuf{}, fakes: map[paths.TeamID]*fakeTeam{},
	}
	w.e, err = New(w.config(delivery))
	check(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	ran := make(chan error, 1)
	go func() { ran <- w.e.Run(ctx) }()
	t.Cleanup(func() {
		check(t, w.e.Shutdown(context.Background()))
		cancel()
		check(t, <-ran)
	})
	return w
}

func (w *world) config(delivery string) Config {
	return Config{
		Root: w.root, RunRoot: w.root, Layout: w.layout, Run: w.run, Clock: w.clk, Log: w.log,
		NewTeam: w.newTeam, NewEnds: func(EndsConfig) team.Ends { return fakeEnds{} },
		Delivery: delivery, OnState: w.onState, Runtime: w.readRuntime,
	}
}

func (w *world) newTeam(cfg TeamConfig) (team.Team, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.newTeamErr != nil {
		return nil, w.newTeamErr
	}
	f := w.fakeLocked(cfg.Team)
	f.cfg = cfg
	return f, nil
}

func (w *world) fake(id paths.TeamID) *fakeTeam {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.fakeLocked(id)
}

func (w *world) fakeLocked(id paths.TeamID) *fakeTeam {
	f, ok := w.fakes[id]
	if !ok {
		f = &fakeTeam{notices: &fakeNotices{}, running: make(chan struct{}), verdicts: map[string]proto.Verdict{}}
		w.fakes[id] = f
	}
	return f
}

func (w *world) onState(s wire.State) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.states = append(w.states, s)
}

func (w *world) sentStates() []wire.State {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]wire.State(nil), w.states...)
}

func (w *world) readRuntime() (records.Runtime, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return records.Runtime{Keeper: records.KeeperCost{RSSKiB: 10}}, w.runtimeErr
}

func (w *world) bundle(id paths.TeamID) []byte {
	w.t.Helper()
	issuer, err := paths.ParseDigest("0123456789abcdef")
	check(w.t, err)
	server := paths.ServerName(id)
	b := seat.Bundle{
		V: 1, Team: id, ServerName: server, UserID: agent(id, 1), AdminID: "@admin:" + server,
		MainRoom: mainRoom, BaseURL: "http://127.0.0.1:8448", PlainHTTPHosts: []string{"127.0.0.1"},
		DeviceID: paths.SeatDevice, IssuedMS: startWall, Issuer: issuer,
	}
	data, err := b.Encode()
	check(w.t, err)
	return data
}

func (w *world) newStore(id paths.TeamID) state.Store {
	return state.NewStore(id, agent(id, 1), state.StoreRun{ID: w.run, StartedMS: startWall}, state.Gate{})
}

// seat makes id's seat directory with its seat.json (unless bare) and store.json (when given),
// and takes its lock.
func (w *world) seat(id paths.TeamID, bare bool, store []byte) *lock.SeatLock {
	w.t.Helper()
	s := w.layout.Seat(id)
	check(w.t, w.root.MkdirAll(s.Dir()))
	exists, err := w.root.Exists(s.Bundle())
	check(w.t, err)
	if !bare && !exists {
		check(w.t, w.root.Create(s.Bundle(), w.bundle(id), 0o600))
	}
	if store != nil {
		check(w.t, w.root.Create(s.Store(), store, 0o600))
	}
	l, err := w.keeper.TakeSeat(w.t.Context(), w.root, s)
	check(w.t, err)
	return l
}

func encodeStore(t *testing.T, s state.Store) []byte {
	t.Helper()
	data, err := json.Marshal(s)
	check(t, err)
	if _, err := state.DecodeStore(data); err != nil {
		t.Fatalf("the test's store does not decode: %v", err)
	}
	return data
}

// add holds id with its bundle and the given store.json (nil: none yet).
func (w *world) add(id paths.TeamID, store []byte) *fakeTeam {
	w.t.Helper()
	f := w.fake(id)
	check(w.t, w.e.Add(w.t.Context(), id, w.seat(id, false, store), nil))
	return f
}

// held is id held with a fresh store, its team running.
func (w *world) held(id paths.TeamID) *fakeTeam {
	w.t.Helper()
	f := w.add(id, encodeStore(w.t, w.newStore(id)))
	<-f.running
	return f
}

func (w *world) store(id paths.TeamID) state.Store {
	w.t.Helper()
	f, err := w.root.Read(w.layout.Seat(id).Store(), 1<<22)
	check(w.t, err)
	s, err := state.DecodeStore(f.Data)
	check(w.t, err)
	return s
}

func (w *world) storeExists(id paths.TeamID) bool {
	w.t.Helper()
	ok, err := w.root.Exists(w.layout.Seat(id).Store())
	check(w.t, err)
	return ok
}

func (w *world) status() state.Status {
	w.t.Helper()
	f, err := w.root.Read(w.layout.Run(w.run).Status(), 1<<20)
	check(w.t, err)
	s, err := state.DecodeStatus(f.Data)
	check(w.t, err)
	return s
}

func (w *world) persist(id paths.TeamID, items ...team.Item) {
	w.t.Helper()
	check(w.t, w.e.Persist(w.t.Context(), team.Batch{Team: id, NextBatch: "s1", Items: items}))
}

func (w *world) serve(ctx context.Context, op string, args any) (wire.Done, *recorder) {
	w.t.Helper()
	raw, err := json.Marshal(args)
	check(w.t, err)
	out := &recorder{}
	return w.e.Serve(ctx, wire.Request{V: 1, Op: op, Args: raw}, out), out
}

// done is a request's answer alone.
func (w *world) done(op string, args any) wire.Done {
	w.t.Helper()
	d, out := w.serve(w.t.Context(), op, args)
	if out == nil {
		w.t.Fatal("no record writer")
	}
	return d
}

// local runs f on the state goroutine, for a look at the engine's memory.
func (w *world) local(f func()) {
	w.t.Helper()
	check(w.t, w.e.do(w.t.Context(), f))
}

// until polls the state goroutine until cond holds.
func (w *world) until(cond func() bool) {
	w.t.Helper()
	for {
		var ok bool
		w.local(func() { ok = cond() })
		if ok {
			return
		}
		runtime.Gosched()
	}
}

func humanItem(id paths.TeamID, n int, sent int64, text string) team.Item {
	return team.Item{
		Pending: state.Pending{ID: evID(n), Room: proto.PurposeMain, Item: records.KindHuman, SentMS: sent, From: alice(id)},
		Verdict: proto.Verdict{Outcome: proto.Deliver, Event: proto.Event{ID: evID(n), OriginTs: sent}, Human: proto.Human{Body: text}},
	}
}

func TestNewRefusesAnIncompleteConfig(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	good := w.config(state.DeliverySocket)
	for name, mutate := range map[string]func(*Config){
		"no root":     func(c *Config) { c.Root = nil },
		"no run root": func(c *Config) { c.RunRoot = nil },
		"no clock":    func(c *Config) { c.Clock = nil },
		"no log":      func(c *Config) { c.Log = nil },
		"no team":     func(c *Config) { c.NewTeam = nil },
		"no ends":     func(c *Config) { c.NewEnds = nil },
		"no state":    func(c *Config) { c.OnState = nil },
		"no runtime":  func(c *Config) { c.Runtime = nil },
		"a delivery":  func(c *Config) { c.Delivery = "post" },
		"no run":      func(c *Config) { c.Run = paths.RunID{} },
	} {
		cfg := good
		mutate(&cfg)
		if _, err := New(cfg); err == nil {
			t.Errorf("%s: New succeeded", name)
		}
	}
	if w.e.Ends() == nil {
		t.Fatal("the engine has no ends")
	}
}

// The ends record into the engine for this link's run (EN1, 8a P6), and a team is told the run
// so a store of this run is not closed again (8a P5).
func TestTheEndsAndEachTeamAreMadeForThisRun(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	cfg := w.config(state.DeliverySocket)
	var got EndsConfig
	cfg.NewEnds = func(c EndsConfig) team.Ends {
		got = c
		return fakeEnds{}
	}
	e, err := New(cfg)
	check(t, err)
	if got.Engine != e || got.Run != w.run || got.Clock != w.clk || got.Log != w.log {
		t.Fatalf("the ends were made with %+v", got)
	}
	ops := tid(t, "ops")
	w.add(ops, encodeStore(t, w.newStore(ops)))
	if r := w.fake(ops).cfg.Run; r != w.run {
		t.Fatalf("the team was made for run %v, not %v", r, w.run)
	}
}

func TestACallAfterTheGoroutineEndedIsStopped(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	e, err := New(w.config(state.DeliverySocket))
	check(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	check(t, e.Run(ctx))
	if _, err := e.Load(tid(t, "ops"), agent(tid(t, "ops"), 1)); !errors.Is(err, ErrStopped) {
		t.Fatalf("Load after the end = %v, want ErrStopped", err)
	}
	if err := e.do(ctx, func() {}); !errors.Is(err, context.Canceled) && !errors.Is(err, ErrStopped) {
		t.Fatalf("do on an ended context = %v", err)
	}
	e.SetHealth(tid(t, "ops"), team.Health{})
	if d := e.Serve(t.Context(), wire.Request{Op: wire.OpRecv, Args: json.RawMessage(`{"team":null}`)}, &recorder{}); d.Exit != 1 {
		t.Fatalf("recv after the end = %+v, want internal", d)
	}
}

func TestALostLogLineIsCountedInTheNext(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	w.log.setFail(true)
	w.e.logf("one")
	w.e.logf("two")
	w.log.setFail(false)
	w.e.logf("three")
	if got := w.log.String(); got != "engine: 2 log lines were lost\nengine: three\n" {
		t.Fatalf("link.log = %q", got)
	}
}
