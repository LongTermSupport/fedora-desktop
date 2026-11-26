package teams

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/conversation"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/discover"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seatcheckouts"
)

// The checkouts a team taken down forgets its seats in are those that hold seats (the list of
// seat checkouts, which outlives a session) and those a running session names, each once.
func TestTheCheckoutsHoldingSeatsAreTheListAndTheRunningSessions(t *testing.T) {
	store := registry.New(filepath.Join(t.TempDir(), "ccy", "sessions"))
	listed, running := t.TempDir(), t.TempDir()
	if err := seatcheckouts.Of(store).Add(listed); err != nil {
		t.Fatal(err)
	}
	for _, rec := range []registry.Record{
		{Name: "ccy-a", Dir: running, Launcher: "/x/ccy", Prefix: "ccy", Args: []string{"--teams", "1@alpha"}},
		{Name: "ccy-b", Dir: listed, Launcher: "/x/ccy", Prefix: "ccy", Args: []string{"--teams", "2@alpha"}},
	} {
		if err := store.Write(rec); err != nil {
			t.Fatal(err)
		}
	}
	got, err := checkoutsHoldingSeats(store)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{listed, running}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestForgetTeamRemovesTheRecordsInACheckoutWhoseSessionHasEnded(t *testing.T) {
	store := registry.New(filepath.Join(t.TempDir(), "ccy", "sessions"))
	ended := t.TempDir()
	if err := seatcheckouts.Of(store).Add(ended); err != nil {
		t.Fatal(err)
	}
	const id = "0f8b7c1e-3d2a-4b5c-9e6f-1a2b3c4d5e6f"
	if err := conversation.RecordSeats(ended, "1@alpha", id, conversation.SourceExit, 1); err != nil {
		t.Fatal(err)
	}
	record := conversation.SeatRecordPath(ended, membership.Seat{Seat: "1", Team: "alpha"})
	if err := forgetTeam(func() (*registry.Store, error) { return store, nil }, "alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(record); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s: %v", record, err)
	}
}

// The real wiring hands ccy-teams remove the forgetting, over the host's own registry (independent
// review of R36, finding 4: Ops skips the step when it is unset).
func TestWireGivesRemoveTheForgettingOfTheTeamsSeatRecords(t *testing.T) {
	h := newHarness(t)
	if h.app.Ops.ForgetTeam == nil {
		t.Fatal("Wire leaves Ops.ForgetTeam unset, so remove would forget nothing")
	}
	store, err := h.Host.Registry()
	if err != nil {
		t.Fatal(err)
	}
	checkout := t.TempDir()
	if err := seatcheckouts.Of(store).Add(checkout); err != nil {
		t.Fatal(err)
	}
	const id = "0f8b7c1e-3d2a-4b5c-9e6f-1a2b3c4d5e6f"
	if err := conversation.RecordSeats(checkout, "1@alpha", id, conversation.SourceExit, 1); err != nil {
		t.Fatal(err)
	}
	if err := h.app.Ops.ForgetTeam("alpha"); err != nil {
		t.Fatal(err)
	}
	record := conversation.SeatRecordPath(checkout, membership.Seat{Seat: "1", Team: "alpha"})
	if _, err := os.Lstat(record); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s: %v", record, err)
	}
}

func TestForgetTeamWithNoRegistryIsAnError(t *testing.T) {
	err := forgetTeam(func() (*registry.Store, error) { return nil, errors.New("no registry") }, "alpha")
	if err == nil || err.Error() != "no registry" {
		t.Errorf("err %v", err)
	}
}

func TestAnUnreadableListOfSeatCheckoutsIsAnError(t *testing.T) {
	store := registry.New(filepath.Join(t.TempDir(), "ccy", "sessions"))
	path := seatcheckouts.Of(store).Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("relative/path\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := checkoutsHoldingSeats(store); err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("err %v", err)
	}
}

// writeRecords writes each record into store, failing the test on an error.
func writeRecords(t *testing.T, store *registry.Store, recs ...registry.Record) {
	t.Helper()
	for _, rec := range recs {
		if err := store.Write(rec); err != nil {
			t.Fatal(err)
		}
	}
}

