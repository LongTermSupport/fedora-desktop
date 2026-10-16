package hot

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
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

func TestRemoveOfTheCoordinatorWaitsForRoleOptions(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.On(teamtest.SudoBus+"list alpha", "MEMBER\tproj.3+local.podman\tcoordinator\tjoin\tactive\n", 0)
	if code := r.run(t, "remove", session, "alpha", "--yes"); code != term.ExitUsage {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if r.desired(t).Generation != 1 {
		t.Fatal("nothing written")
	}
}
