package hot

import (
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/roles"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Plan 00203 Task 3.8: `session role` takes a member's handle in place of a session, so the role
// of a member of any kind (an LXC container's agent, a host, docker or vm member, or a session
// named by its handle) is changed from the command line by the same path as from the picker.
const lxcMember = "box.1+worker.lxc"

func TestRoleOfAMemberNamedByItsHandleIsChangedWhateverItsKind(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.m.roles["alpha\t"+lxcMember] = worker
	r.WriteHostVars(t, teamtest.HostVarsAlpha) // no agent_bus_lxc_members entry
	if code := r.run(t, "role", lxcMember, "alpha", coord, "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if got := r.setRoles(); !slices.Equal(got, []string{"alpha " + lxcMember + " --role=coordinator"}) {
		t.Fatalf("set-role %q", got)
	}
	wantLines(t, r.ErrOut.String(), "✓ role: "+lxcMember+" set to coordinator",
		lxcMember+" is alpha's coordinator now. "+roles.Reach)
	if code := r.run(t, "role", lxcMember, "alpha", coord, "--yes"); code != term.ExitOK {
		t.Fatalf("rerun exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "Nothing to do: "+lxcMember+" is alpha's coordinator.")
	if len(r.setRoles()) != 1 {
		t.Fatal("a rerun changes no role")
	}
}

func TestRoleOfAMemberByHandleIsTheSameTakeoverWithTheSameFlag(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.m.roles["alpha\t"+lxcMember] = worker
	r.m.roles["alpha\tproj.3+local.podman"] = coord
	r.WriteHostVars(t, teamtest.HostVarsAlpha) // no agent_bus_lxc_members entry
	if code := r.run(t, "role", lxcMember, "alpha", coord, "--yes"); code != term.ExitUsage {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if !strings.Contains(r.ErrOut.String(), "add --demote-current") || len(r.setRoles()) != 0 {
		t.Fatalf("a takeover with --yes alone changed something:\n%s", r.ErrOut.String())
	}
	if code := r.run(t, "role", lxcMember, "alpha", coord, "--demote-current", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	want := []string{"alpha proj.3+local.podman --role=worker", "alpha " + lxcMember + " --role=coordinator"}
	if got := r.setRoles(); !slices.Equal(got, want) {
		t.Fatalf("set-role %q, want %q", got, want)
	}
}

func TestRoleOfAHandleThatIsNotAMemberChangesNothing(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	if code := r.run(t, "role", lxcMember, "alpha", worker, "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✗ role: "+lxcMember+" is not a member of alpha")
}
