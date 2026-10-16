package hot

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Plan 00203 M6 (H31, H32): a cc session started with --teams or --bus is hot-controlled on the
// ccy model. It has no container: it is found by its membership under pingbus/membership/ and its
// tmux session on ccy's server, the route is the host's own, and its seats are host seats.

const ccSession = "cc-proj"

func newCCRig(t *testing.T, seats ...membership.Seat) *rig {
	t.Helper()
	return newRigAs(t, membership.KindCC, ccSession, seats...)
}

func TestACCSessionIsAddedMovedAndRemovedWhileItRuns(t *testing.T) {
	r := newCCRig(t, membership.Seat{Team: "beta", Seat: "1"})
	if code := r.run(t, "add", ccSession, "alpha", "--yes"); code != term.ExitOK {
		t.Fatalf("add: exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(),
		"✓ session "+ccSession+": running, can be hot-controlled",
		"✓ route: this host reaches alpha's homeserver",
		"✓ record: --teams 1@beta,3@alpha",
		"Done. The session is told it joined alpha as proj.3+local.host. Nothing restarted.")
	if !r.Fake.Called("/usr/local/share/agent-bus/kit/pingbus reach --team alpha --root "+filepath.Join(r.Checkout(), ".claude", "ccy", "pingbus", "seats")) ||
		r.Fake.Called("podman exec") || !r.Fake.Called("agent-bus seat choose alpha --type=host") {
		t.Fatalf("a cc session's route is the host's and its seats are host seats: %v", r.Fake.Calls())
	}
	if d := r.desired(t); d.Kind != membership.KindCC || membership.Canonical(d.Seats) != "1@beta,3@alpha" {
		t.Fatalf("desired %+v", d)
	}
	if code := r.run(t, "remove", ccSession, "alpha", "--yes"); code != term.ExitOK {
		t.Fatalf("remove: exit %d:\n%s", code, r.ErrOut.String())
	}
	if code := r.run(t, "move", ccSession, "beta", "alpha/3", "--yes"); code != term.ExitOK {
		t.Fatalf("move: exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "Moved: "+ccSession+" is in alpha, and no longer in beta.")
	if got := membership.Canonical(r.desired(t).Seats); got != "3@alpha" {
		t.Fatalf("moved: %s", got)
	}
	if code := r.run(t, "remove", ccSession, "alpha", "--yes"); code != term.ExitOK {
		t.Fatalf("remove the last: exit %d:\n%s", code, r.ErrOut.String())
	}
	if got := r.recordArgs(t); !slices.Equal(got, []string{"--model", "x", "--bus"}) {
		t.Fatalf("a cc session left on no team stays on the bus: record %v", got)
	}
	wantLines(t, r.ErrOut.String(), "✓ record: no --teams (cc keeps --bus)")
}

// Decision 7: cc --bus starts on no team refusing team messages, as a ccy session does. cc has no
// supervisor to relaunch it, so its first join keeps the seat and the record (a restart from
// ccy-sessions starts it in that seat, accepting) and says plainly that cc must be started again,
// exiting non-zero. A re-run says it again rather than "Nothing to do".
func TestTheFirstJoinOfACCBusSessionAsksForARestart(t *testing.T) {
	r := newCCRig(t)
	want := "✗ session: " + ccSession + " is in alpha as seat 3, but it started on no team (cc --bus), so team messages " +
		"cannot reach it until cc is started again, and cc has nothing that can restart it: end it (/exit) and start it " +
		"again from ccy-sessions, which starts it in seat 3"
	if code := r.run(t, "add", ccSession, "alpha/3", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✓ record: --teams 3@alpha", want)
	if strings.Contains(r.ErrOut.String(), "Nothing restarted") {
		t.Fatalf("the first join of a cc --bus session is not hot:\n%s", r.ErrOut.String())
	}
	if _, err := os.Lstat(filepath.Join(r.dir(), membership.RelaunchFile)); err == nil {
		t.Fatal("a cc session is never asked to relaunch")
	}
	if got := r.recordArgs(t); !slices.Equal(got, []string{"--model", "x", "--teams", "3@alpha"}) {
		t.Fatalf("record %v", got)
	}
	r.ErrOut.Reset()
	if code := r.run(t, "add", ccSession, "alpha/3", "--yes"); code != term.ExitFailure {
		t.Fatalf("again: exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), want)
}

func TestACCSessionThatEndsDuringAnAddIsRolledBack(t *testing.T) {
	r := newCCRig(t, membership.Seat{Team: "beta", Seat: "1"})
	r.m.keeper = false
	calls := 0
	r.m.onPanes = func() {
		calls++
		if calls > 2 {
			r.m.pane = 0
		}
	}
	if code := r.run(t, "add", ccSession, "alpha/3", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✗ session: session ended during the add; seat 3 left free",
		"✓ membership: removed (the session is not running; its next launch writes it)")
	if got := r.recordArgs(t); !slices.Equal(got, []string{"--model", "x", "--teams", "1@beta"}) {
		t.Fatalf("record %v", got)
	}
}

func TestACCSessionNotRunningOrUnnamedCannotBeChanged(t *testing.T) {
	r := newCCRig(t, membership.Seat{Team: "beta", Seat: "1"})
	r.m.pane = 0
	if code := r.run(t, "add", ccSession, "alpha", "--yes"); code != exitConfig ||
		!strings.Contains(r.ErrOut.String(), "its tmux session is not running") {
		t.Fatalf("not running: exit %d:\n%s", code, r.ErrOut.String())
	}
	r.m.pane = 4242
	if err := r.store.Remove(ccSession); err != nil {
		t.Fatal(err)
	}
	if code := r.run(t, "add", ccSession, "alpha", "--yes"); code != exitConfig ||
		!strings.Contains(r.ErrOut.String(), "only a session ccy-sessions knows") {
		t.Fatalf("no record: exit %d:\n%s", code, r.ErrOut.String())
	}
}

// A cc session's keeper ends with its claude (its parent), so the host cannot start one that
// would; repair is refused, naming the way back: start cc again.
func TestRepairOfACCSessionIsRefused(t *testing.T) {
	r := newCCRig(t, membership.Seat{Team: "beta", Seat: "1"})
	r.m.alive = false
	if code := r.run(t, "repair", ccSession, "--yes"); code != exitConfig {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✗ keeper: "+ccSession+" is a cc session: its seat keeper ends with its claude, so "+
		"it cannot be started again from outside; end the session (/exit) and start it again from ccy-sessions, which takes its seats")
	r.m.alive = true
	if code := r.run(t, "repair", ccSession, "--yes"); code != term.ExitOK {
		t.Fatalf("a running keeper: exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "- keeper: running")
}

func TestListShowsRunningCCSessions(t *testing.T) {
	r := newCCRig(t, membership.Seat{Team: "beta", Seat: "1"})
	r.labelled("")
	if code := r.run(t, "list"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if want := "SESSION\t" + ccSession + "\tcc\t" + r.Checkout() + "\tbeta/1/worker\tok\n"; r.Out.String() != want {
		t.Fatalf("stdout\n%q\nwant\n%q", r.Out.String(), want)
	}
	r.m.alive, r.m.keeper = false, false
	if code := r.run(t, "list"); code != term.ExitOK ||
		!strings.HasSuffix(r.Out.String(), "\tbeta/1/-\tunguarded: end it and start it again from ccy-sessions\n") {
		t.Fatalf("unguarded: exit %d: %q", code, r.Out.String())
	}
	r.m.pane = 0
	if code := r.run(t, "list"); code != term.ExitOK || r.Out.Len() != 0 {
		t.Fatalf("a cc session that is not running is not listed: %q", r.Out.String())
	}
	in, err := r.x.SessionsIn(t.Context(), "beta")
	if err != nil || len(in) != 0 {
		t.Fatalf("in beta, not running: %v %v", in, err)
	}
	r.m.pane = 4242
	if in, err = r.x.SessionsIn(t.Context(), "beta"); err != nil || !slices.Equal(in, []string{ccSession}) {
		t.Fatalf("in beta: %v %v", in, err)
	}
}
