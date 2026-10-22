package health

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

const (
	podman = "web.1+desk.podman"
	lxcA   = "web.2+box.lxc"
	runA   = "0123456789abcdef"
	runB   = "fedcba9876543210"
)

var t0 = time.UnixMilli(1_800_000_000_000).UTC()

func id(t *testing.T, s string) paths.TeamID {
	t.Helper()
	team, err := paths.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return team
}

// clock is the tick's time: Sleep moves it on.
type clock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Sleep(_ context.Context, d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	c.sleeps = append(c.sleeps, d)
	return nil
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type teams struct {
	list    []Team
	err     error
	removed map[paths.TeamID]bool
}

func (f *teams) Stamped(context.Context) ([]Team, error) { return f.list, f.err }

func (f *teams) Stamp(_ context.Context, team paths.TeamID) (bool, error) {
	return !f.removed[team], nil
}

type units struct {
	mu     sync.Mutex
	states []UnitState // answered in turn, the last one repeated
	err    error
	// afterRestart becomes err once Restart is called, as a bus lost during the restart would.
	afterRestart error
	restarts     int
	inflight     atomic.Int32
	maxSeen      atomic.Int32
	gate         chan struct{}
	stateCall    int
}

func (u *units) State(context.Context, string) (UnitState, error) {
	n := u.inflight.Add(1)
	defer u.inflight.Add(-1)
	for {
		seen := u.maxSeen.Load()
		if n <= seen || u.maxSeen.CompareAndSwap(seen, n) {
			break
		}
	}
	if u.gate != nil {
		<-u.gate
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.err != nil {
		return UnitState{}, u.err
	}
	s := u.states[min(u.stateCall, len(u.states)-1)]
	u.stateCall++
	return s, nil
}

func (u *units) Restart(context.Context, string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.restarts++
	if u.afterRestart != nil {
		u.err = u.afterRestart
	}
	return nil
}

type prober struct {
	mu    sync.Mutex
	fails int // how many probes fail, from the first
	calls int
	// on runs at each call with its number, as a team remove meanwhile would.
	on func(call int)
}

func (p *prober) Probe(context.Context, paths.TeamID) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.on != nil {
		p.on(p.calls)
	}
	if p.calls <= p.fails {
		return errors.New("connection refused")
	}
	return nil
}

type viewer struct {
	view View
	err  error
	// during runs inside the view, as another writer would.
	during func()
}

func (v *viewer) View(context.Context, paths.TeamID) (View, error) {
	if v.during != nil {
		v.during()
	}
	return v.view, v.err
}

type backups struct{ state BackupState }

func (b *backups) State(context.Context, paths.TeamID) (BackupState, error) { return b.state, nil }

type space struct{ free, total int64 }

func (s *space) Space() (int64, int64, error) { return s.free, s.total, nil }

type guests struct {
	probes map[string]GuestProbe
	err    error
}

func (g *guests) Probe(_ context.Context, _ paths.TeamID, handle string) (GuestProbe, error) {
	return g.probes[handle], g.err
}

type asked struct {
	handle  string
	run     string
	sinceMS int64
}

type announcer struct {
	mu    sync.Mutex
	asked []asked
	err   error
	out   Notice
}

func (a *announcer) Disconnected(_ context.Context, _ paths.TeamID, handle string, run paths.RunID, sinceMS int64) (Notice, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.asked = append(a.asked, asked{handle, run.String(), sinceMS})
	return a.out, a.err
}

type locks struct {
	dir  string
	busy map[paths.TeamID]bool
}

func (l *locks) TryLock(team paths.TeamID) (Unlocker, error) {
	if l.busy[team] {
		return nil, &teamlock.BusyError{Holder: "apply " + team.String() + " pid=9 since=1"}
	}
	return teamlock.TryLock(filepath.Join(l.dir, team.String()+".lock"), teamlock.Holder{Verb: "health", Team: team, PID: 1, Since: t0})
}

// world is one tick's fakes over a temporary status tree.
type world struct {
	dir       string
	clock     *clock
	teams     *teams
	units     *units
	prober    *prober
	viewer    *viewer
	backups   *backups
	space     *space
	guests    *guests
	announcer *announcer
	locks     *locks
}

func newWorld(t *testing.T, names ...string) *world {
	t.Helper()
	dir := t.TempDir()
	w := &world{
		dir:    dir,
		clock:  &clock{now: t0},
		teams:  &teams{removed: map[paths.TeamID]bool{}},
		units:  &units{states: []UnitState{{Active: "active", Sub: "running", InvocationID: "inv1", ActiveSince: t0.Add(-time.Hour)}}},
		prober: &prober{},
		viewer: &viewer{view: View{
			Members: []status.Member{
				{Handle: podman, Kind: "podman", Role: "coordinator", State: "active", Rooms: map[string]string{"main": "join"}},
				{Handle: lxcA, Kind: status.KindLXC, Role: "worker", State: "active", Rooms: map[string]string{"main": "join"}},
			},
			Humans: []status.Human{{Name: "ann", State: "active", Rooms: map[string]string{"main": "join"}, Devices: 1}},
		}},
		backups:   &backups{state: BackupState{Newest: &status.Newest{ID: 3, TimeMS: t0.Add(-time.Hour).UnixMilli()}, LastRun: status.RunOK}},
		space:     &space{free: 100 << 30, total: 200 << 30},
		guests:    &guests{probes: map[string]GuestProbe{lxcA: {Held: true, Record: lockRecord(runA, false)}}},
		announcer: &announcer{out: Notice{Posted: true}},
		locks:     &locks{dir: dir, busy: map[paths.TeamID]bool{}},
	}
	for _, n := range names {
		team := id(t, n)
		if err := os.MkdirAll(filepath.Join(dir, n), 0o700); err != nil {
			t.Fatal(err)
		}
		w.teams.list = append(w.teams.list, Team{ID: team, Name: strings.ToUpper(n[:1]) + n[1:], Origin: status.OriginV2, AppliedMS: t0.Add(-48 * time.Hour).UnixMilli()})
	}
	return w
}

func lockRecord(run string, failed bool) []byte {
	if failed {
		return []byte(`{"v":1,"run":"` + run + `","started_ms":1,"launch_failed_ms":2}`)
	}
	return []byte(`{"v":1,"run":"` + run + `","started_ms":1}`)
}

func (w *world) tick(t *testing.T) *Tick {
	t.Helper()
	tick, err := New(Config{
		Clock: w.clock, Teams: w.teams, Units: w.units, Prober: w.prober, Viewer: w.viewer,
		Backups: w.backups, Space: w.space, Guests: w.guests, Announcer: w.announcer, Locks: w.locks,
		StatusPath:    func(team paths.TeamID) string { return filepath.Join(w.dir, team.String(), "status.json") },
		LXCOnlinePath: func(team paths.TeamID) string { return filepath.Join(w.dir, team.String(), "lxc-online.json") },
		GID:           -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return tick
}

func (w *world) run(t *testing.T) []Outcome {
	t.Helper()
	out, err := w.tick(t).RunAll(context.Background())
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	return out
}

// record is alpha's status record.
func (w *world) record(t *testing.T, _ string) status.Record {
	t.Helper()
	rec, found, err := status.Read(filepath.Join(w.dir, "alpha", "status.json"), id(t, "alpha"))
	if err != nil || !found {
		t.Fatalf("record of alpha: %v, %v", found, err)
	}
	return rec
}

func whats(problems []status.Problem) []string {
	out := make([]string, 0, len(problems))
	for _, p := range problems {
		out = append(out, p.What)
	}
	return out
}

func hasProblem(rec status.Record, prefix string) bool {
	return slices.ContainsFunc(rec.Problems, func(p status.Problem) bool { return strings.HasPrefix(p.What, prefix) })
}

func TestNewRefusesAMissingPart(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("New with nothing succeeded")
	}
}

func TestAHealthyTeamIsWritten(t *testing.T) {
	w := newWorld(t, "alpha")
	out := w.run(t)
	if len(out) != 1 || out[0].Result != ResultWritten {
		t.Fatalf("outcomes %+v", out)
	}
	rec := w.record(t, "alpha")
	if rec.Seq != 1 || rec.Name != "Alpha" || rec.Origin != status.OriginV2 || rec.WrittenMS != t0.UnixMilli() {
		t.Fatalf("record %+v", rec)
	}
	if rec.Homeserver.Unit != "active" || rec.Homeserver.Probe != status.ProbeOK || rec.Homeserver.SinceMS != t0.Add(-time.Hour).UnixMilli() {
		t.Fatalf("homeserver %+v", rec.Homeserver)
	}
	if len(rec.Problems) != 0 {
		t.Fatalf("problems %v", whats(rec.Problems))
	}
	checkHealthyParts(t, rec)
	w.clock.advance(time.Minute)
	w.run(t)
	if again := w.record(t, "alpha"); again.Seq != 2 {
		t.Fatalf("second tick seq %d", again.Seq)
	}
}

func checkHealthyParts(t *testing.T, rec status.Record) {
	t.Helper()
	if rec.Members[1].Online == nil || !*rec.Members[1].Online || rec.Members[0].Online != nil {
		t.Fatal("online is not the LXC probe")
	}
	if rec.Disk.FreeBytes != 100<<30 || rec.Disk.FreePct != 50 || rec.Backup.Newest.ID != 3 {
		t.Fatalf("disk %+v backup %+v", rec.Disk, rec.Backup)
	}
}

func TestOneTeamsFailureIsItsOwn(t *testing.T) {
	w := newWorld(t, "alpha", "beta", "gamma")
	// beta's status directory is missing, so its write fails.
	if err := os.Remove(filepath.Join(w.dir, "beta")); err != nil {
		t.Fatal(err)
	}
	out := w.run(t)
	results := map[string]string{}
	for _, o := range out {
		results[o.Team.String()] = o.Result
	}
	if results["alpha"] != ResultWritten || results["gamma"] != ResultWritten || results["beta"] != ResultFailed {
		t.Fatalf("results %v", results)
	}
	if out[1].Err == nil {
		t.Fatal("a failed team carries no error")
	}
}

func TestRunAllListingFails(t *testing.T) {
	w := newWorld(t)
	w.teams.err = errors.New("no stamps")
	if _, err := w.tick(t).RunAll(context.Background()); err == nil {
		t.Fatal("RunAll hid the listing failure")
	}
}

func TestAtMostFourTeamsAtOnce(t *testing.T) {
	w := newWorld(t, "a1", "a2", "a3", "a4", "a5", "a6")
	w.units.gate = make(chan struct{})
	done := make(chan []Outcome)
	go func() {
		out, err := w.tick(t).RunAll(context.Background())
		if err != nil {
			t.Error(err)
		}
		done <- out
	}()
	for w.units.inflight.Load() < MaxParallel {
		runtime.Gosched()
	}
	close(w.units.gate)
	out := <-done
	if len(out) != 6 || w.units.maxSeen.Load() != MaxParallel {
		t.Fatalf("%d outcomes, at most %d at once", len(out), w.units.maxSeen.Load())
	}
}

func TestMissesInTheStartGraceAreNotCounted(t *testing.T) {
	w := newWorld(t, "alpha")
	w.units.states = []UnitState{{Active: "active", InvocationID: "inv1", ActiveSince: t0.Add(-30 * time.Second)}}
	w.prober.fails = 3
	w.run(t)
	rec := w.record(t, "alpha")
	if rec.Homeserver.Probe != status.ProbeMissed || w.units.restarts != 0 || len(w.clock.sleeps) != 0 {
		t.Fatalf("probe %s, %d restarts, sleeps %v", rec.Homeserver.Probe, w.units.restarts, w.clock.sleeps)
	}
}

func TestThreeMissesRestartTheUnit(t *testing.T) {
	w := newWorld(t, "alpha")
	w.prober.fails = 3
	out := w.run(t)
	if out[0].Result != ResultWritten || w.units.restarts != 1 {
		t.Fatalf("outcome %+v, %d restarts", out[0], w.units.restarts)
	}
	if !slices.Equal(w.clock.sleeps, []time.Duration{ProbeGap, ProbeGap}) {
		t.Fatalf("sleeps %v", w.clock.sleeps)
	}
	rec := w.record(t, "alpha")
	if rec.Seq != 2 || len(rec.Events) != 1 || rec.Events[0].What != EventRestarted {
		t.Fatalf("seq %d events %+v", rec.Seq, rec.Events)
	}
}

func TestTwoMissesThenAnAnswerDoNotRestart(t *testing.T) {
	w := newWorld(t, "alpha")
	w.prober.fails = 2
	w.run(t)
	if w.units.restarts != 0 || w.record(t, "alpha").Homeserver.Probe != status.ProbeOK {
		t.Fatal("an answered probe restarted the unit")
	}
}

func TestANewInvocationIsNotRestarted(t *testing.T) {
	w := newWorld(t, "alpha")
	w.prober.fails = 3
	w.units.states = []UnitState{
		{Active: "active", InvocationID: "inv1", ActiveSince: t0.Add(-time.Hour)},
		{Active: "active", InvocationID: "inv2", ActiveSince: t0.Add(-time.Hour)},
	}
	w.run(t)
	rec := w.record(t, "alpha")
	if w.units.restarts != 0 || rec.Homeserver.Probe != status.ProbeNotRun {
		t.Fatalf("a unit restarted meanwhile was restarted again, or its probe was recorded as answered: %+v", rec.Homeserver)
	}
	if rec.Homeserver.Unit != "active" || len(rec.Problems) != 0 {
		t.Fatalf("a new active invocation: %+v, problems %v", rec.Homeserver, whats(rec.Problems))
	}
}

// HT2: a self-restart seen during the re-probes is an event, as one seen at step 1 is.
func TestASelfRestartDuringTheReprobesIsAnEvent(t *testing.T) {
	w := newWorld(t, "alpha")
	w.units.states = []UnitState{{Active: "active", InvocationID: "inv1", NRestarts: 1, ActiveSince: t0.Add(-time.Hour)}}
	w.run(t)
	w.clock.advance(time.Minute)
	w.prober.fails = 3
	w.units.states, w.units.stateCall = []UnitState{
		{Active: "active", InvocationID: "inv1", NRestarts: 1, ActiveSince: t0.Add(-time.Hour)},
		{Active: "active", InvocationID: "inv2", NRestarts: 2, ActiveSince: w.clock.Now()},
	}, 0
	w.run(t)
	rec := w.record(t, "alpha")
	want := fmt.Sprintf(EventSelfRestart, 2)
	if rec.Homeserver.NRestarts != 2 || !slices.ContainsFunc(rec.Events, func(e status.Event) bool { return e.What == want }) {
		t.Fatalf("homeserver %+v, events %+v", rec.Homeserver, rec.Events)
	}
}

// HT2: a unit whose state cannot be read after the tick's restart is a problem on the record.
func TestAnUnreadStateAfterTheRestartIsAProblem(t *testing.T) {
	w := newWorld(t, "alpha")
	w.prober.fails = 3
	w.units.afterRestart = errors.New("dbus down")
	w.run(t)
	rec := w.record(t, "alpha")
	if w.units.restarts != 1 || !hasProblem(rec, "the homeserver unit's state cannot be read after its restart") {
		t.Fatalf("restarts %d, problems %v", w.units.restarts, whats(rec.Problems))
	}
}

// HT2: a unit that stops being active during the re-probes is not-run; one whose state cannot
// be read then is missed. Neither is restarted, and neither is recorded as answered.
func TestAReprobeThatIsNotAnAnswer(t *testing.T) {
	w := newWorld(t, "alpha")
	w.prober.fails = 3
	w.units.states = []UnitState{
		{Active: "active", InvocationID: "inv1", ActiveSince: t0.Add(-time.Hour)},
		{Active: "deactivating", InvocationID: "inv1"},
	}
	w.run(t)
	rec := w.record(t, "alpha")
	if w.units.restarts != 0 || rec.Homeserver.Probe != status.ProbeNotRun {
		t.Fatalf("not active: %d restarts, %+v", w.units.restarts, rec.Homeserver)
	}
	if rec.Homeserver.Unit != "deactivating" || rec.Homeserver.SinceMS != 0 || !hasProblem(rec, "the homeserver unit is deactivating") {
		t.Fatalf("the record keeps the first read beside not-run: %+v, problems %v", rec.Homeserver, whats(rec.Problems))
	}
	w = newWorld(t, "alpha")
	w.prober.fails = 3
	w.prober.on = func(call int) {
		if call == 1 {
			w.units.err = errors.New("dbus down")
		}
	}
	w.run(t)
	if w.units.restarts != 0 || w.record(t, "alpha").Homeserver.Probe != status.ProbeMissed {
		t.Fatalf("unread: %d restarts, %+v", w.units.restarts, w.record(t, "alpha").Homeserver)
	}
}

// HT2, HT5: the restart re-checks the stamp under the lock, so a team removed during the
// re-probes is neither restarted nor written.
func TestARestartRechecksTheStamp(t *testing.T) {
	w := newWorld(t, "alpha")
	w.prober.fails = 3
	w.prober.on = func(call int) {
		if call == 3 {
			w.teams.removed[id(t, "alpha")] = true
		}
	}
	out := w.run(t)
	if w.units.restarts != 0 || out[0].Result != ResultDropped || out[0].Why != "the team was removed" {
		t.Fatalf("restarts %d outcome %+v", w.units.restarts, out[0])
	}
	if _, err := os.Stat(filepath.Join(w.dir, "alpha", "status.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a removed team's record was written")
	}
}

// HT2, n2e-D4: the tick's own restart records the unit's NRestarts after it, so a counter the
// restart reset is not the next tick's "restarted by hand".
func TestTheTicksRestartIsNotRestartedByHand(t *testing.T) {
	w := newWorld(t, "alpha")
	w.units.states = []UnitState{{Active: "active", InvocationID: "inv1", NRestarts: 3, ActiveSince: t0.Add(-time.Hour)}}
	w.run(t)
	w.clock.advance(time.Minute)
	before := UnitState{Active: "active", InvocationID: "inv1", NRestarts: 3, ActiveSince: t0.Add(-time.Hour)}
	after := UnitState{Active: "active", InvocationID: "inv2", ActiveSince: w.clock.Now()}
	w.units.states, w.units.stateCall = []UnitState{before, before, before, after}, 0
	w.prober.fails = 4
	w.run(t)
	if w.units.restarts != 1 || w.record(t, "alpha").Homeserver.NRestarts != 0 {
		t.Fatalf("restarts %d, homeserver %+v", w.units.restarts, w.record(t, "alpha").Homeserver)
	}
	w.clock.advance(time.Minute)
	w.run(t)
	for _, e := range w.record(t, "alpha").Events {
		if e.What == EventByHand {
			t.Fatalf("events %+v", w.record(t, "alpha").Events)
		}
	}
}

func TestABusyLockMeansNoRestartAndNoWrite(t *testing.T) {
	w := newWorld(t, "alpha")
	w.prober.fails = 3
	w.locks.busy[id(t, "alpha")] = true
	out := w.run(t)
	if w.units.restarts != 0 || out[0].Result != ResultDropped || !strings.Contains(out[0].Why, "busy") {
		t.Fatalf("restarts %d outcome %+v", w.units.restarts, out[0])
	}
	if _, err := os.Stat(filepath.Join(w.dir, "alpha", "status.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a busy team's record was written")
	}
}

func TestNRestarts(t *testing.T) {
	w := newWorld(t, "alpha")
	w.run(t)
	w.units.states = []UnitState{{Active: "active", InvocationID: "inv2", NRestarts: 2, ActiveSince: t0}}
	w.units.stateCall = 0
	w.clock.advance(time.Minute)
	w.run(t)
	rec := w.record(t, "alpha")
	if len(rec.Events) != 1 || rec.Events[0].What != fmt.Sprintf(EventSelfRestart, 2) || rec.Homeserver.NRestarts != 2 {
		t.Fatalf("events %+v", rec.Events)
	}
	w.units.states = []UnitState{{Active: "active", InvocationID: "inv3", NRestarts: 0, ActiveSince: t0}}
	w.units.stateCall = 0
	w.clock.advance(time.Minute)
	w.run(t)
	rec = w.record(t, "alpha")
	if len(rec.Events) != 2 || rec.Events[0].What != EventByHand {
		t.Fatalf("events %+v", rec.Events)
	}
	w.clock.advance(25 * time.Hour)
	w.run(t)
	if rec = w.record(t, "alpha"); len(rec.Events) != 0 {
		t.Fatalf("events older than a day kept: %+v", rec.Events)
	}
}

func TestAUnitNotActiveIsNotProbed(t *testing.T) {
	w := newWorld(t, "alpha")
	w.units.states = []UnitState{{Active: "failed", Sub: "failed", InvocationID: "inv1"}}
	w.run(t)
	rec := w.record(t, "alpha")
	if rec.Homeserver.Probe != status.ProbeNotRun || w.prober.calls != 0 || !hasProblem(rec, "the homeserver unit is failed") {
		t.Fatalf("homeserver %+v problems %v", rec.Homeserver, whats(rec.Problems))
	}
}

func TestAUnitStateThatCannotBeReadIsAProblem(t *testing.T) {
	w := newWorld(t, "alpha")
	w.units.err = errors.New("dbus down")
	w.run(t)
	rec := w.record(t, "alpha")
	if rec.Homeserver.Unit != "inactive" || !hasProblem(rec, "the homeserver unit's state cannot be read") {
		t.Fatalf("homeserver %+v problems %v", rec.Homeserver, whats(rec.Problems))
	}
	for _, p := range rec.Problems {
		if strings.Contains(p.What, "dbus down") {
			t.Fatal("an error's text reached the record")
		}
	}
}

func TestAViewThatFailsIsAProblemAndTheRestIsWritten(t *testing.T) {
	w := newWorld(t, "alpha")
	w.viewer.err = fmt.Errorf("the child: %w", context.DeadlineExceeded)
	w.run(t)
	rec := w.record(t, "alpha")
	if !slices.Contains(rec.Problems, status.Problem{
		What: "the team's rooms could not be read: timed out", Fix: "teamchat-admin health alpha, then systemctl status teamchat-server@alpha.service",
	}) || rec.Backup.Newest == nil {
		t.Fatalf("problems %+v", rec.Problems)
	}
}

// HT3: a view that fails on the team's own files, or whose child does not answer, is its own
// class with its own fix, never sent to a healthy homeserver.
func TestAViewFailureNamesItsOwnClass(t *testing.T) {
	for _, c := range []struct {
		err  error
		want status.Problem
	}{
		{fmt.Errorf("%w: team.json is missing", ErrViewFiles), status.Problem{
			What: "the team's rooms could not be read: the team's files could not be read",
			Fix:  "teamchat-admin health alpha, then apply the team again (playbooks/imports/play-teamchat.yml)",
		}},
		{fmt.Errorf("%w: exit status 2", ErrViewChild), status.Problem{
			What: "the team's rooms could not be read: the view's child did not answer",
			Fix:  "teamchat-admin health alpha",
		}},
	} {
		w := newWorld(t, "alpha")
		w.viewer.err = c.err
		w.run(t)
		if rec := w.record(t, "alpha"); !slices.Contains(rec.Problems, c.want) {
			t.Errorf("%v: problems %+v", c.err, rec.Problems)
		}
	}
}

// HT3, HT6: a view that fails leaves step 6 alone: lxc-online.json is kept as it was, so a run
// that ended meanwhile is announced once the view is back.
func TestAViewThatFailsKeepsTheOnlineRecord(t *testing.T) {
	w := newWorld(t, "alpha")
	w.run(t)
	was := lxcEntry(t, w)
	w.viewer.err = errors.New("child failed")
	w.guests.probes[lxcA] = GuestProbe{Record: lockRecord(runA, false)}
	w.clock.advance(time.Minute)
	w.run(t)
	if got := lxcEntry(t, w); got != was || len(w.announcer.asked) != 0 {
		t.Fatalf("entry %+v, asked %+v", got, w.announcer.asked)
	}
	w.viewer.err = nil
	w.clock.advance(time.Minute)
	w.run(t)
	if len(w.announcer.asked) != 1 || w.announcer.asked[0] != (asked{lxcA, runA, t0.UnixMilli()}) {
		t.Fatalf("asked %+v", w.announcer.asked)
	}
}

func TestTheViewsProblemsAreKept(t *testing.T) {
	w := newWorld(t, "alpha")
	w.viewer.view.Problems = []status.Problem{{What: "web.1+desk.podman is deactivated", Fix: "teamchat-admin member retire"}}
	w.run(t)
	if !hasProblem(w.record(t, "alpha"), "web.1+desk.podman is deactivated") {
		t.Fatal("the view's problem was lost")
	}
}

func TestAnotherWriterDropsTheView(t *testing.T) {
	w := newWorld(t, "alpha")
	w.run(t)
	path := filepath.Join(w.dir, "alpha", "status.json")
	w.viewer.during = func() {
		rec, _, err := status.Read(path, id(t, "alpha"))
		if err != nil {
			t.Error(err)
			return
		}
		next := status.Next(&rec, rec.Team, w.clock.Now())
		next.Problems = []status.Problem{{What: "written by apply", Fix: "none"}}
		if err := status.Write(path, next, -1); err != nil {
			t.Error(err)
		}
	}
	w.clock.advance(time.Minute)
	out := w.run(t)
	if out[0].Result != ResultDropped || !hasProblem(w.record(t, "alpha"), "written by apply") {
		t.Fatalf("outcome %+v", out[0])
	}
}

func TestARemovedTeamIsNotWritten(t *testing.T) {
	w := newWorld(t, "alpha")
	w.teams.removed[id(t, "alpha")] = true
	out := w.run(t)
	if out[0].Result != ResultDropped {
		t.Fatalf("outcome %+v", out[0])
	}
	if _, err := os.Stat(filepath.Join(w.dir, "alpha", "status.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a removed team's record was written")
	}
}

func TestBackups(t *testing.T) {
	cases := map[string]struct {
		state   BackupState
		applied time.Duration
		problem bool
	}{
		"fresh":          {BackupState{Newest: &status.Newest{ID: 1, TimeMS: t0.Add(-35 * time.Hour).UnixMilli()}, LastRun: status.RunOK}, 48 * time.Hour, false},
		"old":            {BackupState{Newest: &status.Newest{ID: 1, TimeMS: t0.Add(-37 * time.Hour).UnixMilli()}, LastRun: status.RunOK}, 48 * time.Hour, true},
		"failed":         {BackupState{Newest: &status.Newest{ID: 1, TimeMS: t0.Add(-time.Hour).UnixMilli()}, LastRun: status.RunFailed}, 48 * time.Hour, true},
		"none yet":       {BackupState{LastRun: status.RunNone}, time.Hour, false},
		"none for long":  {BackupState{LastRun: status.RunNone}, 37 * time.Hour, true},
		"none but a run": {BackupState{LastRun: status.RunOK}, 37 * time.Hour, true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, "alpha")
			w.backups.state = c.state
			w.teams.list[0].AppliedMS = t0.Add(-c.applied).UnixMilli()
			w.run(t)
			rec := w.record(t, "alpha")
			if got := hasProblem(rec, "the backup"); got != c.problem {
				t.Fatalf("problem %v, want %v: %v", got, c.problem, whats(rec.Problems))
			}
		})
	}
}

