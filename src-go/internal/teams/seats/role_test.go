package seats

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

const (
	setRole     = teamtest.SudoBus + "set-role alpha "
	seatOne     = "proj.1+local.podman"
	seatTwo     = "proj.2+local.podman"
	coordinator = "--role=coordinator"
	worker      = "--role=worker"
)

// roster is the team's members: seat 1 and the lxc member, each a worker or the coordinator.
func roster(oneIs, twoIs string) string {
	return "HUMAN\towner\tjoin\n" +
		"MEMBER\t" + seatOne + "\t" + oneIs + "\tjoin\tactive\n" +
		"MEMBER\t" + seatTwo + "\t" + twoIs + "\tjoin\tactive\n"
}

// roleRig is a machine whose roster is set, with sudo ready.
func roleRig(t *testing.T, oneIs, twoIs string) rig {
	t.Helper()
	h := newRig(t)
	h.On(teamtest.SudoBus+"list alpha", roster(oneIs, twoIs), 0)
	h.On("sudo -n -v", "", 0)
	h.On(setRole, "", 0)
	return h
}

// setRoleCalls are the set-role commands run, in order, as "<handle> <flag>".
func setRoleCalls(h rig) []string {
	var calls []string
	for _, c := range h.Fake.Calls() {
		if line := c.Line(); strings.HasPrefix(line, setRole) {
			calls = append(calls, strings.TrimPrefix(line, setRole))
		}
	}
	return calls
}

func TestMakeCoordinatorWhenTheTeamHasNone(t *testing.T) {
	h := roleRig(t, "worker", "worker")
	h.Script.Fzf = []string{"|YES"}
	changed, err := h.s.ChangeRole(context.Background(), "alpha", seatOne, true)
	if err != nil || !changed {
		t.Fatalf("%v %v", changed, err)
	}
	if got, want := setRoleCalls(h), []string{seatOne + " " + coordinator}; !slices.Equal(got, want) {
		t.Fatalf("set-role %q, want %q", got, want)
	}
	if !strings.Contains(h.HeaderOf(0), seatOne+" will be copied on what workers tell humans") ||
		!strings.Contains(strings.Join(h.Script.FzfArgs[0], " "), "Make "+seatOne+" the coordinator of alpha?") {
		t.Fatalf("question %q args %q", h.HeaderOf(0), h.Script.FzfArgs[0])
	}
}

func TestMakeCoordinatorNamesTheHolderAndDemotesItFirst(t *testing.T) {
	h := roleRig(t, "worker", "coordinator")
	h.Script.Fzf = []string{"|YES"}
	changed, err := h.s.ChangeRole(context.Background(), "alpha", seatOne, true)
	if err != nil || !changed {
		t.Fatalf("%v %v", changed, err)
	}
	want := []string{seatTwo + " " + worker, seatOne + " " + coordinator}
	if got := setRoleCalls(h); !slices.Equal(got, want) {
		t.Fatalf("set-role %q, want %q", got, want)
	}
	if !strings.Contains(h.HeaderOf(0), seatTwo+" is the coordinator now and becomes a worker") {
		t.Fatalf("question %q", h.HeaderOf(0))
	}
}

func TestMakeCoordinatorFailingAfterTheDemotionPutsTheHolderBack(t *testing.T) {
	h := roleRig(t, "worker", "coordinator")
	h.OnErr(setRole+seatOne+" "+coordinator, "agent-bus: refused", 70)
	h.Script.Fzf = []string{"|YES"}
	changed, err := h.s.ChangeRole(context.Background(), "alpha", seatOne, true)
	if changed || err == nil {
		t.Fatalf("%v %v", changed, err)
	}
	want := []string{seatTwo + " " + worker, seatOne + " " + coordinator, seatTwo + " " + coordinator}
	if got := setRoleCalls(h); !slices.Equal(got, want) {
		t.Fatalf("set-role %q, want %q", got, want)
	}
}

func TestStopBeingCoordinator(t *testing.T) {
	h := roleRig(t, "coordinator", "worker")
	h.Script.Fzf = []string{"|YES"}
	changed, err := h.s.ChangeRole(context.Background(), "alpha", seatOne, false)
	if err != nil || !changed {
		t.Fatalf("%v %v", changed, err)
	}
	if got, want := setRoleCalls(h), []string{seatOne + " " + worker}; !slices.Equal(got, want) {
		t.Fatalf("set-role %q, want %q", got, want)
	}
	if !strings.Contains(h.HeaderOf(0), "alpha will have no coordinator") ||
		!strings.Contains(strings.Join(h.Script.FzfArgs[0], " "), "Stop "+seatOne+" being the coordinator of alpha?") {
		t.Fatalf("question %q args %q", h.HeaderOf(0), h.Script.FzfArgs[0])
	}
}

