package hot

import (
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestMoveAddsToTheNewTeamThenRemovesFromTheOld(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "beta", Seat: "1"})
	if code := r.run(t, "move", session, "beta", "alpha", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	got := r.ErrOut.String()
	added := strings.Index(got, "✓ record: --teams 1@beta,3@alpha")
	removed := strings.Index(got, "✓ record: --teams 3@alpha")
	if added < 0 || removed < added {
		t.Fatalf("add, then remove:\n%s", got)
	}
	wantLines(t, got, "✓ goodbye: proj.1+local.podman left the team: removed by the owner",
		"Moved: "+session+" is in alpha, and no longer in beta.")
	if d := r.desired(t); !slices.Equal(d.Seats, []membership.Seat{{Team: "alpha", Seat: "3"}}) || len(d.Farewell) != 0 {
		t.Fatalf("desired %+v", d)
	}
	if code := r.run(t, "move", session, "beta", "alpha", "--yes"); code != term.ExitOK {
		t.Fatalf("rerun exit %d:\n%s", code, r.ErrOut.String())
	}
	onlySkips(t, r.ErrOut.String())
}

func TestMoveWhoseAddFailsLeavesTheOldTeam(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "beta", Seat: "1"})
	r.On("systemctl is-active agent-bus-hs@alpha.service", "inactive\n", 3)
	if code := r.run(t, "move", session, "beta", "alpha", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "Not moved: "+session+" is still in beta, and nothing else was changed.")
	if r.desired(t).Generation != 1 {
		t.Fatal("the old team is untouched")
	}
}

func TestMoveWhoseRemoveFailsSaysItIsInBoth(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "beta", Seat: "1"})
	r.OnErr("/usr/local/share/agent-bus/kit/pingbus seat farewell", "pingbus: homeserver: unreachable\n", 7)
	if code := r.run(t, "move", session, "beta", "alpha", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), session+" is in alpha, and the move out of beta is not finished: "+
		"run the same move again to finish it.")
	r.On("/usr/local/share/agent-bus/kit/pingbus seat farewell", "", 0)
	if code := r.run(t, "move", session, "beta", "alpha", "--yes"); code != term.ExitOK {
		t.Fatalf("the rerun finishes: exit %d:\n%s", code, r.ErrOut.String())
	}
	if d := r.desired(t); len(d.Farewell) != 0 {
		t.Fatalf("desired %+v", d)
	}
}

func TestMoveOfTheCoordinatorNeedsTheOptionBeforeAnythingChanges(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "beta", Seat: "1"})
	r.m.roles["beta\tproj.1+local.podman"] = coord
	if code := r.run(t, "move", session, "beta", "alpha", "--yes"); code != term.ExitUsage {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if r.desired(t).Generation != 1 {
		t.Fatal("not added to alpha first")
	}
	if code := r.run(t, "move", session, "beta", "alpha", "--coordinator=demote", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
}
