package shutdown

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// SD1: the command line.
func TestArgumentsAreCheckedBeforeAnythingIsRead(t *testing.T) {
	for _, args := range [][]string{{}, {"alpha", "beta"}, {"--force", "alpha"}, {"--yes"}} {
		m := newMachine(t)
		if got := m.run(args...); got != term.ExitUsage {
			t.Fatalf("%q: exit %d, want 64", args, got)
		}
		if !strings.Contains(m.ErrOut.String(), "See ccy-teams --help.") || len(m.Fake.Calls()) != 0 {
			t.Fatalf("%q: stderr %q calls %v", args, m.ErrOut.String(), m.Fake.Calls())
		}
	}
}

func TestHelpPrintsTheUsageAndExitsZero(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		m := newMachine(t)
		if got := m.run(flag); got != term.ExitOK {
			t.Fatalf("%s: exit %d, want 0", flag, got)
		}
		out := m.Out.String()
		for _, want := range []string{"ccy-teams shutdown <team> [--yes]", "/exit", "  0  ", "  1  ", "  64  "} {
			if !strings.Contains(out, want) {
				t.Fatalf("%s: stdout lacks %q: %q", flag, want, out)
			}
		}
		if m.ErrOut.Len() != 0 || len(m.Fake.Calls()) != 0 {
			t.Fatalf("%s: stderr %q calls %v", flag, m.ErrOut.String(), m.Fake.Calls())
		}
	}
}

func TestAnUnknownTeamIsRefused(t *testing.T) {
	m := newMachine(t)
	if got := m.run("omega", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "ERROR: there is no team omega on this machine; ccy-teams --list shows the teams.")
	if len(m.typedInto("ccy-proj")) != 0 {
		t.Fatal("something was typed")
	}
}

// SD4-SD6, SD10, SD11, SD14-SD16: the session is sent /exit, ends, its seat is released; the
// LXC member is listed as not shut down, so the run is not a success.
func TestEverySessionIsSentExitAndItsSeatReleased(t *testing.T) {
	m := newMachine(t)
	got := m.run("--yes", "alpha")
	if typed := m.typedInto("ccy-proj"); !slices.Equal(typed, []string{"/exit"}) {
		t.Fatalf("typed %q", typed)
	}
	if m.killed() {
		t.Fatal("a session that ended was killed")
	}
	if !slices.Contains(m.sleeps, 200*time.Millisecond) {
		t.Fatalf("no submit delay between the text and Enter: %v", m.sleeps)
	}
	m.errHas(t,
		"Team alpha: 1 session to shut down.",
		"  ccy-proj   seat 1 (proj.1+local.podman) in ~/proj",
		"Not reachable from here, so not shut down:",
		"  proj.box+role.lxc   its session runs inside the LXC container, out of reach of this host's tmux; in the container, type /exit in its session",
		"Each session is sent /exit, as if typed; one that has not ended within 60 s is stopped (its tmux session killed). The homeserver keeps running.",
		"ccy-proj: exited cleanly; seat 1@alpha released",
		"proj.box+role.lxc: not shut down: its session runs inside the LXC container",
		"alpha: 1 exited cleanly, 0 stopped, 0 failed; 1 member not reachable from here.",
	)
	if got != term.ExitFailure {
		t.Fatalf("exit %d: an unreachable member is not done", got)
	}
}

func TestAllSessionsEndedAndNothingOutOfReachIsSuccess(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, "alpha: 1 exited cleanly, 0 stopped, 0 failed.")
}

// SD3: local only. Nothing but tmux, the registry and the reads the table makes.
func TestNothingGoesOnTheBus(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.run("alpha", "--yes")
	for _, c := range m.Fake.Calls() {
		line := c.Line()
		switch {
		case c.Name == "tmux", c.Name == "systemctl", c.Name == "git", c.Name == "env",
			strings.HasPrefix(line, "agent-bus seat list"),
			strings.HasPrefix(line, teamtest.SudoBus+"list "),
			strings.HasPrefix(line, teamtest.SudoBus+"human devices "),
			line == "sudo -k":
		default:
			t.Fatalf("a shutdown ran %q", line)
		}
	}
}

// SD4: a session on other teams too says so, and all its seats are checked.
func TestASessionOnOtherTeamsIsNamedWithThem(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.record("ccy-proj", "ccy", m.Checkout(), "1@alpha,2@beta")
	m.run("alpha", "--yes")
	m.errHas(t, "  ccy-proj   seat 1 (proj.1+local.podman) in ~/proj, also on beta (seat 2)",
		"ccy-proj: exited cleanly; seats 1@alpha, 2@beta released")
}

