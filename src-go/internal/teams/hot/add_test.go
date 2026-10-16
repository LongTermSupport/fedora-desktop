package hot

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestAddToAPlainSessionRelaunchesItOnce(t *testing.T) {
	r := newRig(t)
	if code := r.run(t, "add", session, "alpha/3", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(),
		"✓ session "+session+": running, can be hot-controlled",
		"✓ team alpha: homeserver up",
		"✓ seat 3: free, not named by any session in "+r.Checkout(),
		"- seat 3: exists (no sudo needed)",
		"✓ route: the container reaches alpha's homeserver",
		"✓ record: --teams 3@alpha",
		"✓ membership: generation 2 written",
		"✓ session: seat 3 held, applied (0.0 s)",
		"✓ session: relaunched once to accept team messages (it started on no team): resumed, seat 3 held (0 s)",
		"Done. The session is told it joined alpha as proj.3+local.podman.")
	if got := r.recordArgs(t); !slices.Equal(got, []string{"--no-ssh", "--teams", "3@alpha"}) {
		t.Fatalf("record %v", got)
	}
	if d := r.desired(t); d.Generation != 2 || !slices.Equal(d.Seats, []membership.Seat{{Team: "alpha", Seat: "3"}}) {
		t.Fatalf("desired %+v", d)
	}
}

func TestAddToASeatedSessionIsHotAndTheRerunChangesNothing(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "beta", Seat: "1"})
	if code := r.run(t, "add", session, "alpha", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✓ record: --teams 1@beta,3@alpha",
		"Done. The session is told it joined alpha as proj.3+local.podman. Nothing restarted.")
	if strings.Contains(r.ErrOut.String(), "relaunch") {
		t.Fatalf("a seated session is not relaunched:\n%s", r.ErrOut.String())
	}
	if code := r.run(t, "add", session, "alpha/3", "--yes"); code != term.ExitOK {
		t.Fatalf("rerun exit %d:\n%s", code, r.ErrOut.String())
	}
	for line := range strings.Lines(r.ErrOut.String()) {
		if (strings.HasPrefix(line, "✓") && !strings.Contains(line, "can be hot-controlled")) || strings.HasPrefix(line, "✗") {
			t.Fatalf("a rerun does nothing, every step '-':\n%s", r.ErrOut.String())
		}
	}
	wantLines(t, r.ErrOut.String(), "Nothing to do: "+session+" is already in alpha as seat 3.")
	if r.desired(t).Generation != 2 {
		t.Fatal("a rerun writes nothing")
	}
	if code := r.run(t, "add", session, "alpha/4", "--yes"); code != term.ExitUsage {
		t.Fatalf("a second seat in a team: exit %d", code)
	}
}

func TestAddCreatesANewSeatThroughSeatTake(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "beta", Seat: "1"})
	r.m.heldBy["3@alpha"] = true
	if code := r.run(t, "add", session, "alpha", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if !r.Fake.Called("agent-bus seat take 4@alpha --type=podman --no-prompt") {
		t.Fatalf("seat 4 not created: %v", r.Fake.Calls())
	}
	wantLines(t, r.ErrOut.String(), "✓ seat 4: created (proj.4+local.podman)")
}

