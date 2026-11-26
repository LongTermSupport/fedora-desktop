package ops

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
)

// A team taken down forgets its seats' conversation records, so a team made again under its name
// resumes nothing of the old one (Plan 00265 R36, issue 417).
func TestRemoveForgetsTheTeamsSeatConversationsOnceItIsDown(t *testing.T) {
	r := removeRig(t)
	var forgot []string
	r.ops.ForgetTeam = func(team string) error { forgot = append(forgot, team); return nil }
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 0 {
		t.Fatalf("code %d: %s", code, r.said())
	}
	if !slices.Equal(forgot, []string{"alpha"}) {
		t.Errorf("forgot %q", forgot)
	}
	if !strings.Contains(r.said(), "Its seats' conversation records are removed, so a team made again as alpha resumes nothing of this one.") {
		t.Errorf("said %q", r.said())
	}
}

func TestRemoveThatCannotForgetTheSeatConversationsFailsNamingWhy(t *testing.T) {
	r := removeRig(t)
	r.ops.ForgetTeam = func(string) error {
		return errors.New("could not remove /c/.claude/ccy/seat-conversations/alpha: busy")
	}
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 1 {
		t.Fatalf("code %d: %s", code, r.said())
	}
	want := "Team alpha is down, but its seats' conversation records could not all be removed: could not remove /c/.claude/ccy/seat-conversations/alpha: busy"
	if !strings.Contains(r.said(), want) {
		t.Errorf("said %q", r.said())
	}
}

// R36 review finding 3: a stopped session still in the team is taken off it once the team is
// down, before its seats' records are forgotten; which sessions are asked while the team is up.
func TestRemoveTakesTheTeamsStoppedSessionsOffItOnceItIsDown(t *testing.T) {
	r := removeRig(t)
	var steps []string
	r.ops.StoppedIn = func(team string) ([]string, error) {
		steps = append(steps, "asked "+team)
		return []string{"ccy-x", "cc-y"}, nil
	}
	r.ops.TakeStoppedOff = func(team string, names []string) error {
		steps = append(steps, "off "+team+" "+strings.Join(names, ","))
		return nil
	}
	r.ops.ForgetTeam = func(team string) error { steps = append(steps, "forgot "+team); return nil }
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 0 {
		t.Fatalf("code %d: %s", code, r.said())
	}
	if want := []string{"asked alpha", "off alpha ccy-x,cc-y", "forgot alpha"}; !slices.Equal(steps, want) {
		t.Errorf("steps %q, want %q", steps, want)
	}
	if !strings.Contains(r.said(), "The stopped sessions ccy-x, cc-y are taken off alpha, so a restore does not rejoin a team made again as alpha.") {
		t.Errorf("said %q", r.said())
	}
}

// R36 batch 22 review: the stopped sessions are found before the backup and fate questions and the
// play, so one may be restored meanwhile. A session running again when they are taken off is left
// as it is (its record is its launch's, its keeper report live), named, and the remove fails.
func TestRemoveLeavesAStoppedSessionThatStartedMeanwhile(t *testing.T) {
	r := removeRig(t)
	asked := 0
	r.ops.SessionsIn = func(context.Context, string) ([]string, error) {
		asked++
		if asked == 1 {
			return nil, nil
		}
		return []string{"ccy-x"}, nil
	}
	r.ops.StoppedIn = func(string) ([]string, error) { return []string{"ccy-x", "cc-y"}, nil }
	var off []string
	r.ops.TakeStoppedOff = func(_ string, names []string) error { off = names; return nil }
	forgot := false
	r.ops.ForgetTeam = func(string) error { forgot = true; return nil }
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 1 {
		t.Fatalf("code %d: %s", code, r.said())
	}
	if !slices.Equal(off, []string{"cc-y"}) {
		t.Errorf("taken off %q, want only the session still stopped", off)
	}
	if !forgot {
		t.Error("the seat records were not forgotten")
	}
	if want := "ccy-x started while alpha was taken down, so it was left as it is: take it off with ccy-teams session remove ccy-x alpha."; !strings.Contains(r.said(), want) {
		t.Errorf("said %q, want %q", r.said(), want)
	}
}

func TestRemoveThatCannotTellTheStoppedSessionsTakesNothingDown(t *testing.T) {
	r := removeRig(t)
	r.ops.StoppedIn = func(string) ([]string, error) { return nil, errors.New("session record ccy-x: unreadable") }
	r.ops.TakeStoppedOff = func(string, []string) error { t.Fatal("took sessions off a team not taken down"); return nil }
	r.ops.ForgetTeam = func(string) error { t.Fatal("forgot the seats of a team not taken down"); return nil }
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 1 {
		t.Fatalf("code %d: %s", code, r.said())
	}
	if !strings.Contains(r.said(), "the stopped sessions in alpha could not be found (session record ccy-x: unreadable), so it was not taken down.") {
		t.Errorf("said %q", r.said())
	}
}

// A stopped session that cannot be taken off fails the remove, naming it, but the team's seat
// records are still forgotten (R36 batch review, finding 2): the team is already down, and a
// second remove is refused as a team not built here, so nothing would ever forget them.
func TestRemoveThatCannotTakeAStoppedSessionOffStillForgetsAndFailsNamingWhy(t *testing.T) {
	r := removeRig(t)
	r.ops.StoppedIn = func(string) ([]string, error) { return []string{"ccy-x"}, nil }
	r.ops.TakeStoppedOff = func(string, []string) error { return errors.New("could not take session ccy-x off alpha: read-only") }
	forgot := false
	r.ops.ForgetTeam = func(string) error { forgot = true; return nil }
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 1 {
		t.Fatalf("code %d: %s", code, r.said())
	}
	if !forgot {
		t.Error("the seat records were not forgotten, and no later remove can forget them")
	}
	for _, want := range []string{
		"Team alpha is down, but its stopped sessions could not all be taken off it: could not take session ccy-x off alpha: read-only",
		"Its seats' conversation records are removed",
		"still lists alpha",
	} {
		if !strings.Contains(r.said(), want) {
			t.Errorf("said %q, want %q", r.said(), want)
		}
	}
}

func TestATeamStillThereAfterThePlayForgetsNothing(t *testing.T) {
	r := removeRig(t)
	r.onExit("gum confirm", 1, "")
	r.statuses = []map[string]declare.Status{{"alpha": st("current", "")}, {"alpha": st("outdated", "config")}}
	r.ops.ForgetTeam = func(string) error { t.Fatal("a team still there forgot its seats"); return nil }
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 1 {
		t.Errorf("code %d", code)
	}
}