func TestDisk(t *testing.T) {
	cases := map[string]struct {
		free, total int64
		problem     bool
	}{
		"plenty":         {100 << 30, 200 << 30, false},
		"under 2 GiB":    {1 << 30, 5 << 30, true},
		"under a tenth":  {9 << 30, 100 << 30, true},
		"a tenth exact":  {10 << 30, 100 << 30, false},
		"small and full": {3 << 30, 10 << 30, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, "alpha")
			w.space.free, w.space.total = c.free, c.total
			w.run(t)
			if got := hasProblem(w.record(t, "alpha"), "free space on /var/lib"); got != c.problem {
				t.Fatalf("problem %v, want %v", got, c.problem)
			}
		})
	}
}

func lxcEntry(t *testing.T, w *world) onlineEntry {
	t.Helper()
	rec, err := readOnline(filepath.Join(w.dir, "alpha", "lxc-online.json"))
	if err != nil {
		t.Fatal(err)
	}
	return rec.Handles[lxcA]
}

func TestTheLXCDisconnectedNotice(t *testing.T) {
	w := newWorld(t, "alpha")
	w.run(t)
	entry := lxcEntry(t, w)
	if !entry.Held || entry.Run != runA || entry.SinceMS != t0.UnixMilli() || entry.Posted {
		t.Fatalf("entry %+v", entry)
	}
	info, err := os.Stat(filepath.Join(w.dir, "alpha", "lxc-online.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("lxc-online.json mode %v, %v", info.Mode().Perm(), err)
	}
	if len(w.announcer.asked) != 0 {
		t.Fatal("a held seat was announced")
	}

	// The run ended; a new run B has already written its id into the lock file and failed to
	// launch. B is never announced (INT7), but the ended run A still is.
	w.clock.advance(time.Minute)
	w.guests.probes[lxcA] = GuestProbe{Record: lockRecord(runB, true)}
	w.run(t)
	if len(w.announcer.asked) != 1 || w.announcer.asked[0] != (asked{lxcA, runA, t0.UnixMilli()}) {
		t.Fatalf("asked %+v, want the ended run A with its since", w.announcer.asked)
	}
	if entry = lxcEntry(t, w); entry.Held || !entry.Posted {
		t.Fatalf("entry %+v", entry)
	}
	rec := w.record(t, "alpha")
	if *rec.Members[1].Online {
		t.Fatal("a free seat is online")
	}

	w.clock.advance(time.Minute)
	w.run(t)
	if len(w.announcer.asked) != 1 {
		t.Fatal("a posted notice was asked again")
	}
}

func TestAStoppedContainerIsAnnounced(t *testing.T) {
	w := newWorld(t, "alpha")
	w.run(t)
	w.guests.probes[lxcA] = GuestProbe{Stopped: true}
	w.clock.advance(time.Minute)
	w.run(t)
	if len(w.announcer.asked) != 1 || w.announcer.asked[0].run != runA {
		t.Fatalf("asked %+v", w.announcer.asked)
	}
}

func TestATransientFailureIsAskedAgain(t *testing.T) {
	w := newWorld(t, "alpha")
	w.run(t)
	w.guests.probes[lxcA] = GuestProbe{Record: lockRecord(runA, false)}
	w.announcer.err = errors.New("unreachable")
	w.clock.advance(time.Minute)
	w.run(t)
	rec := w.record(t, "alpha")
	if !hasProblem(rec, "the disconnected notice for "+lxcA+" is not posted yet") || lxcEntry(t, w).Posted {
		t.Fatalf("problems %v", whats(rec.Problems))
	}
	w.announcer.err = nil
	w.clock.advance(time.Minute)
	w.run(t)
	if len(w.announcer.asked) != 2 || w.announcer.asked[1].run != runA || !lxcEntry(t, w).Posted {
		t.Fatalf("asked %+v", w.announcer.asked)
	}
	if hasProblem(w.record(t, "alpha"), "the disconnected notice") {
		t.Fatal("the problem stayed after the notice")
	}
}

func TestALaunchFailureIsNeverAnnounced(t *testing.T) {
	w := newWorld(t, "alpha")
	w.run(t)
	w.guests.probes[lxcA] = GuestProbe{Record: lockRecord(runA, true)}
	w.clock.advance(time.Minute)
	w.run(t)
	if len(w.announcer.asked) != 0 {
		t.Fatalf("a launch failure was announced: %+v", w.announcer.asked)
	}
	rec := w.record(t, "alpha")
	if !slices.ContainsFunc(rec.Problems, func(p status.Problem) bool {
		return p.What == lxcA+" launch-failed" && p.Fix == "ccy-teams restart "+lxcA
	}) {
		t.Fatalf("problems %v", whats(rec.Problems))
	}
}

func TestAParkedMemberIsNotAnnounced(t *testing.T) {
	w := newWorld(t, "alpha")
	w.run(t)
	w.guests.probes[lxcA] = GuestProbe{}
	w.viewer.view.Members[1].State = "parked"
	w.clock.advance(time.Minute)
	w.run(t)
	if len(w.announcer.asked) != 0 {
		t.Fatal("a parked member was announced")
	}
}

func TestANewRunIsRecorded(t *testing.T) {
	w := newWorld(t, "alpha")
	w.run(t)
	w.guests.probes[lxcA] = GuestProbe{Held: true, Record: lockRecord(runB, false)}
	w.clock.advance(time.Minute)
	w.run(t)
	entry := lxcEntry(t, w)
	if entry.Run != runB || entry.SinceMS != t0.Add(time.Minute).UnixMilli() || entry.Posted {
		t.Fatalf("entry %+v", entry)
	}
}

func TestAHeldLockWithNoRunNamesNone(t *testing.T) {
	w := newWorld(t, "alpha")
	w.guests.probes[lxcA] = GuestProbe{Held: true, Record: []byte(`junk`)}
	w.run(t)
	w.guests.probes[lxcA] = GuestProbe{}
	w.clock.advance(time.Minute)
	w.run(t)
	if len(w.announcer.asked) != 0 {
		t.Fatal("a lock that named no run was announced")
	}
}

func TestAGuestProbeThatFailsIsAProblem(t *testing.T) {
	w := newWorld(t, "alpha")
	w.guests.err = errors.New("no pid")
	w.run(t)
	rec := w.record(t, "alpha")
	if *rec.Members[1].Online || !hasProblem(rec, "the seat of "+lxcA+" could not be probed") {
		t.Fatalf("problems %v", whats(rec.Problems))
	}
}

func TestABrokenOnlineRecordFailsTheTeam(t *testing.T) {
	w := newWorld(t, "alpha")
	if err := os.WriteFile(filepath.Join(w.dir, "alpha", "lxc-online.json"), []byte(`{"v":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := w.run(t); out[0].Result != ResultFailed {
		t.Fatalf("outcome %+v", out[0])
	}
}
