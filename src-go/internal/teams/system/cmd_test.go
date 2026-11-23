package system

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
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
	if got, err := w.cmd.SeatParts("fresh"); err != nil || strings.Join(got, ",") != "1" {
		t.Fatalf("SeatParts: got %v %v, want [1]", got, err)
	}
	if got, err := w.cmd.Handles("fresh"); err != nil || strings.Join(got, ",") != "repo.1+site.podman" {
		t.Fatalf("Handles: got %v %v, want [repo.1+site.podman]", got, err)
	}
	if got, err := w.cmd.SeatParts("old"); err != nil || got != nil {
		t.Fatalf("SeatParts of a team with no seat here: got %v %v, want none", got, err)
	}
	if _, err := w.cmd.SeatParts("not a team"); err == nil {
		t.Fatal("SeatParts took a name outside the team grammar")
	}
	if got, err := w.cmd.PoolTeams(); err != nil || strings.Join(got, ",") != "fresh,newer" {
		t.Fatalf("PoolTeams: got %v %v, want [fresh newer]", got, err)
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