// R36 review finding 3: remove takes an agent-bus team's running sessions off it, and a stopped
// session's record still names the team, so it was chosen to be taken off as well. Only a record
// on the agent-bus system is: a teamchat team of the same name is another team.
func TestTheStoppedSessionsOfATeamAreTheRecordsOnTheAgentBusThatNameIt(t *testing.T) {
	store := registry.New(filepath.Join(t.TempDir(), "ccy", "sessions"))
	dir := t.TempDir()
	writeRecords(t, store,
		registry.Record{Name: "ccy-a", Dir: dir, Launcher: "/x/ccy", Prefix: "ccy", Args: []string{"--teams", "1@alpha"}},
		registry.Record{Name: "ccy-b", Dir: dir, Launcher: "/x/ccy", Prefix: "ccy", Args: []string{"--teams", "2@alpha"}},
		registry.Record{Name: "ccy-c", Dir: dir, Launcher: "/x/ccy", Prefix: "ccy", Args: []string{"--teams", "1@beta"}},
		registry.Record{Name: "ccy-d", Dir: dir, Launcher: "/x/ccy", Prefix: "ccy"},
	)
	onBus := func(rec registry.Record) (bool, error) { return rec.Name != "ccy-b", nil }
	got, err := stoppedOn(store, "alpha", onBus)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"ccy-a"}; !slices.Equal(got, want) {
		t.Errorf("stopped sessions of alpha %q, want %q (ccy-b is on teamchat)", got, want)
	}
	if _, err := stoppedOn(store, "alpha", func(registry.Record) (bool, error) { return false, errors.New("no status") }); err == nil ||
		!strings.Contains(err.Error(), "ccy-a") {
		t.Errorf("a record whose system cannot be told: %v", err)
	}
}