// SD4: a cc session whose record names its seat is shut down the same way.
func TestACcSessionIsShutDownToo(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	if err := os.Remove(filepath.Join(m.Root, "home/.local/state/ccy/sessions/ccy-proj")); err != nil {
		t.Fatal(err)
	}
	m.sessions[0].name = "cc-proj"
	m.record("cc-proj", "cc", m.Checkout(), "1@alpha")
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if typed := m.typedInto("cc-proj"); !slices.Equal(typed, []string{"/exit"}) {
		t.Fatalf("typed %q", typed)
	}
}

// SD6: a session open on a terminal is shown as such.
func TestAnAttachedSessionIsShownAsOpen(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.sessions[0].attached = 1
	m.run("alpha", "--yes")
	m.errHas(t, "  ccy-proj   seat 1 (proj.1+local.podman) in ~/proj, open on a terminal")
}

// SD5: a held seat no session record names is out of reach.
func TestAHeldSeatWithNoNamedSessionIsNotShutDown(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.held["dev"] = true
	got := m.run("alpha", "--yes")
	m.errHas(t, "dev@alpha (proj.dev+local.podman): not shut down: held by a session ccy-teams cannot name "+
		"(a cc session started with --teams <team> alone, or one outside ccy's tmux); attach it (ccy-sessions) and type /exit")
	if got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
}

// SD5: members that could not be read are said, not taken as none.
func TestUnreadMembersAreSaid(t *testing.T) {
	m := newMachine(t)
	m.OnErr(teamtest.SudoBus+"list alpha", "sudo: a password is required", 1)
	got := m.run("alpha", "--yes")
	m.errHas(t, "the members of alpha: not shut down: the team's members could not be read (need sudo), "+
		"so members outside this host's checkouts are not listed")
	if got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
}

// SD7: nothing running.
func TestNoRunningSessionAsksNothing(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.sessions = nil
	m.held["1"] = false
	if got := m.run("alpha"); got != term.ExitOK {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "Team alpha has no running session.")
}

func TestOnlyUnreachableMembersAreListedAndFail(t *testing.T) {
	m := newMachine(t)
	m.sessions = nil
	m.held["1"] = false
	if got := m.run("alpha"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "Not reachable from here, so not shut down:", "proj.box+role.lxc: not shut down:")
}

// SD8: the question defaults to No and a no changes nothing.
func TestTheQuestionDefaultsToNo(t *testing.T) {
	m := newMachine(t)
	c := m.interactive("|EXIT")
	if got := m.run("alpha"); got != term.ExitOK {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "Nothing was shut down.")
	if len(m.typedInto("ccy-proj")) != 0 {
		t.Fatal("something was typed after a no")
	}
	args := strings.Join(c.Script.FzfArgs[0], " ")
	if !strings.Contains(args, "Shut down 1 session of alpha?") || !slices.Contains(c.Script.Rows[0], "Yes, shut down 1 session") {
		t.Fatalf("question %q rows %q", args, c.Script.Rows[0])
	}
	c.Script.HeadingAt(0, "Teams › alpha › Shut down\nteam alpha · 1 session running")
}

func TestYesShutsDown(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.interactive("|YES")
	if got := m.run("alpha"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if !slices.Equal(m.typedInto("ccy-proj"), []string{"/exit"}) {
		t.Fatal("not typed")
	}
}

// SD9: with no terminal, --yes is the only confirmation.
func TestNoTerminalNeedsYes(t *testing.T) {
	m := newMachine(t)
	if got := m.run("alpha"); got != term.ExitUsage {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "  ccy-proj   seat 1", "ccy-teams: shutting sessions down ends them: confirm it with --yes when there is no terminal. See ccy-teams --help.")
	if len(m.typedInto("ccy-proj")) != 0 {
		t.Fatal("something was typed")
	}
}

// SD12: the stopped session's record goes, as ccy-sessions' End removes it.
func TestAStoppedSessionLosesItsRecord(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.sessions[0].ignoreExit = true
	got := m.run("alpha", "--yes")
	if !m.killed() {
		t.Fatal("not stopped")
	}
	if _, err := os.Stat(filepath.Join(m.Root, "home/.local/state/ccy/sessions/ccy-proj")); !os.IsNotExist(err) {
		t.Fatalf("record still there: %v", err)
	}
	m.errHas(t, "ccy-proj: stopped: it did not end within 60 s of /exit, so its tmux session was killed; seat 1@alpha released",
		"alpha: 0 exited cleanly, 1 stopped, 0 failed.")
	if got != term.ExitOK {
		t.Fatalf("exit %d: a stopped session with its seat released is shut down", got)
	}
}
