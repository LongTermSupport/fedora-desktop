package seats

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/coordinator"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/roles"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

const (
	setRole       = teamtest.SudoBus + "set-role alpha "
	seatOne       = "proj.1+local.podman"
	seatTwo       = "proj.2+local.podman"
	toCoordinator = "--role=coordinator"
	worker        = "--role=worker"
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
	changed, err := h.s.ChangeRole(context.Background(), "alpha", seatOne, true, nil)
	if err != nil || !changed {
		t.Fatalf("%v %v", changed, err)
	}
	if got, want := setRoleCalls(h), []string{seatOne + " " + toCoordinator}; !slices.Equal(got, want) {
		t.Fatalf("set-role %q, want %q", got, want)
	}
	if !strings.Contains(h.HeaderOf(0), "alpha has no coordinator. Make "+seatOne+" its coordinator?") ||
		!strings.Contains(h.HeaderOf(0), seatOne+" will be copied on what workers tell humans") ||
		!strings.Contains(strings.Join(h.Script.FzfArgs[0], " "), "Make "+seatOne+" the coordinator of alpha?") {
		t.Fatalf("question %q args %q", h.HeaderOf(0), h.Script.FzfArgs[0])
	}
}

func TestMakeCoordinatorNamesTheHolderAndDemotesItFirst(t *testing.T) {
	h := roleRig(t, "worker", "coordinator")
	h.Script.Fzf = []string{"|YES"}
	changed, err := h.s.ChangeRole(context.Background(), "alpha", seatOne, true, nil)
	if err != nil || !changed {
		t.Fatalf("%v %v", changed, err)
	}
	want := []string{seatTwo + " " + worker, seatOne + " " + toCoordinator}
	if got := setRoleCalls(h); !slices.Equal(got, want) {
		t.Fatalf("set-role %q, want %q", got, want)
	}
	takeover := "alpha's coordinator is " + seatTwo + " (whether it is running is not known here). A team has one coordinator. " +
		"Remove " + seatTwo + " as coordinator and make " + seatOne + " the coordinator instead?"
	args := strings.Join(h.Script.FzfArgs[0], " ")
	if !strings.Contains(h.HeaderOf(0), takeover) || !strings.Contains(args, "Replace "+seatTwo+" as coordinator of alpha?") ||
		!strings.Contains(args, "--bind=start:last") {
		t.Fatalf("question %q args %q", h.HeaderOf(0), args)
	}
}

func TestTheTakeoverSaysWhetherTheCoordinatorRuns(t *testing.T) {
	h := roleRig(t, "worker", "coordinator")
	h.Script.Fzf = []string{"|EXIT"}
	running := func(agentbus.Member) coordinator.Running { return coordinator.Active }
	if _, err := h.s.ChangeRole(context.Background(), "alpha", seatOne, true, running); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.HeaderOf(0), "alpha's coordinator is "+seatTwo+" (running).") {
		t.Fatalf("question %q", h.HeaderOf(0))
	}
}

func TestSnapshotRunningReadsTheBoardsSeatRows(t *testing.T) {
	snap := &board.Snapshot{Rows: []board.Row{
		{Kind: board.KindCcy, Handle: seatOne, Held: true},
		{Kind: board.KindCcy, Handle: seatTwo},
	}}
	look := SnapshotRunning(snap)
	for _, tc := range []struct {
		m    agentbus.Member
		want coordinator.Running
	}{
		{agentbus.Member{Handle: seatOne}, coordinator.Active},
		{agentbus.Member{Handle: seatTwo}, coordinator.Stopped},
		{agentbus.Member{Handle: seatTwo, Parked: true}, coordinator.Parked},
		{agentbus.Member{Handle: "box.1+local.lxc"}, coordinator.Unknown},
	} {
		if got := look(tc.m); got != tc.want {
			t.Errorf("%+v: got %v, want %v", tc.m, got, tc.want)
		}
	}
}

func TestMakeCoordinatorFailingAfterTheDemotionPutsTheHolderBack(t *testing.T) {
	h := roleRig(t, "worker", "coordinator")
	h.OnErr(setRole+seatOne+" "+toCoordinator, "agent-bus: refused", 70)
	h.Script.Fzf = []string{"|YES"}
	changed, err := h.s.ChangeRole(context.Background(), "alpha", seatOne, true, nil)
	if changed || err == nil {
		t.Fatalf("%v %v", changed, err)
	}
	want := []string{seatTwo + " " + worker, seatOne + " " + toCoordinator, seatTwo + " " + toCoordinator}
	if got := setRoleCalls(h); !slices.Equal(got, want) {
		t.Fatalf("set-role %q, want %q", got, want)
	}
}

func TestStopBeingCoordinator(t *testing.T) {
	h := roleRig(t, "coordinator", "worker")
	h.Script.Fzf = []string{"|YES"}
	changed, err := h.s.ChangeRole(context.Background(), "alpha", seatOne, false, nil)
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
			changed, err := h.s.ChangeRole(context.Background(), "alpha", seatOne, tc.wantCoordnor, nil)
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
	changed, err := h.s.ChangeRole(context.Background(), "alpha", seatOne, true, nil)
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
	changed, err := h.s.ChangeRole(context.Background(), "alpha", seatOne, true, nil)
	if err == nil || changed || len(setRoleCalls(h)) != 0 {
		t.Fatalf("%v %v %v", changed, err, h.Fake.Calls())
	}
}

