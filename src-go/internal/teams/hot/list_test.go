package hot

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func (r *rig) labelled(lines string) {
	r.Fake.OnFunc([]string{"podman", "ps", "--filter", "label=ccy-session", "--format"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{Stdout: []byte(lines)}, nil
	})
}

func TestListPrintsASessionLineForEachRunningSession(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.m.roles["alpha\tproj.3+local.podman"] = coord
	r.labelled("abc123\trunning\t" + session + "\nold1\trunning\tccy-old\ngone\texited\tccy-gone\n")
	if code := r.run(t, "list"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	want := "SESSION\tccy-old\tccy\t-\t-\told or --no-bus: relaunch it to change it while it runs\n" +
		"SESSION\t" + session + "\tccy\t" + r.Checkout() + "\talpha/3/coordinator\tok\n"
	if got := r.Out.String(); got != want {
		t.Fatalf("stdout\n%q\nwant\n%q", got, want)
	}
}

func TestListSaysWhatIsWrongWithASession(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.labelled("abc123\trunning\t" + session + "\n")
	r.On("systemctl is-active agent-bus-hs@alpha.service", "inactive\n", 3)
	if code := r.run(t, "list"); code != term.ExitOK || !strings.HasSuffix(r.Out.String(), "\talpha down\n") {
		t.Fatalf("down: exit %d: %q", code, r.Out.String())
	}
	r.m.alive, r.m.keeper = false, false
	if code := r.run(t, "list"); code != term.ExitOK ||
		!strings.HasSuffix(r.Out.String(), "\talpha/3/-\tunguarded: ccy-teams session repair "+session+"\n") {
		t.Fatalf("unguarded: exit %d: %q", code, r.Out.String())
	}
}

func TestSessionsInATeamAndTakingOneOff(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.labelled("abc123\trunning\t" + session + "\nold1\trunning\tccy-old\n")
	in, err := r.x.SessionsIn(t.Context(), "alpha")
	if err != nil || len(in) != 1 || in[0] != session {
		t.Fatalf("in alpha: %v %v", in, err)
	}
	if in, err = r.x.SessionsIn(t.Context(), "beta"); err != nil || len(in) != 0 {
		t.Fatalf("in beta: %v %v", in, err)
	}
	r.m.roles["alpha\tproj.3+local.podman"] = coord
	if !r.x.TakeOut(t.Context(), session, "alpha") {
		t.Fatalf("not taken off:\n%s", r.ErrOut.String())
	}
	if len(r.desired(t).Seats) != 0 || r.m.role("alpha", "proj.3+local.podman") != worker {
		t.Fatal("the team being taken down: its coordinator is demoted as it leaves")
	}
}

func TestRepairStartsTheKeeperAgainAndWaitsForItsSeats(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.m.alive, r.m.keeper = false, false
	if code := r.run(t, "repair", session, "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if !r.Fake.Called("podman exec -d abc123 pingbus seat keep") {
		t.Fatalf("calls %v", r.Fake.Calls())
	}
	wantLines(t, r.ErrOut.String(), "✓ keeper: running again, 1 seat held (0.0 s)")
	if code := r.run(t, "repair", session, "--yes"); code != term.ExitOK {
		t.Fatalf("rerun exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "- keeper: running", "Nothing to do: "+session+"'s seat keeper is running.")
}

func TestRepairWhoseKeeperDoesNotComeBackSaysSo(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.m.alive, r.m.keeper = false, false
	r.On("podman exec -d", "", 0)
	if code := r.run(t, "repair", session, "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✗ keeper: not running after 10 s: see /tmp/pingbus-home/keeper.log in the session")
}
