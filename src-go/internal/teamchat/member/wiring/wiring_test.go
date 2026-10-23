package wiring

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/engine"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

var errFake = errors.New("fake: not here")

// fakeEngine answers every seam with an error: the tests here never get as far as a store.
type fakeEngine struct{}

func (fakeEngine) Load(paths.TeamID, string) (state.Store, error) { return state.Store{}, errFake }
func (fakeEngine) View(paths.TeamID, []string, []string, proto.Roster) (team.View, error) {
	return team.View{}, errFake
}
func (fakeEngine) Persist(context.Context, team.Batch) (int64, error)         { return 0, errFake }
func (fakeEngine) ResetPosition(context.Context, paths.TeamID, int64) error   { return errFake }
func (fakeEngine) SetEnded(context.Context, paths.TeamID, state.RunEnd) error { return errFake }
func (fakeEngine) BeginRun(context.Context, paths.TeamID) error               { return errFake }
func (fakeEngine) SetHealth(paths.TeamID, team.Health)                        {}
func (fakeEngine) SaveRoster(context.Context, paths.TeamID, int64, json.RawMessage) error {
	return errFake
}
func (fakeEngine) RecoverSends(context.Context, paths.TeamID, team.Notices) error { return errFake }

type fixture struct {
	cfg  engine.TeamConfig
	seat paths.Seat
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := homefs.Open(home, homefs.Private)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cerr := root.Close(); cerr != nil {
			t.Error(cerr)
		}
	})
	layout, err := paths.HomeLayout(home, home+"/rt")
	if err != nil {
		t.Fatal(err)
	}
	id, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	run, err := paths.ParseRunID("0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFake(time.Hour, 1_700_000_000_000)
	e := fakeEngine{}
	ends := NewEnds(engine.EndsConfig{Engine: e, Run: run, Clock: clk, Log: io.Discard})
	return fixture{
		cfg: engine.TeamConfig{
			Team: id, Seat: layout.Seat(id), Run: run, Engine: e, Ends: ends, Root: root,
			Clock: clk, Log: io.Discard,
		},
		seat: layout.Seat(id),
	}
}

// n3c-D5: the binary passes exactly the verbs cli does not build itself.
func TestVerbsAreTheVerbsBuiltOutsideCLI(t *testing.T) {
	got := slices.Sorted(maps.Keys(Verbs()))
	want := slices.Sorted(slices.Values(cli.Outside()))
	if !slices.Equal(got, want) {
		t.Fatalf("Verbs() = %q, want %q", got, want)
	}
	for name, run := range Verbs() {
		if run == nil {
			t.Errorf("%s is nil", name)
		}
	}
}

func TestNewTeamRefusesAnIncompleteConfig(t *testing.T) {
	f := newFixture(t)
	f.cfg.Engine = nil
	if _, err := NewTeam(f.cfg); err == nil {
		t.Fatal("NewTeam made a team with no engine")
	}
}

// A team whose seat directory cannot be watched does not run (LN9): its Run fails, and the
// engine shows the stop.
func TestATeamWhoseSeatCannotBeWatchedDoesNotRun(t *testing.T) {
	f := newFixture(t)
	tm, err := NewTeam(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := tm.Run(t.Context()); err == nil {
		t.Fatal("a team with no seat directory ran")
	}
}

// The seat directory is watched while the team runs (LN9), and the watch is closed when Run
// returns.
func TestTheSeatIsWatchedWhileTheTeamRuns(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(f.seat.Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	tm, err := NewTeam(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	w, ok := tm.(*watched)
	if !ok {
		t.Fatalf("NewTeam made a %T", tm)
	}
	if w.changes.Changes() != nil {
		t.Fatal("a watch before Run")
	}
	ctx, cancel := context.WithCancel(t.Context())
	ran := make(chan error, 1)
	go func() { ran <- tm.Run(ctx) }()
	var ch <-chan struct{}
	for ch == nil {
		select {
		case err := <-ran:
			t.Fatalf("Run ended: %v", err)
		default:
		}
		runtime.Gosched()
		ch = w.changes.Changes()
	}
	cancel()
	if err := <-ran; err != nil {
		t.Fatalf("Run: %v", err)
	}
	if w.changes.Changes() != nil {
		t.Fatal("the watch outlived Run")
	}
}

// n3d W: the team goroutine's config is the engine's TeamConfig, the link's forge checker
// among it, so a delivered ping is checked with the same login and health as a send.
func TestTheTeamsConfigCarriesTheEnginesTeamConfig(t *testing.T) {
	f := newFixture(t)
	f.cfg.Forge = stubForge{}
	f.cfg.Plain = true
	f.cfg.RemovedUserIDs = []string{"@a:example.org"}
	ch := &seatChanges{}
	got := teamsyncConfig(f.cfg, ch)
	if got.Forge != f.cfg.Forge || got.Watcher != ch || got.Team != f.cfg.Team || got.Seat != f.cfg.Seat ||
		got.Run != f.cfg.Run || got.Engine != f.cfg.Engine || got.Ends != f.cfg.Ends || got.Root != f.cfg.Root ||
		got.Clock != f.cfg.Clock || got.Log != f.cfg.Log || !got.Plain ||
		!slices.Equal(got.RemovedUserIDs, f.cfg.RemovedUserIDs) || got.FirstSessionStart != f.cfg.FirstSessionStart {
		t.Fatalf("teamsyncConfig = %+v, from %+v", got, f.cfg)
	}
}

type stubForge struct{}

func (stubForge) Check(context.Context, proto.ForgeCheck, proto.Forge) (team.ForgeResult, error) {
	return team.ForgeResult{Outcome: team.ForgeProven}, nil
}