func TestAMemberThatIsNotInTheTeamIsAnError(t *testing.T) {
	h := roleRig(t, "worker", "worker")
	if _, err := h.s.ChangeRole(context.Background(), "alpha", "proj.9+local.podman", true, nil); err == nil ||
		!strings.Contains(err.Error(), "proj.9+local.podman is not a member of alpha") {
		t.Fatalf("got %v", err)
	}
}

func TestAnUnreadableRosterIsAnError(t *testing.T) {
	h := roleRig(t, "worker", "worker")
	h.OnErr(teamtest.SudoBus+"list alpha", "sudo: a password is required", 1)
	if _, err := h.s.ChangeRole(context.Background(), "alpha", seatOne, true, nil); err == nil {
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
	if got, want := setRoleCalls(h), []string{seatOne + " " + toCoordinator}; !slices.Equal(got, want) {
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

// lxcRoster is roster with an LXC container's agent, the third member.
func lxcRoster(oneIs, lxcIs string) string {
	return roster(oneIs, "worker") + "MEMBER\t" + lxcMember + "\t" + lxcIs + "\tjoin\tactive\n"
}

const lxcMember = "proj.1+worker.lxc"

// probes is the root reader of an LXC container's checkout, which roles reads to find an entry.
type probes struct{ reads int }

func (p *probes) ReadCheckout(context.Context, string, string, string, string, string) (string, error) {
	p.reads++
	return "{}", nil
}

// declaredRig is roleRig with the LXC member declared by an entry, which the play keeps.
func declaredRig(t *testing.T, oneIs, lxcIs string) (rig, *[]string) {
	t.Helper()
	h := roleRig(t, oneIs, "worker")
	h.On(teamtest.SudoBus+"list alpha", lxcRoster(oneIs, lxcIs), 0)
	h.OnDeclare("lxc-members", "LXCMEMBER\talpha\tcontainer-a\tagent\t-\t"+lxcIs+"\tpresent\t/srv/agent/one\t-\n")
	h.OnDeclare("lxc-script", "READER")
	h.OnDeclare("lxc-identity", "IDENTITY\tproj\tworker\n")
	h.OnDeclare("lxc-role", "CHANGED\tLXC member\n")
	h.s.Roles.Reader = &probes{}
	applied := &[]string{}
	h.s.Roles.Apply = func(_ context.Context, teams ...string) error {
		*applied = append(*applied, teams...)
		return nil
	}
	return h, applied
}

// lxcRoleCalls are the declare lxc-role runs, as "<container> <role>".
func lxcRoleCalls(h rig) []string {
	var calls []string
	for _, c := range h.Fake.Calls() {
		if strings.HasPrefix(c.Line(), h.DeclarePrefix("lxc-role")) {
			calls = append(calls, c.Args[len(c.Args)-5]+" "+c.Args[len(c.Args)-1])
		}
	}
	return calls
}

func TestADeclaredLxcMemberIsMadeTheCoordinatorInItsEntryAndLiveThenApplied(t *testing.T) {
	h, applied := declaredRig(t, "worker", "worker")
	h.Script.Fzf = []string{"|YES"}
	changed, err := h.s.ChangeRole(context.Background(), "alpha", lxcMember, true, nil)
	if err != nil || !changed {
		t.Fatalf("%v %v", changed, err)
	}
	if got := lxcRoleCalls(h); !slices.Equal(got, []string{"container-a coordinator"}) {
		t.Fatalf("lxc-role %q", got)
	}
	if got := setRoleCalls(h); !slices.Equal(got, []string{lxcMember + " " + toCoordinator}) {
		t.Fatalf("set-role %q", got)
	}
	if !slices.Equal(*applied, []string{"alpha"}) {
		t.Fatalf("applied %v", *applied)
	}
}

func TestATakeoverFromADeclaredLxcCoordinatorMakesItsEntryAWorkerToo(t *testing.T) {
	h, applied := declaredRig(t, "worker", "coordinator")
	h.Script.Fzf = []string{"|YES"}
	changed, err := h.s.ChangeRole(context.Background(), "alpha", seatOne, true, nil)
	if err != nil || !changed {
		t.Fatalf("%v %v", changed, err)
	}
	if got := lxcRoleCalls(h); !slices.Equal(got, []string{"container-a worker"}) {
		t.Fatalf("lxc-role %q", got)
	}
	if got, want := setRoleCalls(h), []string{lxcMember + " " + worker, seatOne + " " + toCoordinator}; !slices.Equal(got, want) {
		t.Fatalf("set-role %q, want %q", got, want)
	}
	if !slices.Equal(*applied, []string{"alpha"}) {
		t.Fatalf("applied %v", *applied)
	}
}

func TestTheRoleActionOnAnLxcMemberRowChangesItsRoleAndSaysHowItsSessionLearnsIt(t *testing.T) {
	h, applied := declaredRig(t, "worker", "worker")
	h.Script.Fzf = []string{"|YES"}
	snap := h.pass(t)
	res, err := h.s.ToggleCoordinator(context.Background(), snap, h.row(t, snap, lxcMember))
	if err != nil || res != flow.Done || len(*applied) != 1 {
		t.Fatalf("%v %v %v", res, err, *applied)
	}
	if got := setRoleCalls(h); !slices.Equal(got, []string{lxcMember + " " + toCoordinator}) {
		t.Fatalf("set-role %q", got)
	}
	if said := h.ErrOut.String(); !strings.Contains(said, lxcMember+" is now the coordinator of alpha.") ||
		!strings.Contains(said, roles.Reach) {
		t.Fatalf("stderr %q", said)
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
