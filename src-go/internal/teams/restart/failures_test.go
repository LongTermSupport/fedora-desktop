package restart

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/screen"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// failingRunner is a prompt runner whose program cannot be run.
type failingRunner struct{}

func (failingRunner) Run(context.Context, execx.Cmd) (execx.Result, error) {
	return execx.Result{}, errors.New("fzf: cannot run")
}

// The team row's menu line runs the same flow with the picker's yes/no, names a failed run with
// its exit code, and goes back to the list once what it said has been read.
func TestTheMenuLineRestartsAndGoesBackToTheList(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.interactive("|YES")
	if _, err := m.r.Offer(t.Context(), "alpha"); err != nil {
		t.Fatal(err)
	}
	m.errHas(t, "✓ ccy-proj: restarted in place", "Press Enter to go back to the list.")
	m.errLacks(t, "failed, exit")

	m = newMachine(t)
	m.interactive()
	if _, err := m.r.Offer(t.Context(), "nosuchteam"); err != nil {
		t.Fatal(err)
	}
	m.errHas(t, "(ccy-teams restart nosuchteam failed, exit 1: see above.)", "Press Enter to go back to the list.")
}

// B8: a question left unanswered restarts nothing: one the owner leaves is a no, and one that
// could not be asked is a failure.
func TestAQuestionNotAnsweredRestartsNothing(t *testing.T) {
	for _, c := range []struct {
		name string
		set  func(*machine)
		want term.ExitCode
	}{
		{"left", func(m *machine) { m.interactive("|ABORT") }, term.ExitOK},
		{"not asked", func(m *machine) {
			m.interactive()
			m.Host.Picker.Runner = failingRunner{}
		}, term.ExitFailure},
	} {
		m := newMachine(t)
		m.noLxc()
		c.set(m)
		if got := m.run("alpha"); got != c.want {
			t.Fatalf("%s: exit %d, want %d\n%s", c.name, got, c.want, m.ErrOut.String())
		}
		if pending, err := membership.RelaunchPending(m.dir("ccy-proj")); err != nil || pending {
			t.Fatalf("%s: a request was written: %v %v", c.name, pending, err)
		}
	}
}

// A machine that cannot be read is a failure before anything is asked or done.
func TestAnUnreadableMachineRestartsNothing(t *testing.T) {
	m := newMachine(t)
	m.r.Collect = func(context.Context) (*board.Snapshot, error) { return nil, errors.New("the table could not be read") }
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	if len(m.started) != 0 {
		t.Fatalf("started %q", m.started)
	}
}

// Two sessions to act on are both asked before either is waited for, and the counts say so.
func TestTwoRunningSessionsAreBothRestarted(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.withDev(&session{name: "ccy-two", prefix: "ccy", seat: "dev", live: true, container: "d1", supervised: true})
	m.converse("ccy-two", "ccy")
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, "Team alpha: 2 sessions to restart.", "✓ ccy-two: restarted in place", "alpha: 2 restarted, 0 failed, 0 left as they are; 1 member left out.")
}

// A session record that cannot be read fails that session, naming why.
func TestAnUnreadableRecordStoreFailsTheSession(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.r.Registry = func() (*registry.Store, error) { return nil, errors.New("HOME is not set") }
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "✗ ccy-proj: its session record could not be read: HOME is not set")
}

// D8: a restart in place finds the session's membership directory under XDG_RUNTIME_DIR, so a
// relative one is refused rather than resolved against the working directory.
func TestARelativeRuntimeDirFailsARestartInPlace(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.Env["XDG_RUNTIME_DIR"] = "run"
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, "✗ ccy-proj: XDG_RUNTIME_DIR is not an absolute path, so its membership directory cannot be found")
}

// podman not answering fails a restart in place: before the request nothing is written; after
// it the request is withdrawn, so the session does not restart later unasked.
func TestPodmanNotAnsweringFailsARestartInPlace(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.Fake.On([]string{"podman", "ps"}, execx.Result{ExitCode: 125, Stderr: []byte("cannot connect")})
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, "✗ ccy-proj: podman could not list the sessions' containers")
	if pending, err := membership.RelaunchPending(m.dir("ccy-proj")); err != nil || pending {
		t.Fatalf("a request was written: %v %v", pending, err)
	}

	m = newMachine(t)
	m.noLxc()
	m.Fake.OnFunc([]string{"podman", "ps"}, func(execx.Cmd) (execx.Result, error) {
		pending, err := membership.RelaunchPending(m.dir("ccy-proj"))
		switch {
		case err != nil:
			return execx.Result{}, err
		case pending:
			return execx.Result{ExitCode: 125, Stderr: []byte("cannot connect")}, nil
		}
		return execx.Result{Stdout: []byte("c1\trunning\tccy-proj\n")}, nil
	})
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("after: exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, "✗ ccy-proj: podman could not list the sessions' containers", "; the request is withdrawn")
	if pending, err := membership.RelaunchPending(m.dir("ccy-proj")); err != nil || pending {
		t.Fatalf("the request stays: %v %v", pending, err)
	}
}

