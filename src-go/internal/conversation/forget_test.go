package conversation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
)

const forgetID = "0f8b7c1e-3d2a-4b5c-9e6f-1a2b3c4d5e6f"

func recordIn(t *testing.T, checkout, list string) {
	t.Helper()
	if err := RecordSeats(checkout, list, forgetID, SourceLaunch, recordMS); err != nil {
		t.Fatal(err)
	}
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return err == nil
}

// ForgetTeam removes every record of the team's seats in every checkout it is given, and nothing
// of another team's (Plan 00265 R36, issue 417).
func TestForgetTeamRemovesOnlyThatTeamsRecordsInEachCheckout(t *testing.T) {
	one, two := t.TempDir(), t.TempDir()
	recordIn(t, one, "1@alpha,1@beta")
	recordIn(t, one, "2@alpha")
	recordIn(t, two, "3@alpha")
	if err := ForgetTeam([]string{one, two}, "alpha"); err != nil {
		t.Fatal(err)
	}
	for _, s := range []membership.Seat{{Seat: "1", Team: "alpha"}, {Seat: "2", Team: "alpha"}} {
		if exists(t, SeatRecordPath(one, s)) {
			t.Errorf("%s@%s is still recorded in the first checkout", s.Seat, s.Team)
		}
	}
	if exists(t, filepath.Dir(SeatRecordPath(two, membership.Seat{Seat: "3", Team: "alpha"}))) {
		t.Error("alpha's records are still in the second checkout")
	}
	if !exists(t, SeatRecordPath(one, membership.Seat{Seat: "1", Team: "beta"})) {
		t.Error("beta's record went with alpha's")
	}
}

func TestForgetTeamInACheckoutWithNoRecordsIsNothing(t *testing.T) {
	if err := ForgetTeam([]string{t.TempDir(), filepath.Join(t.TempDir(), "gone")}, "alpha"); err != nil {
		t.Fatal(err)
	}
}

func TestForgetTeamRefusesWhatIsNotATeamName(t *testing.T) {
	checkout := t.TempDir()
	recordIn(t, checkout, "1@alpha")
	for _, team := range []string{"", "..", "a/b", "alpha@x"} {
		err := ForgetTeam([]string{checkout}, team)
		if err == nil || !strings.Contains(err.Error(), "is not a team name") {
			t.Errorf("team %q: %v", team, err)
		}
	}
	if !exists(t, SeatRecordPath(checkout, membership.Seat{Seat: "1", Team: "alpha"})) {
		t.Error("a refused name removed a record")
	}
}

// A link higher up (a container may write .claude/ccy as its own) must not lead the removal out
// of the checkout: seat-conversations pointing at another tree that has a directory named for the
// team (independent review of R36, finding 2).
func TestForgetTeamRefusesALinkAboveTheTeamsDirectory(t *testing.T) {
	for _, above := range []string{".claude", ".claude/ccy", seatRecordDir} {
		t.Run(above, func(t *testing.T) {
			checkout, elsewhere := t.TempDir(), t.TempDir()
			victim := filepath.Join(elsewhere, strings.TrimPrefix(seatRecordDir, above), "alpha", "keep")
			if err := os.MkdirAll(filepath.Dir(victim), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(victim, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(checkout, above)
			if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(elsewhere, link); err != nil {
				t.Fatal(err)
			}
			err := ForgetTeam([]string{checkout}, "alpha")
			if err == nil || !strings.Contains(err.Error(), link) {
				t.Errorf("err %v", err)
			}
			if !exists(t, victim) {
				t.Errorf("%s, outside the checkout, was removed", victim)
			}
		})
	}
}

func TestForgetTeamRefusesALinkInPlaceOfTheTeamsDirectory(t *testing.T) {
	checkout, elsewhere := t.TempDir(), t.TempDir()
	recordIn(t, checkout, "1@beta")
	keep := filepath.Join(elsewhere, "keep")
	if err := os.WriteFile(keep, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(checkout, seatRecordDir, "alpha")
	if err := os.Symlink(elsewhere, link); err != nil {
		t.Fatal(err)
	}
	err := ForgetTeam([]string{checkout}, "alpha")
	if err == nil || !strings.Contains(err.Error(), link) {
		t.Fatalf("err %v", err)
	}
	if !exists(t, keep) {
		t.Error("the link was followed")
	}
}
