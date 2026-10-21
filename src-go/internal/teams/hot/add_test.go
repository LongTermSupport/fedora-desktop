package hot

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
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
		"✓ team alpha: homeserver up, 1 of 64 agents",
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
	onlySkips(t, r.ErrOut.String())
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
	other := registry.Record{Name: "ccy-other", Dir: r.Checkout(), Launcher: "/x/ccy", Prefix: "ccy", Args: []string{"--teams", "3@alpha"}}
	if err := r.store.Write(other); err != nil {
		t.Fatal(err)
	}
	if code := r.run(t, "add", session, "alpha/3", "--yes"); code != exitBusy {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✗ seat 3: named by session ccy-other, which has not taken it yet")
	// Fable's M6 review B1: the name now runs a ccy --no-bus, whose launch wrote that record; the
	// membership left under the name is an earlier session's and reserves nothing.
	other.Args = []string{"--no-bus"}
	if err := r.store.Write(other); err != nil {
		t.Fatal(err)
	}
	if code := r.run(t, "add", session, "alpha/3", "--yes"); code != term.ExitOK {
		t.Fatalf("a stale membership under a --no-bus session's name: exit %d:\n%s", code, r.ErrOut.String())
	}
}

// Fable's M6 review B1, the ccy half: a ccy --no-bus under the name of an ended ccy session finds
// that session's membership; its own record says --no-bus, so it is not changed.
func TestAddToACcyNoBusSessionUnderAReusedNameIsRefused(t *testing.T) {
	r := newRig(t)
	rec := registry.Record{Name: session, Dir: r.Checkout(), Launcher: "/x/ccy", Prefix: "ccy", Restore: true, Args: []string{"--no-bus"}}
	if err := r.store.Write(rec); err != nil {
		t.Fatal(err)
	}
	if code := r.run(t, "add", session, "alpha/3", "--yes"); code != exitConfig {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✗ session "+session+": it was started with --no-bus, so it is not on the agent team bus "+
		"(the membership under its name is an earlier session's); relaunch it from ccy's tmux without --no-bus to change it while it runs")
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
	wantLines(t, r.ErrOut.String(), "✗ session: session ended during the add; seat 3 left free",
		"✓ record: restored (no --teams)",
		"✓ membership: removed (the session is not running; its next launch writes it)")
	if _, err := membership.Read(r.dir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an ended session keeps no desired membership (H28): %v", err)
	}
	if got := r.recordArgs(t); !slices.Equal(got, []string{"--no-ssh"}) {
		t.Fatalf("record %v", got)
	}
}

// Fable's M4 B2: a record that cannot be restored must not stop the desired file's removal.
func TestAnEndedSessionLeavesNoDesiredFileEvenWhenItsRecordCannotBeRestored(t *testing.T) {
	r := newRig(t)
	r.Fake.OnFunc([]string{"podman", "exec"}, func(execx.Cmd) (execx.Result, error) {
		r.m.container = ""
		r.m.onPs = func() { r.breakRecords(t) }
		return execx.Result{}, nil
	})
	if code := r.run(t, "add", session, "alpha/3", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✓ membership: removed (the session is not running; its next launch writes it)")
	if !strings.Contains(r.ErrOut.String(), "✗ record: ") {
		t.Fatalf("the failed record write is not said:\n%s", r.ErrOut.String())
	}
	if _, err := membership.Read(r.dir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an ended session keeps no desired membership (H28): %v", err)
	}
}

func TestARefusedAddRestoresTheDesiredFileEvenWhenItsRecordCannotBeRestored(t *testing.T) {
	r := newRig(t)
	r.m.keeper = false
	sleep := r.x.Sleep
	r.x.Sleep = func(d time.Duration) {
		if r.m.onPs == nil {
			r.m.onPs = func() {}
			r.breakRecords(t)
		}
		sleep(d)
	}
	if code := r.run(t, "add", session, "alpha/3", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✓ membership: generation 3 written (restored)")
	if d := r.desired(t); d.Generation != 3 || len(d.Seats) != 0 {
		t.Fatalf("the desired file was not restored: %+v", d)
	}
}

func TestAddAsCoordinatorNeedsDemoteCurrentWhenAnotherHoldsTheRole(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "beta", Seat: "1"})
	r.m.roles["alpha\tother.5+local.podman"] = coord
	if code := r.run(t, "add", session, "alpha/3", "--role=coordinator", "--yes"); code != term.ExitUsage {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if r.desired(t).Generation != 1 || len(r.setRoles()) != 0 {
		t.Fatal("nothing is changed without --demote-current")
	}
	if code := r.run(t, "add", session, "alpha/3", "--role=coordinator", "--demote-current", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if got, want := r.setRoles(), []string{
		"alpha other.5+local.podman --role=worker", "alpha proj.3+local.podman --role=coordinator",
	}; !slices.Equal(got, want) {
		t.Fatalf("set-role %q, want %q", got, want)
	}
	wantLines(t, r.ErrOut.String(),
		"✓ role: other.5+local.podman set to worker (it was alpha's coordinator)",
		"✓ role: proj.3+local.podman set to coordinator")
}

func TestAddAsWorkerOfAWorkerChangesNoRole(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "beta", Seat: "1"})
	if code := r.run(t, "add", session, "alpha/3", "--role=worker", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "- role: proj.3+local.podman is a worker already")
	if len(r.setRoles()) != 0 {
		t.Fatal("no role change")
	}
}