func TestAlreadyInTheWantedRoleAsksNothingAndRunsNoSudo(t *testing.T) {
	for _, tc := range []struct {
		name         string
		oneIs        string
		wantCoordnor bool
	}{{"coordinator stays", "coordinator", true}, {"worker stays", "worker", false}} {
		t.Run(tc.name, func(t *testing.T) {
			h := roleRig(t, tc.oneIs, "worker")
			changed, err := h.s.ChangeRole(context.Background(), "alpha", seatOne, tc.wantCoordnor)
			if err != nil || changed {
				t.Fatalf("%v %v", changed, err)
			}
			if len(h.Script.Rows) != 0 || h.Fake.Called("sudo -n -v") || len(setRoleCalls(h)) != 0 {
				t.Fatalf("asked or ran sudo: %v %v", h.Script.Rows, h.Fake.Calls())
			}
		})
	}
}

func TestDecliningChangesNothing(t *testing.T) {
	h := roleRig(t, "worker", "coordinator")
	h.Script.Fzf = []string{"|EXIT"}
	changed, err := h.s.ChangeRole(context.Background(), "alpha", seatOne, true)
	if err != nil || changed {
		t.Fatalf("%v %v", changed, err)
	}
	if h.Fake.Called("sudo -n -v") || len(setRoleCalls(h)) != 0 {
		t.Fatalf("sudo ran after a no: %v", h.Fake.Calls())
	}
}

func TestWithoutSudoNothingIsChangedAndItIsSaid(t *testing.T) {
	h := roleRig(t, "worker", "worker")
	h.OnErr("sudo -n -v", "sudo: a password is required", 1)
	h.OnErr("sudo -v", "sudo: no", 1)
	h.Script.Fzf = []string{"|YES"}
	changed, err := h.s.ChangeRole(context.Background(), "alpha", seatOne, true)
	if err == nil || changed || len(setRoleCalls(h)) != 0 {
		t.Fatalf("%v %v %v", changed, err, h.Fake.Calls())
	}
}

func TestAMemberThatIsNotInTheTeamIsAnError(t *testing.T) {
	h := roleRig(t, "worker", "worker")
	if _, err := h.s.ChangeRole(context.Background(), "alpha", "proj.9+local.podman", true); err == nil ||
		!strings.Contains(err.Error(), "proj.9+local.podman is not a member of alpha") {
		t.Fatalf("got %v", err)
	}
}

func TestAnUnreadableRosterIsAnError(t *testing.T) {
	h := roleRig(t, "worker", "worker")
	h.OnErr(teamtest.SudoBus+"list alpha", "sudo: a password is required", 1)
	if _, err := h.s.ChangeRole(context.Background(), "alpha", seatOne, true); err == nil {
		t.Fatal("changed a role without reading the team")
	}
	if len(setRoleCalls(h)) != 0 {
		t.Fatal("set a role without reading the team")
	}
}

func TestTheSeatMenuActionMakesTheCoordinatorAndGoesBackToTheList(t *testing.T) {
	h := roleRig(t, "worker", "worker")
	h.Script.Fzf = []string{"|YES"}
	snap := h.pass(t)
	res, err := h.s.ToggleCoordinator(context.Background(), snap, h.row(t, snap, "1"))
	if err != nil || res != flow.Done {
		t.Fatalf("%v %v", res, err)
	}
	if got, want := setRoleCalls(h), []string{seatOne + " " + coordinator}; !slices.Equal(got, want) {
		t.Fatalf("set-role %q, want %q", got, want)
	}
}

func TestTheSeatMenuActionOnTheCoordinatorStopsIt(t *testing.T) {
	h := roleRig(t, "coordinator", "worker")
	h.Script.Fzf = []string{"|YES"}
	snap := h.pass(t)
	if _, err := h.s.ToggleCoordinator(context.Background(), snap, h.row(t, snap, "1")); err != nil {
		t.Fatal(err)
	}
	if got, want := setRoleCalls(h), []string{seatOne + " " + worker}; !slices.Equal(got, want) {
		t.Fatalf("set-role %q, want %q", got, want)
	}
}

func TestTheSeatMenuActionSaysWhenNothingWasChanged(t *testing.T) {
	h := roleRig(t, "worker", "worker")
	h.Script.Fzf = []string{"|EXIT"}
	snap := h.pass(t)
	if _, err := h.s.ToggleCoordinator(context.Background(), snap, h.row(t, snap, "1")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.ErrOut.String(), "Nothing was changed.") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}
