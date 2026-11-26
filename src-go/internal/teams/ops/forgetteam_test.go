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

func TestATeamStillThereAfterThePlayForgetsNothing(t *testing.T) {
	r := removeRig(t)
	r.onExit("gum confirm", 1, "")
	r.statuses = []map[string]declare.Status{{"alpha": st("current", "")}, {"alpha": st("outdated", "config")}}
	r.ops.ForgetTeam = func(string) error { t.Fatal("a team still there forgot its seats"); return nil }
	if code := r.ops.Remove(context.Background(), []string{"alpha"}); code != 1 {
		t.Errorf("code %d", code)
	}
}
