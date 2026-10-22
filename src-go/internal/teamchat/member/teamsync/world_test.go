package teamsync

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/rooms"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
)

type noWait struct{}

func (noWait) Sleep(ctx context.Context, _ time.Duration) error { return ctx.Err() }

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

// world is team alpha on fakehs on one fake clock, the member's seat directory holding its
// pair, and the fakes of the engine and the ends.
type world struct {
	t      *testing.T
	clk    *clock.Fake
	srv    *fakehs.Server
	base   string
	adm    *matrix.Admin
	team   paths.TeamID
	self   string
	main   string
	ids    proto.RoomIDs
	roster proto.Roster
	tokens map[string]string
	layout paths.Layout
	seat   paths.Seat
	root   *homefs.Root
	eng    *fakeEngine
	ends   *fakeEnds
	log    *syncBuffer
	watch  *fakeWatcher
}

func newWorld(t *testing.T) *world {
	t.Helper()
	clk := clock.NewFake(time.Hour, startMS)
	srv := fakehs.New(fakehs.Options{
		ServerName: server, Quirks: fakehs.TuwunelQuirks(), Hold: clk.Hold,
		Now: func() time.Time { return time.UnixMilli(clk.WallMS()).UTC() },
	})
	check(t, srv.Start())
	t.Cleanup(func() {
		if err := srv.CheckModelled(); err != nil {
			t.Errorf("fakehs: %v", err)
		}
		if err := srv.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	w := emptyWorld(t, srv.URL())
	w.clk, w.srv = clk, srv
	srv.AddUser(proto.AdminLocalpart, "pw", true)
	for _, local := range accounts {
		srv.AddUser(local, "pw", false)
	}
	w.populate(func(id string) (string, error) { return srv.MintToken(id) })
	return w
}

// accounts are the world's accounts besides the admin.
var accounts = []string{alice, coord, me, peer, deaf}

func emptyWorld(t *testing.T, base string) *world {
	t.Helper()
	return &world{t: t, base: base, team: alphaID, self: me, tokens: map[string]string{}, log: &syncBuffer{}, watch: newFakeWatcher()}
}

// populate makes team alpha's rooms as the keeper does, gives each account a token by mint and
// joins it to main, and places me's pair in a new seat directory.
func (w *world) populate(mint func(userID string) (string, error)) {
	t := w.t
	t.Helper()
	w.roster = baseRoster()
	rec := &recorder{}
	if _, err := rooms.Converge(t.Context(), w.admin(), rec, rooms.Team{Server: server, Team: teamName, Roster: w.roster}, proto.RoomIDs{}); err != nil {
		t.Fatalf("Converge: %v", err)
	}
	w.ids, w.main = rec.ids, rec.ids.Main
	w.roster = w.readRoster()
	for _, local := range accounts {
		token, err := mint(user(local))
		check(t, err)
		w.tokens[local] = token
		if _, err := w.client(local).Join(t.Context(), w.main); err != nil {
			t.Fatalf("%s joins main: %v", local, err)
		}
	}
	w.seatDir()
	w.eng = newFakeEngine(t, w.team, runID(t, linkRun))
	w.ends = &fakeEnds{backed: make(chan struct{}, 8)}
}

// admin is the team's admin client: the one given, or a new token from fakehs.
func (w *world) admin() *matrix.Admin {
	w.t.Helper()
	if w.adm != nil {
		return w.adm
	}
	token, err := w.srv.MintToken(proto.AdminID(server))
	check(w.t, err)
	a, err := matrix.NewAdmin(matrix.Config{BaseURL: w.base, Token: token, Clock: noWait{}})
	check(w.t, err)
	w.t.Cleanup(a.Close)
	return a
}

// client is local's own client, on real timers, so it arms nothing on the fake clock.
func (w *world) client(local string) *matrix.Client {
	w.t.Helper()
	c, err := matrix.NewMember(matrix.Config{BaseURL: w.base, PlainHTTPHosts: []string{"127.0.0.1"}, Token: w.tokens[local], Clock: noWait{}})
	check(w.t, err)
	w.t.Cleanup(c.Close)
	return c
}

func (w *world) seatDir() {
	w.t.Helper()
	l, err := paths.HomeLayout(w.t.TempDir(), w.t.TempDir())
	check(w.t, err)
	w.layout, w.seat = l, l.Seat(w.team)
	check(w.t, os.MkdirAll(w.seat.Dir(), 0o700))
	check(w.t, os.Chmod(l.State(), 0o700))
	check(w.t, os.Chmod(w.seat.Dir(), 0o700))
	root, err := homefs.Open(l.State(), homefs.Private)
	check(w.t, err)
	w.t.Cleanup(func() { check(w.t, root.Close()) })
	w.root = root
	w.place(w.tokens[w.self], nil)
}

// as makes local the member under test: its pair placed in the seat directory.
func (w *world) as(local string) {
	w.t.Helper()
	w.self = local
	w.place(w.tokens[local], nil)
}

func (w *world) bundle() seat.Bundle {
	issuer, err := paths.ParseDigest("0123456789abcdef")
	check(w.t, err)
	return seat.Bundle{
		V: 1, Team: w.team, ServerName: server, UserID: user(w.self), AdminID: proto.AdminID(server),
		MainRoom: w.main, BaseURL: w.base, PlainHTTPHosts: []string{"127.0.0.1"},
		DeviceID: paths.SeatDevice, IssuedMS: startMS - 1000, Issuer: issuer,
	}
}

// place writes the pair, token then seat.json, each by rename as GU3 does; a released time
// writes the held form.
func (w *world) place(token string, releasedMS *int64) {
	w.t.Helper()
	w.replace(w.seat.Token(), []byte(token))
	var data []byte
	var err error
	if releasedMS == nil {
		data, err = w.bundle().Encode()
	} else {
		claim, cerr := paths.ParseClaimID("00112233aabbccdd")
		check(w.t, cerr)
		data, err = seat.HeldBundle{Bundle: w.bundle(), Claim: claim, ReleasedMS: releasedMS}.Encode()
	}
	check(w.t, err)
	w.replace(w.seat.Bundle(), data)
}

func (w *world) replace(path string, data []byte) {
	w.t.Helper()
	tmp := path + ".tmp"
	check(w.t, os.WriteFile(tmp, data, 0o600))
	check(w.t, os.Chmod(tmp, 0o600))
	check(w.t, os.Rename(tmp, path))
}

// say posts a human's message with content as given.
func (w *world) say(local string, content map[string]any) string {
	w.t.Helper()
	data, err := json.Marshal(content)
	check(w.t, err)
	id, err := w.client(local).Send(w.t.Context(), w.main, proto.MessageType, "", data)
	check(w.t, err)
	return id
}

// agentText posts local's teamchat.text to the given addressees through A's composer.
func (w *world) agentText(local, text string, to ...string) string {
	w.t.Helper()
	out, err := proto.Composer{Server: server, Self: user(local), Token: w.tokens[local], Roster: w.roster}.Text(proto.Text{To: to, Text: text}, "", text)
	check(w.t, err)
	id, err := w.client(local).Send(w.t.Context(), w.main, proto.MessageType, "", out.Content.Compact())
	check(w.t, err)
	return id
}

// readRoster is the roster main holds.
func (w *world) readRoster() proto.Roster {
	w.t.Helper()
	raw, err := w.admin().GetState(w.t.Context(), w.main, proto.RosterType, "")
	check(w.t, err)
	v, err := proto.Parse(raw)
	check(w.t, err)
	r, err := proto.DecodeRoster(v, server, teamName, w.main)
	check(w.t, err)
	return r
}

// writeRoster rewrites the roster in main as the admin does, with a new rev.
func (w *world) writeRoster(r proto.Roster) {
	w.t.Helper()
	r.Rev = w.roster.Rev + 1
	w.roster = r
	if _, err := w.admin().PutState(w.t.Context(), w.main, proto.RosterType, "", r.Encode().Compact()); err != nil {
		w.t.Fatalf("roster: %v", err)
	}
}

// --- the team under test ---

type running struct {
	tm    *Team
	polls chan string
	waits chan time.Duration
	done  chan error
	stop  context.CancelFunc
}

type option func(*Config)

func (w *world) start(opts ...option) *running {
	w.t.Helper()
	return w.startTeam(w.newTeam(opts...))
}

// startTeam runs tm, its seams set by the test, until the test ends.
func (w *world) startTeam(tm *Team) *running {
	w.t.Helper()
	r := &running{tm: tm, polls: make(chan string, 64), waits: make(chan time.Duration, 64), done: make(chan error, 1)}
	onPoll, onWait := tm.onPoll, tm.onWait
	tm.onPoll = func(since string) { onPoll(since); r.polls <- since }
	tm.onWait = func(d time.Duration) { onWait(d); r.waits <- d }
	ctx, cancel := context.WithCancel(w.t.Context())
	r.stop = cancel
	go func() { r.done <- tm.Run(ctx) }()
	w.t.Cleanup(func() {
		cancel()
		if err := <-r.done; err != nil {
			w.t.Errorf("Run: %v", err)
		}
	})
	return r
}

func (w *world) newTeam(opts ...option) *Team {
	w.t.Helper()
	first := make(chan struct{})
	close(first)
	cfg := Config{
		Team: w.team, Seat: w.seat, Root: w.root, Run: runID(w.t, linkRun), Engine: w.eng, Ends: w.ends,
		Clock: w.clk, Watcher: w.watch, FirstSessionStart: first, Log: w.log,
		Jitter: func(time.Duration) time.Duration { return 0 },
	}
	for _, o := range opts {
		o(&cfg)
	}
	tm, err := New(cfg)
	check(w.t, err)
	return tm
}

// poll waits for the team's next long-poll and gives its since.
func (r *running) poll(t *testing.T) string {
	t.Helper()
	select {
	case s := <-r.polls:
		return s
	case err := <-r.done:
		t.Fatalf("Run ended: %v", err)
	case <-t.Context().Done():
		t.Fatal("no long-poll")
	}
	return ""
}

// wait waits for the team's next backoff wait and gives its length.
func (r *running) wait(t *testing.T) time.Duration {
	t.Helper()
	select {
	case d := <-r.waits:
		return d
	case err := <-r.done:
		t.Fatalf("Run ended: %v", err)
	case <-t.Context().Done():
		t.Fatal("no wait")
	}
	return 0
}

// batch waits for the engine's next persisted batch.
func (w *world) batch() team.Batch {
	w.t.Helper()
	select {
	case b := <-w.eng.batches:
		return b
	case <-w.t.Context().Done():
		w.t.Fatal("no batch")
	}
	return team.Batch{}
}

func (w *world) noBatch() {
	w.t.Helper()
	select {
	case b := <-w.eng.batches:
		w.t.Fatalf("unexpected batch %+v", b)
	default:
	}
}

// syncs are the /sync requests fakehs received, by since ("" for none).
func (w *world) syncs() []string {
	var out []string
	for _, r := range w.srv.Requests() {
		if strings.HasSuffix(r.Path, "/sync") {
			out = append(out, r.Query.Get("since"))
		}
	}
	return out
}

func (w *world) requests(suffix string) int {
	n := 0
	for _, r := range w.srv.Requests() {
		if strings.Contains(r.Path, suffix) {
			n++
		}
	}
	return n
}

// --- fakes ---

type fakeEngine struct {
	t   *testing.T
	tm  paths.TeamID
	run paths.RunID

	mu         sync.Mutex
	store      *state.Store
	related    map[string]proto.Related
	persistErr func(team.Batch) error
	health     []team.Health
	resets     []int64
	begun      int
	views      int
	batches    chan team.Batch
	healthed   chan struct{}
}

func newFakeEngine(t *testing.T, tm paths.TeamID, run paths.RunID) *fakeEngine {
	t.Helper()
	return &fakeEngine{t: t, tm: tm, run: run, related: map[string]proto.Related{}, batches: make(chan team.Batch, 64), healthed: make(chan struct{}, 1)}
}

func (e *fakeEngine) Load(t paths.TeamID, userID string) (state.Store, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if t != e.tm {
		return state.Store{}, errors.New("fake engine: another team")
	}
	if e.store == nil {
		s := state.NewStore(t, userID, state.StoreRun{ID: e.run, StartedMS: startMS}, state.Gate{})
		e.store = &s
	}
	return *e.store, nil
}

func (e *fakeEngine) View(_ paths.TeamID, ids, relations []string, _ proto.Roster) (team.View, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.views++
	v := team.View{SeenIDs: map[string]bool{}, Events: map[string]proto.Related{}, OwnPings: map[string]proto.OwnPing{}}
	if e.store == nil {
		return v, nil
	}
	for _, p := range e.store.Pending {
		if slices.Contains(ids, p.ID) {
			v.SeenIDs[p.ID] = true
		}
	}
	for _, l := range e.store.Ledger {
		if slices.Contains(ids, l.ID) {
			v.SeenIDs[l.ID] = true
		}
	}
	for _, id := range relations {
		if r, ok := e.related[id]; ok {
			v.Events[id] = r
		}
	}
	return v, nil
}

func (e *fakeEngine) Persist(_ context.Context, b team.Batch) (int64, error) {
	e.mu.Lock()
	hook := e.persistErr
	e.mu.Unlock()
	if hook != nil {
		if err := hook(b); err != nil {
			return 0, err
		}
	}
	e.mu.Lock()
	s := e.store
	s.Sync.NextBatch, s.Sync.Fresh, s.Sync.RestoreEpoch = b.NextBatch, b.Fresh, b.RestoreEpoch
	maps.Copy(s.Sync.FloorMS, b.Floors)
	for _, it := range b.Items {
		if !slices.ContainsFunc(s.Pending, func(p state.Pending) bool { return p.ID == it.Pending.ID }) {
			s.Pending = append(s.Pending, it.Pending)
		}
	}
	floor := s.Sync.FloorMS[proto.PurposeMain]
	for _, p := range s.Pending {
		if p.Room == proto.PurposeMain && p.SentMS <= floor {
			floor = p.SentMS - 1
			s.Sync.FloorMS[proto.PurposeMain] = floor
		}
	}
	e.mu.Unlock()
	e.batches <- b
	return floor, nil
}

func (e *fakeEngine) ResetPosition(_ context.Context, _ paths.TeamID, epoch int64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.resets = append(e.resets, epoch)
	e.store.Sync.NextBatch, e.store.Sync.Fresh, e.store.Sync.RestoreEpoch = "", true, epoch
	return nil
}

func (*fakeEngine) SetEnded(context.Context, paths.TeamID, state.RunEnd) error { return nil }

func (e *fakeEngine) BeginRun(context.Context, paths.TeamID) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.begun++
	e.store.Run = state.StoreRun{ID: e.run, StartedMS: startMS}
	return nil
}

func (e *fakeEngine) SetHealth(_ paths.TeamID, h team.Health) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.health = append(e.health, h)
	select {
	case e.healthed <- struct{}{}:
	default:
	}
}

