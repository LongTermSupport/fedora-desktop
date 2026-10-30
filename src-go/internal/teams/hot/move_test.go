package hot

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
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
	if at := strings.Index(got, "This session is on 1 team:"); at < strings.Index(got, "Moved: ") {
		t.Fatalf("the block, after the move, names the one team left:\n%s", got)
	}
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

// The coordinator pre-check that cannot read the old team stops the move with the cause, before
// the add changes anything: an unread team is never taken as "not the coordinator".
func TestMoveWhoseCoordinatorCheckCannotReadTheOldTeamChangesNothing(t *testing.T) {
	cases := []struct {
		name string
		fail func(r *rig)
		say  string
	}{
		{"systemctl cannot run", func(r *rig) {
			r.Fake.OnFunc([]string{"systemctl", "is-active", "agent-bus-hs@beta.service"}, func(execx.Cmd) (execx.Result, error) {
				return execx.Result{}, errors.New("no systemctl")
			})
		}, "✗ team beta: whether its homeserver is up could not be read, so whether seat 1 is its coordinator is unknown: "},
		{"the seat list cannot be read", func(r *rig) {
			r.CrashSeat(t, r.Checkout(), "beta", "9")
		}, "✗ team beta: its seat lock could not be read, so whether seat 1 is its coordinator is unknown: agent-bus seat list in "},
		{"the roster cannot be read", func(r *rig) {
			r.OnErr(teamtest.SudoBus+"list beta", "no route", 2)
		}, "✗ team beta: its members could not be read, so whether seat 1 is its coordinator is unknown: "},
	}
	for _, c := range cases {
		r := newRig(t, membership.Seat{Team: "beta", Seat: "1"})
		c.fail(r)
		if code := r.run(t, "move", session, "beta", "alpha", "--yes"); code != term.ExitFailure {
			t.Errorf("%s: exit %d:\n%s", c.name, code, r.ErrOut.String())
			continue
		}
		if got := r.ErrOut.String(); !strings.Contains(got, c.say) || !strings.Contains(got, "nothing was changed.") {
			t.Errorf("%s: stderr lacks %q:\n%s", c.name, c.say, got)
		}
		if r.desired(t).Generation != 1 {
			t.Errorf("%s: the membership changed", c.name)
		}
	}
}

// Fable's M4 N3: a hand-to that names no member, or several, is a usage error known before
// the add, so the session is never left in both teams for it.
func TestMoveOfTheCoordinatorToAnUnresolvableHeirChangesNothing(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "beta", Seat: "1"})
	r.m.roles["beta\tproj.1+local.podman"] = coord
	r.m.roles["beta\tone.7+local.podman"] = worker
	r.m.roles["beta\ttwo.7+local.podman"] = worker
	for _, heir := range []string{"9", "7"} {
		if code := r.run(t, "move", session, "beta", "alpha", "--coordinator=hand-to="+heir, "--yes"); code != term.ExitUsage {
			t.Fatalf("hand-to=%s: exit %d:\n%s", heir, code, r.ErrOut.String())
		}
		if !strings.Contains(r.ErrOut.String(), "--coordinator=hand-to="+heir) {
			t.Fatalf("hand-to=%s: the refusal does not name the option:\n%s", heir, r.ErrOut.String())
		}
		if r.desired(t).Generation != 1 || len(r.setRoles()) != 0 {
			t.Fatalf("hand-to=%s: something changed before the refusal", heir)
		}
	}
}