// Taking a stopped session off the team leaves its other teams, drops --teams when the team was
// its only one, and drops its keeper's report (never authority; the keeper writes it again when
// the session starts), so neither a restore nor a seat lookup finds the old team in it.
func TestTakingStoppedSessionsOffATeamKeepsTheirOtherTeams(t *testing.T) {
	store := registry.New(filepath.Join(t.TempDir(), "ccy", "sessions"))
	dir := t.TempDir()
	writeRecords(t, store,
		registry.Record{Name: "ccy-a", Dir: dir, Launcher: "/x/ccy", Prefix: "ccy", Args: []string{"--teams", "1@alpha,3@beta"}},
		registry.Record{Name: "ccy-b", Dir: dir, Launcher: "/x/ccy", Prefix: "ccy", Args: []string{"--teams", "2@alpha"}},
		registry.Record{Name: "cc-c", Dir: dir, Launcher: "/x/cc", Prefix: "cc", Args: []string{"--teams", "4@alpha"}},
	)
	for _, name := range []string{"ccy-a", "ccy-b"} {
		report := membership.ReportPath(dir, name)
		if err := os.MkdirAll(filepath.Dir(report), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(report, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := takeStoppedOff(store, "alpha", []string{"ccy-a", "ccy-b", "cc-c"}); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"ccy-a": "3@beta", "ccy-b": "", "cc-c": ""} {
		rec, err := store.Get(name)
		if err != nil {
			t.Fatal(err)
		}
		if got := discover.RecordTeams(rec); got != want {
			t.Errorf("%s --teams %q, want %q", name, got, want)
		}
		if _, err := os.Lstat(membership.ReportPath(dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s's keeper report: %v, want it gone", name, err)
		}
	}
}

// R36 (3), the coordinator's test (batch 22 review): a team made again under the name of one taken
// down resumes nothing of the old one. Before the stopped session is taken off, a plain launch's
// seat lookup (ForSeats) finds its conversation; after, it finds no session that held the seat.
func TestATeamMadeAgainResumesNothingOfAStoppedSessionTakenOff(t *testing.T) {
	store := registry.New(filepath.Join(t.TempDir(), "ccy", "sessions"))
	dir := t.TempDir()
	writeRecords(t, store, registry.Record{Name: "ccy-a", Dir: dir, Launcher: "/x/ccy", Prefix: "ccy", Args: []string{"--teams", "1@alpha"}})
	plantV1Seat(t, dir, "ccy-a", "1", "alpha", seatConvID)
	if got, err := conversation.ForSeats(dir, "1@alpha", conversation.Here(noHome)); err != nil || got.ID != seatConvID {
		t.Fatalf("before: the seat lookup found %+v, %v, want the old conversation", got, err)
	}
	if err := takeStoppedOff(store, "alpha", []string{"ccy-a"}); err != nil {
		t.Fatal(err)
	}
	got, err := conversation.ForSeats(dir, "1@alpha", conversation.Here(noHome))
	if !errors.Is(err, conversation.ErrNoSeatConversation) {
		t.Fatalf("after: the seat lookup found %+v, %v, want no session that held the seat", got, err)
	}
}

// A keeper's report is removed inside its checkout only (R36 batch review, finding 1): a container
// can replace a directory above the report with a link, and a removal that followed it would delete
// a file of the host's outside the checkout.
func TestTakingAStoppedSessionOffNeverRemovesAReportThroughALink(t *testing.T) {
	store := registry.New(filepath.Join(t.TempDir(), "ccy", "sessions"))
	dir, outside := t.TempDir(), t.TempDir()
	writeRecords(t, store,
		registry.Record{Name: "ccy-a", Dir: dir, Launcher: "/x/ccy", Prefix: "ccy", Args: []string{"--teams", "1@alpha"}},
	)
	victim := filepath.Join(outside, "ccy-a", "applied.json")
	if err := os.MkdirAll(filepath.Dir(victim), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(victim, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sessions := filepath.Dir(membership.SessionDir(dir, "ccy-a"))
	if err := os.MkdirAll(filepath.Dir(sessions), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, sessions); err != nil {
		t.Fatal(err)
	}
	err := takeStoppedOff(store, "alpha", []string{"ccy-a"})
	if err == nil || !strings.Contains(err.Error(), "ccy-a") {
		t.Errorf("a report reached through a link: err %v, want a refusal naming ccy-a", err)
	}
	if _, err := os.Lstat(victim); err != nil {
		t.Errorf("the file outside the checkout: %v, want it kept", err)
	}
}

// One stopped session that cannot be taken off does not leave the others on the team (R36 batch
// review, finding 2): the team is already down when they are taken off, and nothing later takes
// them off, so each is tried and every failure is named.
func TestOneStoppedSessionThatCannotBeTakenOffLeavesTheOthersTakenOff(t *testing.T) {
	store := registry.New(filepath.Join(t.TempDir(), "ccy", "sessions"))
	linked, plain, outside := t.TempDir(), t.TempDir(), t.TempDir()
	writeRecords(t, store,
		registry.Record{Name: "ccy-a", Dir: linked, Launcher: "/x/ccy", Prefix: "ccy", Args: []string{"--teams", "1@alpha"}},
		registry.Record{Name: "ccy-b", Dir: plain, Launcher: "/x/ccy", Prefix: "ccy", Args: []string{"--teams", "2@alpha"}},
	)
	sessions := filepath.Dir(membership.SessionDir(linked, "ccy-a"))
	if err := os.MkdirAll(filepath.Dir(sessions), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, sessions); err != nil {
		t.Fatal(err)
	}
	err := takeStoppedOff(store, "alpha", []string{"ccy-a", "ccy-b"})
	if err == nil || !strings.Contains(err.Error(), "ccy-a") {
		t.Errorf("err %v, want a failure naming ccy-a", err)
	}
	for name, want := range map[string]string{"ccy-a": "1@alpha", "ccy-b": ""} {
		rec, getErr := store.Get(name)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if got := discover.RecordTeams(rec); got != want {
			t.Errorf("%s --teams %q, want %q", name, got, want)
		}
	}
}

// The real wiring hands remove the stopped sessions of the team and their taking off, over the
// host's own registry, a record's system read as a restore reads it.
func TestWireGivesRemoveTheStoppedSessionsAndTheirTakingOff(t *testing.T) {
	h := newHarness(t)
	if h.app.Ops.StoppedIn == nil || h.app.Ops.TakeStoppedOff == nil {
		t.Fatal("Wire leaves Ops.StoppedIn or Ops.TakeStoppedOff unset, so remove would leave stopped sessions in the team")
	}
	store, err := h.Host.Registry()
	if err != nil {
		t.Fatal(err)
	}
	writeRecords(t, store, registry.Record{Name: "ccy-z", Dir: t.TempDir(), Launcher: "/x/ccy", Prefix: "ccy", Args: []string{"--teams", "1@alpha"}})
	names, err := h.app.Ops.StoppedIn("alpha")
	if err != nil || !slices.Equal(names, []string{"ccy-z"}) {
		t.Fatalf("StoppedIn(alpha) = %q, %v", names, err)
	}
	if err := h.app.Ops.TakeStoppedOff("alpha", names); err != nil {
		t.Fatal(err)
	}
	rec, err := store.Get("ccy-z")
	if err != nil {
		t.Fatal(err)
	}
	if got := discover.RecordTeams(rec); got != "" {
		t.Errorf("ccy-z --teams %q after it was taken off alpha", got)
	}
}

func TestAnUnreadableSessionRecordIsAnError(t *testing.T) {
	store := registry.New(filepath.Join(t.TempDir(), "ccy", "sessions"))
	if err := os.MkdirAll(store.Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(store.Dir(), "ccy-bad")
	if err := os.WriteFile(bad, []byte("not a record\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := checkoutsHoldingSeats(store); err == nil || !strings.Contains(err.Error(), bad) {
		t.Errorf("err %v", err)
	}
}
