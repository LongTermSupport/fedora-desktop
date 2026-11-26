package system

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seatchoice"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// scratch is a home with a checkout, and a Cmd over it whose status records are none.
type scratch struct {
	cmd      *Cmd
	home     string
	out, err *bytes.Buffer
}

func world(t *testing.T, declared ...string) scratch {
	t.Helper()
	home := t.TempDir()
	checkout := filepath.Join(home, "repo")
	if err := os.MkdirAll(checkout, 0o700); err != nil {
		t.Fatal(err)
	}
	w := scratch{home: home, out: &bytes.Buffer{}, err: &bytes.Buffer{}}
	env := map[string]string{"HOME": home}
	w.cmd = &Cmd{
		Getenv:   func(k string) string { return env[k] },
		Getwd:    func() (string, error) { return checkout, nil },
		Stdout:   w.out,
		Stderr:   w.err,
		Declared: func(context.Context) ([]string, error) { return declared, nil },
		Status:   func(paths.TeamID) (status.Record, bool, error) { return status.Record{}, false, nil },
		Life:     func(paths.TeamID) error { return nil },
	}
	return w
}

func placeSeat(t *testing.T, home, checkout, team string) {
	t.Helper()
	store, err := paths.SeatStore(filepath.Join(home, ".local", "state"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := paths.CheckoutKeyOf(checkout)
	if err != nil {
		t.Fatal(err)
	}
	id, err := paths.ParseTeamID(team)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(store.PoolTeam(key, id)+"/repo.1+site.podman", 0o700); err != nil {
		t.Fatal(err)
	}
}

// The chooser's side (choose-teams, Plan 00209 Task 4.3): whether a team is live on teamchat
// here, its seats here as the seat parts a launch names, and the teams with a seat here.
func TestCmdAnswersTheChooserFromThisCheckoutsPool(t *testing.T) {
	w := world(t, "old")
	placeSeat(t, w.home, filepath.Join(w.home, "repo"), "newer")
	placeSeat(t, w.home, filepath.Join(w.home, "repo"), "fresh")
	ctx := context.Background()
	for team, want := range map[string]bool{"fresh": true, "old": false} {
		if got, err := w.cmd.Live(ctx, team); err != nil || got != want {
			t.Fatalf("Live(%s): got %v %v, want %v", team, got, err, want)
		}
	}
	if got, err := w.cmd.SeatCount("fresh", seatchoice.KindPodman); err != nil || got != 1 {
		t.Fatalf("SeatCount: got %v %v, want 1", got, err)
	}
	if got, err := w.cmd.Handles("fresh", seatchoice.KindPodman); err != nil || strings.Join(got, ",") != "repo.1+site.podman" {
		t.Fatalf("Handles: got %v %v, want [repo.1+site.podman]", got, err)
	}
	if got, err := w.cmd.SeatCount("old", seatchoice.KindPodman); err != nil || got != 0 {
		t.Fatalf("SeatCount of a team with no seat here: got %v %v, want 0", got, err)
	}
	if _, err := w.cmd.SeatCount("not a team", seatchoice.KindPodman); err == nil {
		t.Fatal("SeatCount took a name outside the team grammar")
	}
	if got, err := w.cmd.PoolTeams(); err != nil || strings.Join(got, ",") != "fresh,newer" {
		t.Fatalf("PoolTeams: got %v %v, want [fresh newer]", got, err)
	}
}

// Register R55: a launch is answered with the seats of its own kind only; a host seat beside a
// podman one is counted, named and offered to cc alone.
func TestCmdAnswersEachKindWithItsOwnSeats(t *testing.T) {
	w := world(t)
	checkout := filepath.Join(w.home, "repo")
	placeSeat(t, w.home, checkout, "fresh")
	placeHandle(t, w.home, checkout, "fresh", "repo.2+site.host")
	w.cmd.Sessions = restorable{}
	for kind, want := range map[string]string{seatchoice.KindPodman: "repo.1+site.podman", seatchoice.KindHost: "repo.2+site.host"} {
		if got, err := w.cmd.SeatCount("fresh", kind); err != nil || got != 1 {
			t.Errorf("SeatCount(%s): got %v %v, want 1", kind, got, err)
		}
		if got, err := w.cmd.Handles("fresh", kind); err != nil || strings.Join(got, ",") != want {
			t.Errorf("Handles(%s): got %v %v, want %s", kind, got, err, want)
		}
		got, err := w.cmd.PoolState("fresh", kind)
		if err != nil || len(got) != 1 || got[0].Handle != want {
			t.Errorf("PoolState(%s): got %+v %v, want %s", kind, got, err, want)
		}
	}
	// AnyKind is every seat, for the refusal of a named seat of another kind.
	if got, err := w.cmd.Handles("fresh", seatchoice.AnyKind); err != nil || len(got) != 2 {
		t.Errorf("Handles(AnyKind): got %v %v, want both seats", got, err)
	}
}

// placeHandle makes the pool seat handle of team in checkout.
func placeHandle(t *testing.T, home, checkout, team, handle string) {
	t.Helper()
	store, err := paths.SeatStore(filepath.Join(home, ".local", "state"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := paths.CheckoutKeyOf(checkout)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(store.PoolTeam(key, mustTeam(t, team))+"/"+handle, 0o700); err != nil {
		t.Fatal(err)
	}
}

// restorable is a fake of the session records: a name in it is restorable.
type restorable map[string]bool

func (r restorable) Restorable(n paths.SessionName) (bool, error) { return r[n.String()], nil }

// unreadable is session records that cannot be read.
type unreadable struct{}

func (unreadable) Restorable(paths.SessionName) (bool, error) {
	return false, errors.New("the registry cannot be read")
}

// holdSeat writes a complete held/ on the pool seat, naming session, as a take leaves it.
func holdSeat(t *testing.T, home, checkout, team, handle, session string) {
	t.Helper()
	store, err := paths.SeatStore(filepath.Join(home, ".local", "state"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := paths.CheckoutKeyOf(checkout)
	if err != nil {
		t.Fatal(err)
	}
	id, err := paths.ParseTeamID(team)
	if err != nil {
		t.Fatal(err)
	}
	name, err := paths.ParseSeatName(handle)
	if err != nil {
		t.Fatal(err)
	}
	pool := store.PoolSeat(key, id, name)
	if err := os.MkdirAll(pool.Held(), 0o700); err != nil {
		t.Fatal(err)
	}
	rec := `{"v":1,"session":"` + session + `","claim":"0123456789abcdef","since_ms":1}`
	if err := os.WriteFile(pool.HeldRecord(), []byte(rec), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Issue 423: the chooser's seats carry the session a take would be refused for, so a held seat
// is never offered as one to take; a holding whose session is over is free.
func TestCmdPoolStateNamesTheSessionHoldingEachSeat(t *testing.T) {
	w := world(t)
	checkout := filepath.Join(w.home, "repo")
	placeSeat(t, w.home, checkout, "fresh")
	holdSeat(t, w.home, checkout, "fresh", "repo.1+site.podman", "ccy-repo")
	holdSeat(t, w.home, checkout, "fresh", "repo.2+site.podman", "ccy-repo-2")
	incomplete := filepath.Join(w.home, ".local", "state")
	store, err := paths.SeatStore(incomplete)
	if err != nil {
		t.Fatal(err)
	}
	key, err := paths.CheckoutKeyOf(checkout)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(store.PoolTeam(key, mustTeam(t, "fresh"))+"/repo.3+site.podman/held", 0o700); err != nil {
		t.Fatal(err)
	}
	w.cmd.Sessions = restorable{"ccy-repo": true}
	got, err := w.cmd.PoolState("fresh", seatchoice.KindPodman)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("three seats, got %+v", got)
	}
	if got[2].Name != "3" || got[2].HeldBy != "" {
		t.Errorf("an incomplete holding is no one's, so seat 3 is free: %+v", got)
	}
	byName := map[string]string{}
	for _, s := range got {
		byName[s.Name+" "+s.Handle] = s.HeldBy
	}
	if byName["1 repo.1+site.podman"] != "ccy-repo" {
		t.Errorf("seat 1 is held by ccy-repo: %+v", got)
	}
	if held, ok := byName["2 repo.2+site.podman"]; !ok || held != "" {
		t.Errorf("seat 2's session is over, so it is free: %+v", got)
	}
	if _, err := w.cmd.PoolState("not a team", seatchoice.KindPodman); err == nil {
		t.Error("PoolState took a name outside the team grammar")
	}
	w.cmd.Sessions = unreadable{}
	if _, err := w.cmd.PoolState("fresh", seatchoice.KindPodman); err == nil || !strings.Contains(err.Error(), "is seat repo.1+site.podman of fresh held") {
		t.Errorf("a record that cannot be read made a seat free: %v", err)
	}
	w.cmd.Sessions = nil
	if _, err := w.cmd.PoolState("fresh", seatchoice.KindPodman); err == nil {
		t.Error("PoolState with no session records answered, so a held seat could read as free")
	}
}

// Plan 00265 R90 (Q5, Q7): each pool seat carries its role in the team, as the team's status
// record names its member; a seat the record does not name, or no record, has none. SeatRoles
// is the same for a checkout named by path, keyed by seat.
func TestCmdPoolStateAndSeatRolesNameEachSeatsRole(t *testing.T) {
	w := world(t)
	checkout := filepath.Join(w.home, "repo")
	holdSeat(t, w.home, checkout, "brisk", "repo.1+site.podman", "ccy-repo")
	holdSeat(t, w.home, checkout, "brisk", "repo.2+site.podman", "ccy-repo-2")
	w.cmd.Sessions = restorable{}
	w.cmd.Status = func(paths.TeamID) (status.Record, bool, error) {
		return status.Record{Members: []status.Member{
			{Handle: "repo.1+site.podman", Role: "coordinator"},
			{Handle: "other.2+site.podman", Role: "worker"},
		}}, true, nil
	}
	got, err := w.cmd.PoolState("brisk", seatchoice.KindPodman)
	if err != nil {
		t.Fatal(err)
	}
	roles := map[string]string{}
	for _, s := range got {
		roles[s.Name] = s.Role
	}
	if roles["1"] != "coordinator" || roles["2"] != "" || len(roles) != 2 {
		t.Fatalf("roles %v: seat 1 is the coordinator, seat 2 is not in the record", roles)
	}
	byPath, err := w.cmd.SeatRoles(checkout, "brisk")
	if err != nil || len(byPath) != 1 || byPath["1"] != "coordinator" {
		t.Fatalf("SeatRoles = %v, %v", byPath, err)
	}
	w.cmd.Status = func(paths.TeamID) (status.Record, bool, error) {
		return status.Record{}, false, errors.New("unreadable")
	}
	if _, err := w.cmd.PoolState("brisk", seatchoice.KindPodman); err == nil || !strings.Contains(err.Error(), "unreadable") {
		t.Fatalf("an unreadable status record: %v", err)
	}
	w.cmd.Status = func(paths.TeamID) (status.Record, bool, error) { return status.Record{}, false, nil }
	if byPath, err := w.cmd.SeatRoles(checkout, "brisk"); err != nil || len(byPath) != 0 {
		t.Fatalf("no status record: %v, %v", byPath, err)
	}
	if _, err := w.cmd.SeatRoles(checkout, "Not A Team"); err == nil {
		t.Fatal("SeatRoles took a name outside the team grammar")
	}
}

func TestCmdLiveRefusesANameOnBothSystems(t *testing.T) {
	w := world(t, "fresh")
	placeSeat(t, w.home, filepath.Join(w.home, "repo"), "fresh")
	if _, err := w.cmd.Live(context.Background(), "fresh"); err == nil || !strings.Contains(err.Error(), "both a declared v1 team and a teamchat team") {
		t.Fatalf("got %v, want the refusal of a name on both systems", err)
	}
}

func TestCmdPrintsOneLinePerTeam(t *testing.T) {
	w := world(t, "old")
	placeSeat(t, w.home, filepath.Join(w.home, "repo"), "fresh")
	if code := w.cmd.Run(context.Background(), []string{"old"}); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if code := w.cmd.Run(context.Background(), []string{"fresh"}); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if got := w.out.String(); got != "old old agent-bus\nfresh fresh teamchat\n" {
		t.Fatalf("got %q", got)
	}
}

func TestCmdRefusesBothSystemsWith64AndNoOutput(t *testing.T) {
	w := world(t, "old")
	placeSeat(t, w.home, filepath.Join(w.home, "repo"), "fresh")
	if code := w.cmd.Run(context.Background(), []string{"old", "fresh"}); code != term.ExitUsage {
		t.Fatalf("exit %d", code)
	}
	if w.out.Len() != 0 || !strings.Contains(w.err.String(), "Run two sessions") {
		t.Fatalf("stdout %q stderr %q", w.out, w.err)
	}
}

func TestCmdNeedsATeamAndAHome(t *testing.T) {
	w := world(t)
	if code := w.cmd.Run(context.Background(), nil); code != term.ExitUsage {
		t.Fatalf("no team: exit %d", code)
	}
	w.cmd.Getenv = func(string) string { return "" }
	if code := w.cmd.Run(context.Background(), []string{"x"}); code != term.ExitFailure {
		t.Fatalf("no HOME: exit %d", code)
	}
}

func TestCmdUndecidedIs78(t *testing.T) {
	w := world(t)
	set, err := paths.ParseSetID("s1")
	if err != nil {
		t.Fatal(err)
	}
	w.cmd.Status = func(paths.TeamID) (status.Record, bool, error) {
		return status.Record{Origin: status.OriginMigration, Set: &set}, true, nil
	}
	if code := w.cmd.Run(context.Background(), []string{"mig"}); code != ExitUndecided {
		t.Fatalf("exit %d", code)
	}
}