func TestAddOfAHeldSeatChangesNothing(t *testing.T) {
	r := newRig(t)
	r.m.heldBy["3@alpha"] = true
	if code := r.run(t, "add", session, "alpha/3", "--yes"); code != exitBusy {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✗ seat 3: held by another session")
	if r.desired(t).Generation != 1 || len(r.recordArgs(t)) != 1 {
		t.Fatal("nothing is written for a held seat")
	}
}

func TestAddRefusesASeatAnotherLiveSessionWants(t *testing.T) {
	r := newRig(t)
	r.On("tmux -L ccy list-sessions", session+" 0 /x\nccy-other 0 /x\n", 0)
	if err := membership.Write(membership.Dir(r.runtime, "ccy-other"), membership.Desired{
		V: 1, Session: "ccy-other", Kind: "ccy", Checkout: r.Checkout(), Generation: 1, WrittenMS: 1,
		Seats: []membership.Seat{{Team: "alpha", Seat: "3"}}, Farewell: []membership.Farewell{},
	}); err != nil {
		t.Fatal(err)
	}
	if code := r.run(t, "add", session, "alpha/3", "--yes"); code != exitBusy {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✗ seat 3: named by session ccy-other, which has not taken it yet")
}

func TestAddThatTheKeeperRefusesIsRolledBack(t *testing.T) {
	r := newRig(t)
	r.m.keeper = false
	if code := r.run(t, "add", session, "alpha/3", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✗ session: seat 3 not held after 10 s: no-bundle",
		"✓ record: restored (no --teams)", "✓ membership: generation 3 written (restored)")
	if d := r.desired(t); d.Generation != 3 || len(d.Seats) != 0 {
		t.Fatalf("desired %+v", d)
	}
	if got := r.recordArgs(t); !slices.Equal(got, []string{"--no-ssh"}) {
		t.Fatalf("record %v", got)
	}
}

func TestAddWhenTheSessionEndsMidwayIsRolledBack(t *testing.T) {
	r := newRig(t)
	r.Fake.OnFunc([]string{"podman", "exec"}, func(execx.Cmd) (execx.Result, error) {
		r.m.container = ""
		return execx.Result{}, nil
	})
	if code := r.run(t, "add", session, "alpha/3", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✗ session: session ended during the add; seat 3 left free")
	if len(r.desired(t).Seats) != 0 {
		t.Fatal("restored")
	}
}

func TestAddWhoseRelaunchDoesNotComeSaysWhatToDo(t *testing.T) {
	r := newRig(t)
	r.m.relaunch = false
	if code := r.run(t, "add", session, "alpha/3", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if !strings.Contains(r.ErrOut.String(), "✗ session: not relaunched within 180 s") {
		t.Fatalf("stderr:\n%s", r.ErrOut.String())
	}
	if _, err := os.Lstat(filepath.Join(r.dir(), membership.RelaunchFile)); err != nil {
		t.Fatalf("the relaunch request stays: %v", err)
	}
	if code := r.run(t, "add", session, "alpha/3", "--yes"); code != term.ExitFailure {
		t.Fatalf("a rerun while the relaunch is pending: exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✗ session: in alpha as seat 3, but not relaunched yet: team messages cannot wake it "+
		"until it relaunches at its next idle moment (the request stays); remove alpha to withdraw it")
	if strings.Contains(r.ErrOut.String(), "Nothing to do") {
		t.Fatalf("a pending relaunch is not nothing to do:\n%s", r.ErrOut.String())
	}
}

func TestRemoveOfTheLastTeamWithdrawsAPendingRelaunch(t *testing.T) {
	r := newRig(t)
	r.m.relaunch = false
	if code := r.run(t, "add", session, "alpha/3", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if code := r.run(t, "remove", session, "alpha", "--yes"); code != term.ExitOK {
		t.Fatalf("remove exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✓ relaunch: request withdrawn (the session is on no team now)")
	if _, err := os.Lstat(filepath.Join(r.dir(), membership.RelaunchFile)); !os.IsNotExist(err) {
		t.Fatalf("the relaunch request is withdrawn: %v", err)
	}
}

func TestReachFailureStopsTheAdd(t *testing.T) {
	r := newRig(t)
	r.OnErr("podman exec abc123 pingbus reach", "pingbus: team alpha: http://x did not answer\n", 7)
	if code := r.run(t, "add", session, "alpha/3", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	wantLines(t, r.ErrOut.String(), "✗ route: pingbus: team alpha: http://x did not answer")
	if r.desired(t).Generation != 1 {
		t.Fatal("nothing written")
	}
}
