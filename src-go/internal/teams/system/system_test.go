package system

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// fakeSources answers from maps: the status records, the teams with a pool seat in the session's
// checkout, and the declared v1 teams.
type fakeSources struct {
	records  map[string]status.Record
	pool     map[string]bool
	declared []string
	err      error
}

func (f fakeSources) sources() Sources {
	return Sources{
		Status: func(t paths.TeamID) (status.Record, bool, error) {
			rec, ok := f.records[t.String()]
			return rec, ok, f.err
		},
		PoolHolds: func(t paths.TeamID) (bool, error) { return f.pool[t.String()], nil },
		Declared:  func(context.Context) ([]string, error) { return f.declared, nil },
	}
}

func setID(t *testing.T) *paths.SetID {
	t.Helper()
	id, err := paths.ParseSetID("s1")
	if err != nil {
		t.Skipf("set id grammar: %v", err)
	}
	return &id
}

func TestAV2NativeStatusRecordIsTeamchat(t *testing.T) {
	src := fakeSources{records: map[string]status.Record{"alpha": {Origin: status.OriginV2}}}
	lines, err := Resolve(context.Background(), []string{"alpha"}, src.sources())
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0] != (Line{Given: "alpha", ID: "alpha", System: Teamchat}) {
		t.Fatalf("got %+v", lines)
	}
}

func TestAPoolSeatInTheSessionsCheckoutIsTeamchat(t *testing.T) {
	src := fakeSources{pool: map[string]bool{"beta": true}}
	lines, err := Resolve(context.Background(), []string{"beta"}, src.sources())
	if err != nil || lines[0].System != Teamchat {
		t.Fatalf("got %+v, %v", lines, err)
	}
}

func TestAnyOtherTeamIsAgentBus(t *testing.T) {
	src := fakeSources{declared: []string{"gamma"}}
	lines, err := Resolve(context.Background(), []string{"gamma", "Not_A_V2_Id"}, src.sources())
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range lines {
		if l.System != AgentBus || l.ID != l.Given {
			t.Fatalf("got %+v", lines)
		}
	}
}

func TestAMigratedTeamIsUndecidedUntilSetRecordsAreRead(t *testing.T) {
	for _, rec := range []status.Record{{Origin: status.OriginMigration, Set: setID(t)}, {Origin: status.OriginV2, Set: setID(t)}} {
		src := fakeSources{records: map[string]status.Record{"delta": rec}}
		_, err := Resolve(context.Background(), []string{"delta"}, src.sources())
		if !errors.Is(err, ErrUndecided) {
			t.Fatalf("%+v: got %v, want ErrUndecided", rec, err)
		}
	}
}

func TestANameOnBothSystemsIsRefusedNamingBoth(t *testing.T) {
	src := fakeSources{pool: map[string]bool{"eps": true}, declared: []string{"eps"}}
	_, err := Resolve(context.Background(), []string{"eps"}, src.sources())
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "eps") ||
		!strings.Contains(err.Error(), "declared v1 team") || !strings.Contains(err.Error(), "pool seat") {
		t.Fatalf("got %v", err)
	}
}

func TestTeamsOnBothSystemsAreRefusedWithTheTwoChoices(t *testing.T) {
	src := fakeSources{pool: map[string]bool{"v2team": true}, declared: []string{"v1team"}}
	_, err := Resolve(context.Background(), []string{"v1team", "v2team"}, src.sources())
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("got %v", err)
	}
	for _, want := range []string{"agent-bus: v1team", "teamchat: v2team", "two sessions", "migrate"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("%q lacks %q", err, want)
		}
	}
}

func TestAStatusReadFailureFails(t *testing.T) {
	src := fakeSources{err: errors.New("permission denied")}
	if _, err := Resolve(context.Background(), []string{"alpha"}, src.sources()); err == nil || errors.Is(err, ErrRefused) {
		t.Fatalf("got %v", err)
	}
}

func TestNoTeamIsAUsageError(t *testing.T) {
	if _, err := Resolve(context.Background(), nil, fakeSources{}.sources()); !errors.Is(err, ErrRefused) {
		t.Fatalf("got %v", err)
	}
}

func TestOneSystemIsTheSharedSystem(t *testing.T) {
	if s, err := One([]Line{{System: Teamchat}, {System: Teamchat}}); err != nil || s != Teamchat {
		t.Fatalf("got %q %v", s, err)
	}
	if s, err := One(nil); err != nil || s != AgentBus {
		t.Fatalf("no team: got %q %v, want agent-bus", s, err)
	}
}

// The pool is read under the session's own checkout key only (L3): a pool seat of the same team
// in another checkout does not make the team v2-live here.
func TestPoolHoldsReadsOnlyTheGivenCheckoutsPool(t *testing.T) {
	state := t.TempDir()
	store, err := paths.SeatStore(state)
	if err != nil {
		t.Fatal(err)
	}
	here, err := paths.CheckoutKeyOf("/work/here")
	if err != nil {
		t.Fatal(err)
	}
	there, err := paths.CheckoutKeyOf("/work/there")
	if err != nil {
		t.Fatal(err)
	}
	team, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	seatDir := store.PoolTeam(there, team) + "/repo.1+site.podman"
	if err := os.MkdirAll(seatDir, 0o700); err != nil {
		t.Fatal(err)
	}
	holds := PoolHolds(store, here)
	if got, err := holds(team); err != nil || got {
		t.Fatalf("another checkout's seat: got %v %v, want false", got, err)
	}
	if err := os.MkdirAll(filepath.Join(store.PoolTeam(here, team), "repo.1+site.podman.new-0123456789abcdef"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got, err := holds(team); err != nil || got {
		t.Fatalf("a placement's claim only: got %v %v, want false", got, err)
	}
	if err := os.MkdirAll(store.PoolTeam(here, team)+"/repo.1+site.podman", 0o700); err != nil {
		t.Fatal(err)
	}
	if got, err := holds(team); err != nil || !got {
		t.Fatalf("this checkout's seat: got %v %v, want true", got, err)
	}
}
