package winddown

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// W1: the command line is checked before anything is read.
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
		for _, want := range []string{"ccy-teams wind-down <team> [--yes]", "/goal clear", "  0  ", "  1  ", "  64  "} {
			if !strings.Contains(m.Out.String(), want) {
				t.Fatalf("%s: stdout lacks %q: %q", flag, want, m.Out.String())
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
	if len(m.typed()) != 0 {
		t.Fatal("something was typed")
	}
}

// W5, W8: /goal clear goes in first, then the wind-down text, each its own line with Enter
// pressed apart from the text after the submit pause.
func TestGoalClearLandsBeforeTheWindDownText(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	s := m.find("ccy-proj")
	if len(s.entered) != 2 || s.entered[0] != "/goal clear" {
		t.Fatalf("entered %q", s.entered)
	}
	text := s.entered[1]
	for _, want := range []string{
		"wind-down of team alpha", "about to shut down", "does not mean the work is done",
		"WIP commits are fine", "sub-agent", "push", "QA gate", "can be resumed",
		"set no new one", "STOPPING BECAUSE: the owner ordered a team wind-down",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the wind-down text lacks %q: %s", want, text)
		}
	}
	if strings.Contains(text, "\n") {
		t.Fatal("the wind-down text has a newline, which would submit it early")
	}
	if strings.Contains(text, "coordinator") {
		t.Fatalf("a worker was told it is the coordinator: %s", text)
	}
	if !slices.Contains(m.sleeps, 200*time.Millisecond) {
		t.Fatalf("no submit pause between the text and Enter: %v", m.sleeps)
	}
	if s.enters != 2 || s.escapes != 0 {
		t.Fatalf("enters %d escapes %d", s.enters, s.escapes)
	}
	m.errHas(t,
		"Team alpha: 1 session to wind down.",
		"  ccy-proj   seat 1 (proj.1+local.podman) in ~/proj",
		"Each session is sent /goal clear, then the wind-down instruction, typed into its pane at an idle moment",
		"✓ ccy-proj: /goal clear and the wind-down typed",
		"alpha: 1 wound down, 0 failed, 2 not running.",
	)
	m.errLacks(t, "To wind them down by hand")
}

// W2, D4, W9, W10: the LXC member is out of reach, so the run fails, naming it with the text to
// give it by hand.
func TestAnLxcMemberIsNotReachedAndFailsTheRun(t *testing.T) {
	m := newMachine(t)
	if got := m.run("--yes", "alpha"); got != term.ExitFailure {
		t.Fatalf("exit %d: an unreachable member is not wound down", got)
	}
	m.errHas(t,
		"Not reachable from here:",
		"  proj.box+role.lxc   its session runs inside the LXC container, out of reach of this host's tmux",
		"✓ ccy-proj: /goal clear and the wind-down typed",
		"✗ proj.box+role.lxc: not reached: its session runs inside the LXC container",
		"alpha: 1 wound down, 0 failed, 2 not running; 1 member not reachable from here.",
		"To wind them down by hand, type /goal clear in each one's session, then:",
		"wind-down of team alpha",
	)
}

// D4: a parked member and a stopped session are named, not failures.
func TestStoppedAndParkedMembersAreNamedAndDoNotFail(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.record("ccy-other", "ccy", m.Checkout(), "dev@alpha")
	m.build()
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t,
		"Not running, nothing to wind down:",
		"- proj.2+local.podman: not running: parked",
		"- dev@alpha: not running: its session ccy-other is stopped",
	)
}

func TestOnlyUnreachableMembersFailWithNothingAsked(t *testing.T) {
	m := newMachine(t)
	m.sessions = nil
	m.interactive()
	if got := m.run("alpha"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "✗ proj.box+role.lxc: not reached:")
}

// W8, W3: the coordinator is wound down like the others and told about its workers.
func TestTheCoordinatorIsToldItsWorkersAreToo(t *testing.T) {
	m := newMachine(t)
	m.coordinator()
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	s := m.find("ccy-proj")
	if len(s.entered) != 2 || !strings.Contains(s.entered[1], "You are the team's coordinator") {
		t.Fatalf("entered %q", s.entered)
	}
	m.errHas(t, "  ccy-proj   seat 1 (proj.1+local.podman) in ~/proj, the coordinator")
}

