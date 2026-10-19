package hot

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRemoveReleasesReBaselinesAndSaysGoodbye(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	if code := r.run(t, "remove", session, "alpha", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(),
		"✓ session "+session+": running, can be hot-controlled",
		"✓ role: seat 3 is a worker",
		"✓ record: no --teams",
		"✓ membership: generation 2 written (farewell alpha/3)",
		"✓ session: seat 3 released (0.0 s)",
		"✓ re-baseline: sync position cleared, listening status expired",
		"✓ goodbye: proj.3+local.podman left the team: removed by the owner",
		"✓ membership: generation 3 written (farewell done)",
		"Removed. "+session+" is no longer in alpha; seat 3 is free. Its token stays valid: remove stops this session's pingbus, it does not revoke.")
	root := filepath.Join(r.Checkout(), ".claude/ccy/pingbus/seats")
	if !r.Fake.Called("/usr/local/share/agent-bus/kit/pingbus seat retire --root "+root+" 3@alpha") ||
		!r.Fake.Called("/usr/local/share/agent-bus/kit/pingbus seat farewell --root "+root+" 3@alpha") {
		t.Fatalf("calls %v", r.Fake.Calls())
	}
	if d := r.desired(t); len(d.Seats) != 0 || len(d.Farewell) != 0 {
		t.Fatalf("desired %+v", d)
	}
	if code := r.run(t, "remove", session, "alpha", "--yes"); code != term.ExitOK ||
		!strings.Contains(r.ErrOut.String(), "Nothing to do: "+session+" is not in alpha.") {
		t.Fatalf("rerun: exit %d:\n%s", code, r.ErrOut.String())
	}
}

func TestRemoveWhoseGoodbyeFailsKeepsItOwedAndTheRerunFinishes(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.OnErr("/usr/local/share/agent-bus/kit/pingbus seat farewell", "pingbus: homeserver: unreachable\n", 7)
	r.On("systemctl is-active agent-bus-hs@alpha.service", "failed\n", 3)
	if code := r.run(t, "remove", session, "alpha", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "- role: not checked (the homeserver is down)",
		"✗ goodbye: pingbus: homeserver: unreachable",
		"Removed, but goodbye and status not sent: homeserver down. Run the same command again once it is up.")
	if d := r.desired(t); len(d.Farewell) != 1 || len(d.Seats) != 0 {
		t.Fatalf("the farewell stays owed: %+v", d)
	}
	r.On("/usr/local/share/agent-bus/kit/pingbus seat farewell", "", 0)
	r.On("systemctl is-active agent-bus-hs@alpha.service", "active\n", 0)
	if code := r.run(t, "remove", session, "alpha", "--yes"); code != term.ExitOK {
		t.Fatalf("rerun exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "- session: seat 3 already released", "✓ goodbye: proj.3+local.podman left the team: removed by the owner")
	if d := r.desired(t); len(d.Farewell) != 0 {
		t.Fatalf("done: %+v", d)
	}
}

func TestRemoveOfTheCoordinatorNeedsToBeToldWhatBecomesOfTheRole(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.m.roles["alpha\tproj.3+local.podman"] = coord
	if code := r.run(t, "remove", session, "alpha", "--yes"); code != term.ExitUsage {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if !strings.Contains(r.ErrOut.String(), "--coordinator=demote") || r.desired(t).Generation != 1 {
		t.Fatalf("nothing written, the options named:\n%s", r.ErrOut.String())
	}
	if code := r.run(t, "remove", session, "alpha", "--coordinator=demote", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✓ role: proj.3+local.podman set to worker")
	if got := r.setRoles(); !slices.Equal(got, []string{"alpha proj.3+local.podman --role=worker"}) {
		t.Fatalf("set-role %q", got)
	}
}

// Plan 00203 Task 3.8 (Fable's F6): making the coordinator a worker is destructive, so the
// question a remove asks about it starts on No.
func TestRemoveOfTheCoordinatorAsksAboutItsRoleDefaultingToNo(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.m.roles["alpha\tproj.3+local.podman"] = coord
	c := r.interactive(t, "NO")
	if code := r.run(t, "remove", session, "alpha"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if len(c.Script.GumLog) != 1 || !strings.Contains(c.Script.GumLog[0], "Make it a worker as it leaves") ||
		!strings.Contains(c.Script.GumLog[0], "--default=false") {
		t.Fatalf("questions %q, want the role question defaulting to no", c.Script.GumLog)
	}
	if len(r.setRoles()) != 0 || r.desired(t).Generation != 1 {
		t.Fatal("no is nothing changed")
	}
}

func TestRemoveOfTheCoordinatorCanHandTheRoleToAnotherSeat(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.m.roles["alpha\tproj.3+local.podman"] = coord
	r.m.roles["alpha\trepo.5+local.podman"] = worker
	r.m.roles["alpha\tother.6+local.podman"] = worker
	r.m.roles["alpha\tmore.6+local.lxc"] = worker
	if code := r.run(t, "remove", session, "alpha", "--coordinator=hand-to=6", "--yes"); code != term.ExitUsage ||
		!strings.Contains(r.ErrOut.String(), "more.6+local.lxc, other.6+local.podman") {
		t.Fatalf("an ambiguous seat: exit %d:\n%s", code, r.ErrOut.String())
	}
	if code := r.run(t, "remove", session, "alpha", "--coordinator=hand-to=9", "--yes"); code != term.ExitUsage {
		t.Fatalf("no such seat: exit %d:\n%s", code, r.ErrOut.String())
	}
	if code := r.run(t, "remove", session, "alpha", "--coordinator=hand-to=5", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if got, want := r.setRoles(), []string{
		"alpha proj.3+local.podman --role=worker", "alpha repo.5+local.podman --role=coordinator",
	}; !slices.Equal(got, want) {
		t.Fatalf("set-role %q, want %q", got, want)
	}
}

func TestRemoveWithParkParksTheHandle(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.On("agent-bus seat remove 3@alpha", "CHANGED\tseat 3@alpha proj.3+local.podman\n", 0)
	if code := r.run(t, "remove", session, "alpha", "--park", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✓ park: proj.3+local.podman parked, seat 3 removed from the checkout")
}

func TestRemoveWhenTheSessionEndsMidwayLeavesNoMembership(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.Fake.OnFunc(strings.Fields("/usr/local/share/agent-bus/kit/pingbus seat retire"), func(execx.Cmd) (execx.Result, error) {
		r.m.container = ""
		return execx.Result{}, nil
	})
	if code := r.run(t, "remove", session, "alpha", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✓ membership: removed (the session is not running; its next launch writes it)")
	if _, err := membership.Read(r.dir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("H28: %v", err)
	}
}
