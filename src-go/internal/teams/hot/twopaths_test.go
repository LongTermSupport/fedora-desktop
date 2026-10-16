package hot

import (
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The two coordinator paths (the owner's feedback on Plan 00203; SPEC-hot-control.md H25): a
// team with no coordinator is offered one plainly; a team that has one is a takeover, which
// names the current coordinator, says whether it runs, defaults to no, and needs
// --demote-current where nothing can be asked.

const current = "other.5+local.podman"

// interactive makes the rig a terminal with scripted gum answers.
func (r *rig) interactive(t *testing.T, answers ...string) *teamtest.Console {
	t.Helper()
	c := teamtest.Interactive(t, r.World)
	r.x.Kit = flow.New(r.Host)
	c.Script.Gum = answers
	return c
}

// runningCoordinator makes seat 5 of the checkout alpha's coordinator, held by a live session.
func (r *rig) runningCoordinator() {
	r.m.exists["5@alpha"] = current
	r.m.heldBy["5@alpha"] = true
	r.m.roles["alpha\t"+current] = coord
}

func TestRoleWithNoCoordinatorIsOfferedPlainly(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	c := r.interactive(t, "YES")
	if code := r.run(t, "role", session, "alpha", coord); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if len(c.Script.GumLog) != 1 {
		t.Fatalf("one question, got %q", c.Script.GumLog)
	}
	want := "alpha has no coordinator. Make ccy-proj (seat 3, proj.3+local.podman) its coordinator?"
	if !strings.Contains(c.Script.GumLog[0], want) {
		t.Fatalf("question %q lacks %q", c.Script.GumLog[0], want)
	}
}

func TestRoleOverARunningCoordinatorAsksTheTakeoverOnceDefaultingToNo(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.runningCoordinator()
	c := r.interactive(t, "NO")
	if code := r.run(t, "role", session, "alpha", coord); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	want := "alpha's coordinator is other.5+local.podman (running). A team has one coordinator. " +
		"Remove other.5+local.podman as coordinator and make ccy-proj (seat 3, proj.3+local.podman) the coordinator instead?"
	if len(c.Script.GumLog) != 1 || !strings.Contains(c.Script.GumLog[0], want) || !strings.Contains(c.Script.GumLog[0], "--default=false") {
		t.Fatalf("questions %q, want one, defaulting to no, with %q", c.Script.GumLog, want)
	}
	if len(r.setRoles()) != 0 {
		t.Fatal("no is nothing changed")
	}
	c.Script.Gum = []string{"YES"}
	if code := r.run(t, "role", session, "alpha", coord); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if len(c.Script.GumLog) != 2 {
		t.Fatalf("the takeover is asked once, not twice: %q", c.Script.GumLog)
	}
	if got, want := r.setRoles(), []string{"alpha " + current + " --role=worker", "alpha proj.3+local.podman --role=coordinator"}; !slices.Equal(got, want) {
		t.Fatalf("set-role %q, want %q", got, want)
	}
}

func TestATakeoverWithoutTheFlagIsRefusedNamingTheCoordinatorAndTheFlag(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.runningCoordinator()
	want := "alpha's coordinator is other.5+local.podman (running). A team has one coordinator: to remove other.5+local.podman " +
		"as coordinator and make ccy-proj (seat 3, proj.3+local.podman) the coordinator instead, add --demote-current. Nothing was changed."
	if code := r.run(t, "role", session, "alpha", coord, "--yes"); code != term.ExitUsage || !strings.Contains(r.ErrOut.String(), want) {
		t.Fatalf("no terminal: exit %d:\n%s", code, r.ErrOut.String())
	}
	// --yes on a terminal does not stand for the takeover either.
	r.interactive(t)
	if code := r.run(t, "role", session, "alpha", coord, "--yes"); code != term.ExitUsage || !strings.Contains(r.ErrOut.String(), want) {
		t.Fatalf("terminal: exit %d:\n%s", code, r.ErrOut.String())
	}
	if len(r.setRoles()) != 0 {
		t.Fatal("nothing changed")
	}
}

func TestATakeoverOfACoordinatorWhoseSeatNobodyHoldsSaysItIsNotRunning(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.runningCoordinator()
	r.m.heldBy["5@alpha"] = false
	if code := r.run(t, "role", session, "alpha", coord, "--yes"); code != term.ExitUsage ||
		!strings.Contains(r.ErrOut.String(), "alpha's coordinator is other.5+local.podman (not running).") {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
}

func TestAddAsCoordinatorIsRefusedBeforeAnythingChanges(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "beta", Seat: "1"})
	r.runningCoordinator()
	if code := r.run(t, "add", session, "alpha/3", "--role=coordinator", "--yes"); code != term.ExitUsage {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if !strings.Contains(r.ErrOut.String(), "alpha's coordinator is other.5+local.podman (running). A team has one coordinator: "+
		"to remove other.5+local.podman as coordinator and make ccy-proj the coordinator instead, add --demote-current.") {
		t.Fatalf("refusal:\n%s", r.ErrOut.String())
	}
	if r.Fake.Called("podman exec abc123 pingbus reach") || r.desired(t).Generation != 1 {
		t.Fatal("the takeover is refused before the route is checked or anything written")
	}
}

func TestAddAsCoordinatorOnATerminalAsksOneQuestionNamingThePath(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		takeover   bool
	}{
		{"none", "Add ccy-proj to alpha as seat 3 (proj.3+local.podman). alpha has no coordinator. Make ccy-proj its coordinator?", false},
		{"takeover", "Add ccy-proj to alpha as seat 3 (proj.3+local.podman). alpha's coordinator is other.5+local.podman (running). " +
			"A team has one coordinator. Remove other.5+local.podman as coordinator and make ccy-proj the coordinator instead?", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, membership.Seat{Team: "beta", Seat: "1"})
			if tc.takeover {
				r.runningCoordinator()
			}
			c := r.interactive(t, "YES")
			if code := r.run(t, "add", session, "alpha/3", "--role=coordinator"); code != term.ExitOK {
				t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
			}
			if len(c.Script.GumLog) != 1 || !strings.Contains(c.Script.GumLog[0], tc.want) || !strings.Contains(c.Script.GumLog[0], "--default=false") {
				t.Fatalf("questions %q, want one with %q", c.Script.GumLog, tc.want)
			}
			if r.m.role("alpha", "proj.3+local.podman") != coord {
				t.Fatalf("set-role %q", r.setRoles())
			}
		})
	}
}

func TestRemoveOfTheCoordinatorSaysWhereTheRoleGoes(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.m.roles["alpha\tproj.3+local.podman"] = coord
	r.m.roles["alpha\trepo.5+local.podman"] = worker
	c := r.interactive(t, "NO")
	if code := r.run(t, "remove", session, "alpha", "--coordinator=hand-to=5"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	want := "Seat 3 is alpha's coordinator: repo.5+local.podman becomes alpha's coordinator instead."
	if len(c.Script.GumLog) != 1 || !strings.Contains(c.Script.GumLog[0], want) {
		t.Fatalf("questions %q, want %q", c.Script.GumLog, want)
	}
	c.Script.Gum = []string{"NO"}
	if code := r.run(t, "remove", session, "alpha", "--coordinator=demote"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if want := "Seat 3 is alpha's coordinator: it becomes a worker, and alpha has no coordinator."; !strings.Contains(c.Script.GumLog[1], want) {
		t.Fatalf("question %q, want %q", c.Script.GumLog[1], want)
	}
}