// W3: a session on other teams too says so.
func TestASessionOnOtherTeamsIsNamedWithThem(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.record("ccy-proj", "ccy", m.Checkout(), "1@alpha,2@beta")
	m.build()
	m.run("alpha", "--yes")
	m.errHas(t, "  ccy-proj   seat 1 (proj.1+local.podman) in ~/proj, also on beta (seat 2)")
}

// D2, W6: a busy session is waited for, never interrupted.
func TestABusySessionIsWaitedForNotInterrupted(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	s := m.find("ccy-proj")
	s.busyLooks = 5
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if s.escapes != 0 || len(s.entered) != 2 || s.entered[0] != "/goal clear" {
		t.Fatalf("escapes %d entered %q", s.escapes, s.entered)
	}
}

func TestASessionBusyPastTheBoundFailsWithNothingTyped(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	s := m.find("ccy-proj")
	s.busyLooks = 100000
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	if len(m.typed()) != 0 || s.escapes != 0 {
		t.Fatalf("typed %q escapes %d", m.typed(), s.escapes)
	}
	m.errHas(t, "✗ ccy-proj: it stayed busy for "+secs(300)+"; nothing was typed", "alpha: 0 wound down, 1 failed")
}

// D3: text a person left in the box is never typed over.
func TestTextInTheBoxIsNeverTypedOver(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.find("ccy-proj").box = "half a thought"
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	if len(m.typed()) != 0 {
		t.Fatalf("typed %q", m.typed())
	}
	m.errHas(t, "✗ ccy-proj: its input box holds unsent text; attach it (ccy-sessions), send or clear it, and run wind-down again")
}

// W5: an Enter that did not submit is pressed once more.
func TestALineLeftInTheBoxGetsOneMoreEnter(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	s := m.find("ccy-proj")
	s.dropEnters = 1
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if s.enters != 3 || len(s.entered) != 2 || s.entered[0] != "/goal clear" {
		t.Fatalf("enters %d entered %q", s.enters, s.entered)
	}
}

// W7: /goal clear that stays in the box stops the job before the wind-down is typed.
func TestGoalClearStuckInTheBoxStopsTheJob(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	s := m.find("ccy-proj")
	s.dropEnters = 1000
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	if typed := m.typed(); !slices.Equal(typed, []string{"/goal clear"}) {
		t.Fatalf("typed %q", typed)
	}
	m.errHas(t, "✗ ccy-proj: /goal clear stayed in its input box after a second Enter; the wind-down was not typed")
}

// W7: the wind-down text stuck in the box fails, saying the goal is already gone.
func TestTheWindDownStuckInTheBoxSaysTheGoalWentIn(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	s := m.find("ccy-proj")
	m.Fake.OnFunc([]string{"tmux", "-L", "ccy", "send-keys"}, func(c execx.Cmd) (execx.Result, error) {
		if strings.Contains(strings.Join(c.Args, " "), "wind-down of team") {
			s.dropEnters = 1000
		}
		m.press(s, c.Args[len(c.Args)-2:])
		return execx.Result{}, nil
	})
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "✗ ccy-proj: /goal clear went in, but the wind-down stayed in its input box after a second Enter")
}

// W7: a session that ends part way is a failure that says how far it got.
func TestASessionEndingAfterGoalClearFails(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.find("ccy-proj").endAfterGoal = true
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "✗ ccy-proj: /goal clear went in, but the session ended before the wind-down was typed")
}

func TestAnEndedLauncherFails(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.find("ccy-proj").pane = paneEnded
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	if len(m.typed()) != 0 {
		t.Fatal("something was typed into an ended pane")
	}
	m.errHas(t, "✗ ccy-proj: claude is not running in it (launcher-exited); nothing was typed")
}

// W5: a session busy after its wind-down is the agent at work: done.
func TestBusyAfterTheWindDownIsDone(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.find("ccy-proj").busyAfterEntered = true
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
}