// A start tmux refuses fails at once, and nothing is waited for.
func TestAStartTmuxRefusesFails(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.stop(m.find("ccy-proj"))
	m.Fake.On([]string{"systemd-run"}, execx.Result{ExitCode: 1, Stderr: []byte("unit failed")})
	start := m.now
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if m.now != start {
		t.Fatalf("waited %v after a start that failed", m.now.Sub(start))
	}
	m.errHas(t, "✗ ccy-proj: could not start it: ")
}

// B7: a started session whose checkout's seats cannot be read any more fails, naming why, rather
// than waiting out its bound on a list it cannot read.
func TestStartedSeatsThatCannotBeReadFail(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.stop(m.find("ccy-proj"))
	m.Fake.OnFunc([]string{"systemd-run"}, func(execx.Cmd) (execx.Result, error) {
		m.CrashSeat(m.t, m.Checkout(), "alpha", "1")
		return execx.Result{}, nil
	})
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, "✗ ccy-proj: the seats in "+m.Checkout()+" could not be read: ")
}

// fedora-desktop#284: a launcher's unattended stop with no stop file, or one that cannot be read,
// is said from the pane alone, with why, and its record is kept.
func TestAnUnattendedStopWithoutItsStopFileIsSaidFromThePane(t *testing.T) {
	refused := func(m *machine) {
		s := m.find("ccy-proj")
		m.stop(s)
		s.holdsOnStart = false
		s.pane = "ERROR: the token is used up\n\nccy " + screen.UnattendedStopText + " " + screen.EndedText + "\n"
	}
	m := newMachine(t)
	m.noLxc()
	refused(m)
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "✗ ccy-proj: started, but it did not start: NOT-STARTED", "; its record is kept")
	m.errLacks(t, "could not")

	m = newMachine(t)
	m.noLxc()
	refused(m)
	stop := registry.StopFile(filepath.Join(m.Root, "home/.local/state/ccy/sessions"), "ccy-proj")
	if err := os.MkdirAll(stop, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("unreadable: exit %d", got)
	}
	m.errHas(t, "✗ ccy-proj: started, but it did not start: NOT-STARTED", "); its record is kept")

	m = newMachine(t)
	m.noLxc()
	refused(m)
	store := m.r.Registry
	asked := 0
	m.r.Registry = func() (*registry.Store, error) {
		if asked++; asked > 1 {
			return nil, errors.New("HOME is not set")
		}
		return store()
	}
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("no store: exit %d", got)
	}
	m.errHas(t, "(its stop file could not be found: HOME is not set); its record is kept")
}

// A session that holds two seats of the team is one job, named once, done when both are held.
func TestASessionOnTwoSeatsIsRestartedOnce(t *testing.T) {
	m := newMachine(t)
	m.On(teamtest.SudoBus+"list alpha", teamtest.BusList+"MEMBER\tproj.dev+local.podman\tdev\tjoin\tactive\n", 0)
	m.record("ccy-proj", "ccy", "1@alpha,dev@alpha")
	m.HoldSeat(m.t, m.Checkout(), "alpha", "dev", true)
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, "Team alpha: 1 session to restart.", "seats 1@alpha, dev@alpha held")
}

// B3: a team whose roster cannot be read still restarts the sessions this desktop's checkouts
// seat, and says the members outside them could not be listed; a member running elsewhere is
// left out and named.
func TestMembersThisDesktopCannotRestartAreNamed(t *testing.T) {
	m := newMachine(t)
	m.OnErr(teamtest.SudoBus+"list alpha", "agent-bus: no admin token", 78)
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, "✓ ccy-proj: restarted in place",
		"- the members of alpha: left out: the team's members could not be read (read failed (above)), "+
			"so members outside this desktop's checkouts are not listed")

	m = newMachine(t)
	m.On(teamtest.SudoBus+"list alpha", strings.Replace(teamtest.BusList, teamtest.BoxMember,
		"MEMBER\tfar.1+remote.podman\tdev\tjoin\tactive\n", 1), 0)
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("elsewhere: exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, "- far.1+remote.podman: left out: "+whyElsewhere)
}