func TestAddThatIsRolledBackPutsTheRolesBack(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "beta", Seat: "1"})
	r.m.keeper = false
	r.m.roles["alpha\tother.5+local.podman"] = coord
	if code := r.run(t, "add", session, "alpha/3", "--role=coordinator", "--demote-current", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if r.m.role("alpha", "other.5+local.podman") != coord || r.m.role("alpha", "proj.3+local.podman") != worker {
		t.Fatalf("roles not put back: %v", r.setRoles())
	}
	wantLines(t, r.ErrOut.String(), "✓ role: put back as it was")
}

func TestAddRefusesANewMemberWhenTheRosterIsFull(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "beta", Seat: "1"})
	r.m.heldBy["3@alpha"] = true
	r.m.others["alpha"] = 63
	if code := r.run(t, "add", session, "alpha", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✓ team alpha: homeserver up, 64 of 64 agents",
		"✗ seat 4: alpha has 64 agents, the most a team can have: park or remove a member first (ccy-teams, Ctrl-X on a member)")
	if r.Fake.Called("agent-bus seat take") || r.desired(t).Generation != 1 {
		t.Fatal("nothing is created or written for a full roster")
	}
}

func TestAddRefusesASeatAStoppedSessionsRecordRestores(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "beta", Seat: "1"})
	if err := r.store.Write(registry.Record{
		Name: "ccy-old", Dir: r.Checkout(), Launcher: "/x/ccy", Prefix: "ccy", Restore: true,
		Args: []string{"--teams", "3@alpha"},
	}); err != nil {
		t.Fatal(err)
	}
	if code := r.run(t, "add", session, "alpha/3", "--yes"); code != exitBusy {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✗ seat 3: named by session ccy-old's record, which a restore starts again")
	if code := r.run(t, "add", session, "alpha", "--yes"); code != term.ExitOK {
		t.Fatalf("the chooser passes over it: exit %d:\n%s", code, r.ErrOut.String())
	}
	if !slices.Equal(r.desired(t).Seats, []membership.Seat{{Team: "beta", Seat: "1"}, {Team: "alpha", Seat: "4"}}) {
		t.Fatalf("desired %+v", r.desired(t).Seats)
	}
}