// W4: the question defaults to No, and a no types nothing.
func TestTheQuestionDefaultsToNo(t *testing.T) {
	m := newMachine(t)
	c := m.interactive("|EXIT")
	if got := m.run("alpha"); got != term.ExitOK {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "Nothing was wound down.")
	if len(m.typed()) != 0 {
		t.Fatal("something was typed after a no")
	}
	args := strings.Join(c.Script.FzfArgs[0], " ")
	if !strings.Contains(args, "Wind down 1 session of alpha?") || !slices.Contains(c.Script.Rows[0], "Yes, wind down 1 session") {
		t.Fatalf("question %q rows %q", args, c.Script.Rows[0])
	}
	c.Script.HeadingAt(0, "Teams › alpha › Wind down\nteam alpha · 1 session running")
}

func TestYesWindsDown(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.interactive("|YES")
	if got := m.run("alpha"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if len(m.find("ccy-proj").entered) != 2 {
		t.Fatal("not wound down")
	}
}

func TestNoTerminalNeedsYes(t *testing.T) {
	m := newMachine(t)
	if got := m.run("alpha"); got != term.ExitUsage {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "ccy-teams: winding sessions down tells their agents to stop: confirm it with --yes when there is no terminal. See ccy-teams --help.")
	if len(m.typed()) != 0 {
		t.Fatal("something was typed")
	}
}

// W11: local only, and nothing is ended.
func TestNothingGoesOnTheBusAndNothingIsEnded(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.run("alpha", "--yes")
	for _, c := range m.Fake.Calls() {
		line := c.Line()
		switch {
		case c.Name == "tmux" && !slices.Contains(c.Args, "kill-session"), c.Name == "systemctl", c.Name == "git", c.Name == "env",
			strings.HasPrefix(line, "agent-bus seat list"),
			strings.HasPrefix(line, teamtest.SudoBus+"list "),
			strings.HasPrefix(line, teamtest.SudoBus+"human devices "),
			line == "sudo -k":
		default:
			t.Fatalf("a wind-down ran %q", line)
		}
	}
}

// W12: the menu line is the same flow, then back to the list.
func TestOfferAsksThenGoesBackToTheList(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.interactive("|EXIT")
	if _, err := m.w.Offer(m.t.Context(), "alpha"); err != nil {
		t.Fatal(err)
	}
	m.errHas(t, "Nothing was wound down.", "Press Enter to go back to the list.")
}

// W2, D4: a member of another kind is out of reach; a session holding two seats of the team is
// one job; its seats on other teams are named in order.
func TestOtherKindsAreNotReachedAndASessionIsOneJob(t *testing.T) {
	m := newMachine(t)
	m.On(teamtest.SudoBus+"list alpha", teamtest.BusList+"MEMBER\tother.1+box.host\tdev\tjoin\tactive\n", 0)
	m.On("agent-bus seat list", "SEAT\talpha\t1\theld\tself\tproj.1+local.podman\nSEAT\talpha\tdev\theld\tself\tproj.dev+local.podman\n", 0)
	m.record("ccy-proj", "ccy", m.Checkout(), "1@alpha,dev@alpha,3@gamma,2@beta")
	m.build()
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	if len(m.find("ccy-proj").entered) != 2 {
		t.Fatalf("entered %q", m.find("ccy-proj").entered)
	}
	m.errHas(t, "Team alpha: 1 session to wind down.", "also on beta (seat 2), gamma (seat 3)",
		"✗ other.1+box.host: not reached: not a session on this host's ccy tmux")
}

func TestUnreadMembersAreNotReached(t *testing.T) {
	m := newMachine(t)
	m.OnErr(teamtest.SudoBus+"list alpha", "homeserver down", 1)
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "✗ the members of alpha: not reached: the team's members could not be read")
}

func TestAHeldSeatNoSessionNamesIsNotReached(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.sessions = nil
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "✗ 1@alpha (proj.1+local.podman): not reached: held by a session ccy-teams cannot name")
}

func TestATeamWithNoMemberSaysSo(t *testing.T) {
	m := newMachine(t)
	m.On(teamtest.SudoBus+"list alpha", "HUMAN\towner\tjoin\n", 0)
	m.On("agent-bus seat list", "", 0)
	m.sessions = nil
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, "Team alpha has no member to wind down.")
}
