package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

const (
	invocationA = "0123456789abcdef0123456789abcdef"
	invocationB = "fedcba9876543210fedcba9876543210"
	// newMetaTime is the time Tuwunel writes into the meta file of the backup it makes.
	newMetaTime = 1792700000
)

var startTime = time.UnixMilli(1792710000123)

// fakeClock is a clock whose Sleep advances it and calls onSleep with the sleep's count.
type fakeClock struct {
	now     time.Time
	sleeps  int
	onSleep func(n int)
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	c.now = c.now.Add(d)
	c.sleeps++
	if c.onSleep != nil {
		c.onSleep(c.sleeps)
	}
	return ctx.Err()
}

// fakeUnits answers the states in turn, the last one again once they run out, and records each
// Kill, calling onKill.
type fakeUnits struct {
	states   []UnitState
	stateErr error
	reads    int
	kills    []string
	killErr  error
	onKill   func()
}

func (u *fakeUnits) State(context.Context, string) (UnitState, error) {
	if u.stateErr != nil {
		return UnitState{}, u.stateErr
	}
	s := u.states[min(u.reads, len(u.states)-1)]
	u.reads++
	return s, nil
}

func (u *fakeUnits) Kill(_ context.Context, unit, whom string, signal int32) error {
	u.kills = append(u.kills, unit+" "+whom+" "+strconv.Itoa(int(signal)))
	if u.killErr != nil {
		return u.killErr
	}
	if u.onKill != nil {
		u.onKill()
	}
	return nil
}

type fakeReady struct {
	err   error
	ports []int
}

func (r *fakeReady) ServerVersion(_ context.Context, port int) error {
	r.ports = append(r.ports, port)
	return r.err
}

func active(invocation string) UnitState {
	return UnitState{ActiveState: "active", InvocationID: invocation, Result: "success"}
}

// world is a host tree with one applied team whose homeserver has backups 2 and 3, and whose
// Kill makes backup 4.
type world struct {
	t     *testing.T
	root  string
	team  paths.TeamID
	units *fakeUnits
	ready *fakeReady
	clock *fakeClock
	lock  *teamlock.Lock
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{t: t, root: treeRoot(t), team: alpha(t), ready: &fakeReady{}, clock: &fakeClock{now: startTime}}
	w.units = &fakeUnits{states: []UnitState{active(invocationA)}}
	w.units.onKill = func() { w.meta(4, strconv.Itoa(newMetaTime)+"\n100\n") }
	src := sampleSources(t)
	w.file(paths.AdminState(w.team), map[string][]byte{"team.json": src.TeamJSON, "config.toml": src.Config})
	w.file(paths.CtlState(w.team), map[string][]byte{"roster.json": src.Roster, "rooms.json": src.Rooms, "admin.token": src.Token})
	w.meta(2, "1792600000\n")
	w.meta(3, "1792650000\n")
	lock, err := teamlock.Take(context.Background(), filepath.Join(t.TempDir(), "run"), w.team, "backup", 0, w.clock, teamlock.Unguarded)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lock.Release(); err != nil {
			t.Error(err)
		}
	})
	w.lock = lock
	return w
}

func (w *world) file(dir string, files map[string][]byte) {
	w.t.Helper()
	if err := os.MkdirAll(w.root+dir, 0o700); err != nil {
		w.t.Fatal(err)
	}
	for name, data := range files {
		if err := os.WriteFile(w.root+dir+"/"+name, data, 0o600); err != nil {
			w.t.Fatal(err)
		}
	}
}

func (w *world) meta(n int64, text string) {
	w.t.Helper()
	plantMeta(w.t, w.root, w.team, strconv.FormatInt(n, 10), text)
}

func (w *world) deps() Deps {
	return Deps{Root: w.root, Units: w.units, Ready: w.ready, Clock: w.clock, CtlUID: selfUID(w.t), StateOwnerUID: selfUID(w.t)}
}