func TestAddTakesASeatThatOnlyAnotherCheckoutOrAnUnrestoredRecordNames(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "beta", Seat: "1"})
	for _, rec := range []registry.Record{
		{Name: "ccy-elsewhere", Dir: "/elsewhere", Launcher: "/x/ccy", Prefix: "ccy", Restore: true, Args: []string{"--teams", "3@alpha"}},
		{Name: "ccy-done", Dir: r.Checkout(), Launcher: "/x/ccy", Prefix: "ccy", Restore: false, Args: []string{"--teams", "3@alpha"}},
	} {
		if err := r.store.Write(rec); err != nil {
			t.Fatal(err)
		}
	}
	r.On("tmux -L ccy list-sessions", session+" 0 /x\nccy-far 0 /x\n", 0)
	if err := membership.Write(membership.Dir(r.runtime, "ccy-far"), membership.Desired{
		V: 1, Session: "ccy-far", Kind: "ccy", Checkout: "/elsewhere", Generation: 1, WrittenMS: 1,
		Seats: []membership.Seat{{Team: "alpha", Seat: "3"}}, Farewell: []membership.Farewell{},
	}); err != nil {
		t.Fatal(err)
	}
	if code := r.run(t, "add", session, "alpha/3", "--yes"); code != term.ExitOK {
		t.Fatalf("the same seat of another checkout is another seat: exit %d:\n%s", code, r.ErrOut.String())
	}
}

func TestASeatAllocationInProgressInTheCheckoutRefusesTheAdd(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "beta", Seat: "1"})
	path, err := allocPath(r.runtime, r.Checkout())
	if err != nil {
		t.Fatal(err)
	}
	held, err := takeLock(path, "agent-bus seat take 3@alpha (pid 9)")
	if err != nil {
		t.Fatal(err)
	}
	if code := r.run(t, "add", session, "alpha", "--yes"); code != exitBusy {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✗ seat: another seat allocation in "+r.Checkout()+" is in progress: agent-bus seat take 3@alpha (pid 9)")
	if r.desired(t).Generation != 1 {
		t.Fatal("nothing written")
	}
	if releaseErr := held.release(); releaseErr != nil {
		t.Fatal(releaseErr)
	}
	if code := r.run(t, "add", session, "alpha", "--yes"); code != term.ExitOK {
		t.Fatalf("free again: exit %d:\n%s", code, r.ErrOut.String())
	}
	if again, err := takeLock(path, "x"); err != nil {
		t.Fatalf("the add let the allocation lock go: %v", err)
	} else if err := again.release(); err != nil {
		t.Fatal(err)
	}
}

func TestAddSaysWhenTheKeepersReportCannotBeRead(t *testing.T) {
	r := newRig(t)
	r.m.report = "{not json"
	if code := r.run(t, "add", session, "alpha/3", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if got := r.ErrOut.String(); !strings.Contains(got, "is not what a keeper writes") {
		t.Fatalf("the read error is said:\n%s", got)
	}
}

func TestAddWhoseRelaunchedSessionReportCannotBeReadSaysWhy(t *testing.T) {
	r := newRig(t)
	r.m.onPs = func() {
		if _, err := os.Lstat(filepath.Join(r.dir(), membership.RelaunchFile)); err == nil {
			r.m.report = "{not json"
		}
	}
	if code := r.run(t, "add", session, "alpha/3", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if got := r.ErrOut.String(); !strings.Contains(got, "the session's report could not be read: ") {
		t.Fatalf("the read error is said:\n%s", got)
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

func TestTheOwedFarewellRerunWithdrawsAPendingRelaunchOnNoTeam(t *testing.T) {
	r := newRig(t)
	r.m.relaunch = false
	if code := r.run(t, "add", session, "alpha/3", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	// The first remove is cut short after the desired file dropped the seat: the goodbye fails,
	// the farewell stays owed and the relaunch request is still there.
	r.OnErr("/usr/local/share/agent-bus/kit/pingbus seat farewell", "pingbus: homeserver: unreachable\n", 7)
	if code := r.run(t, "remove", session, "alpha", "--yes"); code != term.ExitFailure {
		t.Fatalf("remove exit %d:\n%s", code, r.ErrOut.String())
	}
	if err := membership.WriteRelaunch(r.dir(), 3, r.now); err != nil {
		t.Fatal(err)
	}
	r.On("/usr/local/share/agent-bus/kit/pingbus seat farewell", "", 0)
	if code := r.run(t, "remove", session, "alpha", "--yes"); code != term.ExitOK {
		t.Fatalf("rerun exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "- session: seat 3 already released",
		"✓ relaunch: request withdrawn (the session is on no team now)")
	if _, err := os.Lstat(filepath.Join(r.dir(), membership.RelaunchFile)); !os.IsNotExist(err) {
		t.Fatalf("the relaunch request is withdrawn on the rerun: %v", err)
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