// preload gives the engine a store of an earlier run, positioned at since when it is not "".
func (e *fakeEngine) preload(userID string, run paths.RunID, since string, epoch int64) {
	s := state.NewStore(e.tm, userID, state.StoreRun{ID: run, StartedMS: startMS - 5000}, state.Gate{})
	if since != "" {
		s.Sync.NextBatch, s.Sync.Fresh = since, false
	}
	s.Sync.RestoreEpoch = epoch
	e.mu.Lock()
	defer e.mu.Unlock()
	e.store = &s
}

func (e *fakeEngine) lastHealth() team.Health {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.health) == 0 {
		return team.Health{}
	}
	return e.health[len(e.health)-1]
}

// position is the stored next_batch.
func (e *fakeEngine) position() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.store.Sync.NextBatch
}

func (e *fakeEngine) healths() []team.Health {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.health)
}

func (e *fakeEngine) begunRuns() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.begun
}

func (e *fakeEngine) resetEpochs() []int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.resets)
}

func (e *fakeEngine) pending() []state.Pending {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.store.Pending)
}

func (e *fakeEngine) setPersistErr(f func(team.Batch) error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.persistErr = f
}

type fakeEnds struct {
	mu       sync.Mutex
	closes   []state.StoreRun
	backs    []state.StoreRun
	closeErr error
	backErr  error
	backed   chan struct{}
}

func (f *fakeEnds) Close(_ context.Context, _ paths.TeamID, _ team.Notices, prev state.StoreRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closeErr != nil {
		err := f.closeErr
		f.closeErr = nil
		return err
	}
	f.closes = append(f.closes, prev)
	return nil
}

func (f *fakeEnds) Back(_ context.Context, _ paths.TeamID, _ team.Notices, run state.StoreRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.backs = append(f.backs, run)
	f.backed <- struct{}{}
	return f.backErr
}

func (*fakeEnds) End(context.Context, team.End, []team.EndTeam) []error { return nil }

func (*fakeEnds) Leave(context.Context, paths.TeamID, team.Notices, string) error { return nil }

func (f *fakeEnds) runs() ([]state.StoreRun, []state.StoreRun) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.closes), slices.Clone(f.backs)
}

func (f *fakeEnds) calls() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.closes), len(f.backs)
}

type fakeWatcher struct{ ch chan struct{} }

func newFakeWatcher() *fakeWatcher { return &fakeWatcher{ch: make(chan struct{}, 1)} }

func (f *fakeWatcher) Changes() <-chan struct{} { return f.ch }

func (f *fakeWatcher) fire() { f.ch <- struct{}{} }