func (w *world) tars() []string {
	w.t.Helper()
	entries, err := os.ReadDir(w.root + paths.BackupDir(w.team))
	if err != nil {
		w.t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func (w *world) lastRun() Run {
	w.t.Helper()
	run, found, err := LastRun(w.deps(), w.team)
	if err != nil || !found {
		w.t.Fatalf("last run %v %v", found, err)
	}
	return run
}

func (w *world) take(keep ...int64) (Result, error) {
	return Take(context.Background(), w.deps(), w.lock, keep)
}

func TestTakeWritesThePairAndPrunes(t *testing.T) {
	w := newWorld(t)
	w.file(paths.BackupDir(w.team), map[string][]byte{"1.tar": []byte("old"), "2.tar": sampleTar(t, 2), "5.tar": []byte("x"), "notes.txt": []byte("x"), "07.tar": []byte("x")})
	res, err := w.take()
	if err != nil {
		t.Fatal(err)
	}
	if res.Backup != 4 || res.TimeMS != newMetaTime*1000 || !slices.Equal(res.Pruned, []int64{1, 5}) {
		t.Fatalf("result %+v", res)
	}
	if got := strings.Join(w.tars(), " "); got != "07.tar 2.tar 4.tar last-run.json notes.txt" {
		t.Fatalf("backups %s", got)
	}
	w.checkPair(4)
	if w.units.kills[0] != paths.ServerUnit(w.team)+" main 12" || len(w.units.kills) != 1 {
		t.Fatalf("kills %v", w.units.kills)
	}
	if w.ready.ports[0] != alphaPort {
		t.Fatalf("probed %v", w.ready.ports)
	}
	if run := w.lastRun(); run != (Run{Result: "ok", AtMS: startTime.UnixMilli(), Backup: 4}) {
		t.Fatalf("last run %+v", run)
	}
}

// checkPair checks tar n decodes with the new meta's time, and the pair's modes.
func (w *world) checkPair(n int64) {
	w.t.Helper()
	raw, found, err := state.ReadFile(w.root+paths.BackupTar(w.team, n), maxArchive)
	if err != nil || !found {
		w.t.Fatal(err)
	}
	c, err := Decode(raw, w.team, n)
	if err != nil || c.Manifest.TimeMS != newMetaTime*1000 || c.Manifest.ServerName != "alpha.teamchat.internal" {
		w.t.Fatalf("tar %+v %v", c.Manifest, err)
	}
	for path, mode := range map[string]os.FileMode{paths.BackupTar(w.team, n): 0o600, paths.BackupLastRun(w.team): 0o600, paths.BackupDir(w.team): 0o700} {
		info, err := os.Lstat(w.root + path)
		if err != nil || info.Mode().Perm() != mode {
			w.t.Fatalf("%s: %v %v", path, info, err)
		}
	}
}

func TestTakeKeepsWhatItIsAskedToKeep(t *testing.T) {
	w := newWorld(t)
	w.file(paths.BackupDir(w.team), map[string][]byte{"1.tar": []byte("old"), "5.tar": []byte("x")})
	res, err := w.take(5)
	if err != nil || !slices.Equal(res.Pruned, []int64{1}) {
		t.Fatalf("%+v %v", res, err)
	}
}

// TestTakeNeverSignalsAServerNotReady is BT1's three no-Kill cases, and one more: a unit no
// longer active on the second read.
func TestTakeNeverSignalsAServerNotReady(t *testing.T) {
	cases := map[string]struct {
		states []UnitState
		ready  error
		want   error
	}{
		"not active":           {states: []UnitState{{ActiveState: "inactive"}}, want: ErrNotRunning},
		"activating":           {states: []UnitState{{ActiveState: "activating", InvocationID: invocationA}}, want: ErrNotRunning},
		"not answering":        {states: []UnitState{active(invocationA)}, ready: errors.New("connection refused"), want: ErrNotRunning},
		"another invocation":   {states: []UnitState{active(invocationA), active(invocationB)}, want: ErrLost},
		"stopped since":        {states: []UnitState{active(invocationA), {ActiveState: "deactivating", InvocationID: invocationA}}, want: ErrLost},
		"no invocation at all": {states: []UnitState{active("")}, want: ErrNotRunning},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.units.states, w.ready.err = c.states, c.ready
			_, err := w.take()
			if !errors.Is(err, c.want) || len(w.units.kills) != 0 {
				t.Fatalf("err %v, kills %v", err, w.units.kills)
			}
			if run := w.lastRun(); run.Result != "failed" || run.Backup != 0 || !strings.Contains(run.Error, "alpha") {
				t.Fatalf("last run %+v", run)
			}
		})
	}
}

