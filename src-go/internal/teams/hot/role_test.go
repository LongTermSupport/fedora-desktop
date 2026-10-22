package hot

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRoleMakesTheSeatCoordinatorAndWaitsForTheSessionToBeTold(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	if code := r.run(t, "role", session, "alpha", coord, "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(),
		"✓ team alpha: homeserver up, 1 of 64 agents",
		"✓ role: proj.3+local.podman set to coordinator",
		"✓ session: told it is alpha's coordinator (0.0 s)")
	if got := r.setRoles(); !slices.Equal(got, []string{"alpha proj.3+local.podman --role=coordinator"}) {
		t.Fatalf("set-role %q", got)
	}
	if code := r.run(t, "role", session, "alpha", coord, "--yes"); code != term.ExitOK {
		t.Fatalf("rerun exit %d:\n%s", code, r.ErrOut.String())
	}
	onlySkips(t, r.ErrOut.String())
	wantLines(t, r.ErrOut.String(), "- role: proj.3+local.podman is a coordinator already",
		"Nothing to do: proj.3+local.podman is alpha's coordinator, and the session knows it.")
	if len(r.setRoles()) != 1 {
		t.Fatal("a rerun changes no role")
	}
}

// Fable's M4 N1: on the wall clock no two readings are equal, so the no-op must not depend on
// the clock standing still between them.
func TestARoleThatAlreadyHoldsIsANoOpOnAClockThatMovesOnEveryReading(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.m.roles["alpha\tproj.3+local.podman"] = coord
	r.writeReport(t)
	r.x.Now = func() time.Time {
		r.now = r.now.Add(time.Millisecond)
		return r.now
	}
	if code := r.run(t, "role", session, "alpha", coord, "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	onlySkips(t, r.ErrOut.String())
	wantLines(t, r.ErrOut.String(), "- session: told it is alpha's coordinator",
		"Nothing to do: proj.3+local.podman is alpha's coordinator, and the session knows it.")
}

func TestRoleToWorker(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.m.roles["alpha\tproj.3+local.podman"] = coord
	if code := r.run(t, "role", session, "alpha", worker, "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✓ role: proj.3+local.podman set to worker", "✓ session: told it is a worker in alpha (0.0 s)")
}

func TestRoleWithAnotherCoordinatorNeedsDemoteCurrent(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.m.roles["alpha\tother.5+local.podman"] = coord
	if code := r.run(t, "role", session, "alpha", coord, "--yes"); code != term.ExitUsage {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if len(r.setRoles()) != 0 {
		t.Fatal("nothing changed")
	}
	if code := r.run(t, "role", session, "alpha", coord, "--demote-current", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if got, want := r.setRoles(), []string{
		"alpha other.5+local.podman --role=worker", "alpha proj.3+local.podman --role=coordinator",
	}; !slices.Equal(got, want) {
		t.Fatalf("set-role %q, want %q", got, want)
	}
}

func TestRoleNotToldInTimeSaysSo(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.m.tells = false
	if code := r.run(t, "role", session, "alpha", coord, "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✓ role: proj.3+local.podman set to coordinator",
		"✗ session: not told within 10 s that it is alpha's coordinator: its watcher tells it at its next sync of alpha, "+
			"or its next prompt does; run the same command again to wait for it")
}

func TestRoleNotToldBecauseTheReportCannotBeReadSaysWhy(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.m.tells = false
	r.m.report = "{not json"
	if code := r.run(t, "role", session, "alpha", coord, "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if got := r.ErrOut.String(); !strings.Contains(got, "the session's report could not be read: ") {
		t.Fatalf("the read error is said:\n%s", got)
	}
}

func TestRoleNotToldWithNoReportYetIsTheTimeoutNotAReadError(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.m.tells = false
	r.m.report = "-"
	if code := r.run(t, "role", session, "alpha", coord, "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	got := r.ErrOut.String()
	if !strings.Contains(got, "not told within 10 s") || strings.Contains(got, "could not be read") {
		t.Fatalf("a report not written yet is waited for:\n%s", got)
	}
}

func TestRoleOfATeamTheSessionIsNotInOrWhoseHomeserverIsDown(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	if code := r.run(t, "role", session, "beta", coord, "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✗ role: "+session+" is not in beta")
	r.On("systemctl is-active agent-bus-hs@alpha.service", "inactive\n", 3)
	if code := r.run(t, "role", session, "alpha", coord, "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if len(r.setRoles()) != 0 {
		t.Fatal("H26: a role is not changed while the homeserver is down")
	}
}