func TestTakeWaitsForAWholeNewerMeta(t *testing.T) {
	w := newWorld(t)
	w.units.onKill = func() { w.meta(4, "17927") }
	w.clock.onSleep = func(n int) {
		switch n {
		case 2:
			w.meta(1, "1792500000\n")
		case 3:
			w.meta(4, "1792700000\n")
		case 4:
			w.meta(6, "1792700009\n")
		}
	}
	res, err := w.take()
	if err != nil || res.Backup != 4 || w.clock.sleeps != 3 {
		t.Fatalf("%+v %v after %d sleeps", res, err, w.clock.sleeps)
	}
}

func TestTakeTakesTheHighestNewNumber(t *testing.T) {
	w := newWorld(t)
	w.units.onKill = func() {
		w.meta(4, "1792700000\n")
		w.meta(5, "1792700005\n")
	}
	res, err := w.take()
	if err != nil || res.Backup != 5 || res.TimeMS != 1792700005000 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestTakeGivesUpAfterSixtySeconds(t *testing.T) {
	w := newWorld(t)
	w.units.onKill = nil
	_, err := w.take()
	var e *Error
	if !errors.As(err, &e) || e.Step != StepWait || !strings.Contains(err.Error(), "60 s") || errors.Is(err, ErrLost) {
		t.Fatalf("err %v", err)
	}
	if w.clock.sleeps != 60 || w.clock.now.Sub(startTime) != 60*time.Second {
		t.Fatalf("%d sleeps, %v", w.clock.sleeps, w.clock.now.Sub(startTime))
	}
	if run := w.lastRun(); run.Result != "failed" || run.AtMS != w.clock.now.UnixMilli() {
		t.Fatalf("last run %+v", run)
	}
}

// TestTakeNamesAServerLostDuringTheWait is RS15's 69 for BV1: the unit restarted or stopped
// while the wait ran.
func TestTakeNamesAServerLostDuringTheWait(t *testing.T) {
	w := newWorld(t)
	w.units.onKill = nil
	w.units.states = []UnitState{active(invocationA), active(invocationA), active(invocationB)}
	if _, err := w.take(); !errors.Is(err, ErrLost) || !strings.Contains(err.Error(), "60 s") {
		t.Fatalf("err %v", err)
	}
}

func TestTakeStopsWhenTheContextEnds(t *testing.T) {
	w := newWorld(t)
	w.units.onKill = nil
	ctx, cancel := context.WithCancel(context.Background())
	w.clock.onSleep = func(n int) {
		if n == 2 {
			cancel()
		}
	}
	if _, err := Take(ctx, w.deps(), w.lock, nil); !errors.Is(err, context.Canceled) || w.clock.sleeps != 2 {
		t.Fatalf("err %v after %d", err, w.clock.sleeps)
	}
}

func TestTakeRefusesBadSources(t *testing.T) {
	cases := map[string]func(w *world){
		"no roster": func(w *world) { remove(w.t, w.root+paths.RosterFile(w.team)) },
		"no token":  func(w *world) { remove(w.t, w.root+paths.CtlState(w.team)+"/admin.token") },
		"no config": func(w *world) { remove(w.t, w.root+paths.ServerConfig(w.team)) },
		"no team":   func(w *world) { remove(w.t, w.root+paths.TeamDeclaration(w.team)) },
		"a linked rooms file": func(w *world) {
			remove(w.t, w.root+paths.RoomsFile(w.team))
			if err := os.Symlink(w.root+paths.RosterFile(w.team), w.root+paths.RoomsFile(w.team)); err != nil {
				w.t.Fatal(err)
			}
		},
		"a group-writable roster": func(w *world) {
			if err := unix.Chmod(w.root+paths.RosterFile(w.team), 0o620); err != nil {
				w.t.Fatal(err)
			}
		},
		"a team file that does not decode": func(w *world) {
			w.file(paths.AdminState(w.team), map[string][]byte{"team.json": []byte(`{"v":1}`)})
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			change(w)
			_, err := w.take()
			var e *Error
			if !errors.As(err, &e) || e.Step != StepSources || e.Team != w.team || len(w.units.kills) != 0 {
				t.Fatalf("err %v, kills %v", err, w.units.kills)
			}
		})
	}
}

func TestTakeNamesTheMissingSource(t *testing.T) {
	w := newWorld(t)
	remove(t, w.root+paths.RosterFile(w.team))
	if _, err := w.take(); err == nil || !strings.Contains(err.Error(), "roster.json") {
		t.Fatalf("err %v", err)
	}
}

func remove(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

// TestTakeRemovesATarThatDoesNotValidate is BT3's read-back: a roster that does not decode is
// written, refused by BK3, and removed, so no half of a pair is left looking whole.
func TestTakeRemovesATarThatDoesNotValidate(t *testing.T) {
	w := newWorld(t)
	w.file(paths.CtlState(w.team), map[string][]byte{"roster.json": []byte(`{"v":1}`)})
	_, err := w.take()
	var e *Error
	if !errors.As(err, &e) || e.Step != StepVerify {
		t.Fatalf("err %v", err)
	}
	if slices.Contains(w.tars(), "4.tar") {
		t.Fatal("the tar that failed its read-back is still there")
	}
}

func TestTakeStepErrors(t *testing.T) {
	cases := map[string]struct {
		change func(w *world)
		step   string
	}{
		"the state read": {func(w *world) { w.units.stateErr = errors.New("no bus") }, StepReady},
		"the signal":     {func(w *world) { w.units.killErr = errors.New("refused") }, StepSignal},
		"the meta tree":  {func(w *world) { w.meta(4, "1792700000\n"+strings.Repeat("x", maxMeta)) }, StepSignal},
		"a linked backup directory": {func(w *world) {
			if err := os.Symlink(w.t.TempDir(), w.root+paths.BackupDir(w.team)); err != nil {
				w.t.Fatal(err)
			}
		}, StepWrite},
		"a meta tree broken in the wait": {func(w *world) {
			w.units.onKill = func() { w.meta(4, "1792700000\n"+strings.Repeat("x", maxMeta)) }
		}, StepWait},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			c.change(w)
			_, err := w.take()
			var e *Error
			if !errors.As(err, &e) || e.Step != c.step || !strings.Contains(err.Error(), "team alpha") {
				t.Fatalf("err %v", err)
			}
		})
	}
}

func TestTakeConvergesTheBackupDirectory(t *testing.T) {
	w := newWorld(t)
	if err := unix.Mkdir(w.root+paths.BackupDir(w.team), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := w.take(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(w.root + paths.BackupDir(w.team))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("%v %v", info.Mode(), err)
	}
}

func TestTakeJoinsALastRunItCouldNotWrite(t *testing.T) {
	w := newWorld(t)
	if err := os.MkdirAll(w.root+paths.BackupLastRun(w.team), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := w.take(); err == nil || !strings.Contains(err.Error(), "last-run.json") {
		t.Fatalf("err %v", err)
	}
}

func TestTakeNeedsTheHeldLock(t *testing.T) {
	w := newWorld(t)
	if _, err := Take(context.Background(), w.deps(), nil, nil); err == nil || len(w.units.kills) != 0 {
		t.Fatalf("err %v", err)
	}
}

func TestLastRunErrorIsOneLine(t *testing.T) {
	for _, in := range []string{"a\nb", strings.Repeat("é", 400), "tab\there", "nul\x00"} {
		line := oneLine(errors.New(in))
		if !status.OneLine(line) || strings.Contains(line, "\n") {
			t.Fatalf("%q gave %q", in, line)
		}
	}
}
