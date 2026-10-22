package restart

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// B1: the command line.
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
	m := newMachine(t)
	if got := m.run("--help"); got != term.ExitOK {
		t.Fatalf("exit %d", got)
	}
	for _, want := range []string{"ccy-teams restart <team> [--yes]", "--resume", "LXC", "  0  ", "  1  ", "  64  "} {
		if !strings.Contains(m.Out.String(), want) {
			t.Fatalf("stdout lacks %q: %q", want, m.Out.String())
		}
	}
	if len(m.Fake.Calls()) != 0 {
		t.Fatalf("calls %v", m.Fake.Calls())
	}
}

func TestAnUnknownTeamIsRefused(t *testing.T) {
	m := newMachine(t)
	if got := m.run("omega", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "ERROR: there is no team omega on this machine; ccy-teams --list shows the teams.")
}

// B3, B4, B9: a running ccy member is restarted in place through its supervisor, by a request
// that says why; the LXC member is left out and named, which is not a failure.
func TestARunningCcySessionIsRestartedInPlaceAndTheLxcMemberLeftOut(t *testing.T) {
	m := newMachine(t)
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t,
		"Team alpha: 1 session to restart.",
		"  ccy-proj   seat 1 (proj.1+local.podman) in ~/proj: running, so it restarts in place at its next idle moment, resuming its conversation in a fresh container",
		"Left out:",
		"  proj.box+role.lxc   an LXC container's agent: restart it inside its container",
		"✓ ccy-proj: restarted in place, resuming its conversation; seat 1@alpha held",
		"- proj.box+role.lxc: left out: an LXC container's agent: restart it inside its container",
		"alpha: 1 restarted, 0 failed, 0 left as they are; 1 member left out.",
	)
	if m.find("ccy-proj").container == "c1" {
		t.Fatal("the session was not restarted")
	}
	body, err := os.ReadFile(filepath.Join(m.dir("ccy-proj"), membership.RelaunchFile))
	if err != nil || !strings.Contains(string(body), `"why":"restart"`) || !strings.Contains(string(body), `"generation":3`) {
		t.Fatalf("the request %q %v", body, err)
	}
	if len(m.started) != 0 {
		t.Fatalf("a running session was started again: %q", m.started)
	}
}

// B7: a restart in place not done in time withdraws its request, so it does not restart later
// unasked, and fails.
func TestARestartInPlaceNotDoneInTimeIsWithdrawnAndFails(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.find("ccy-proj").supervised = false
	start := m.now
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	if waited := m.now.Sub(start); waited < 300*time.Second || waited > 302*time.Second {
		t.Fatalf("waited %v, want 300 s", waited)
	}
	m.errHas(t, "✗ ccy-proj: not restarted within 300 s: it may be busy, or its supervisor does not take plugins; the request is withdrawn",
		"alpha: 0 restarted, 1 failed, 0 left as they are.")
	if pending, err := membership.RelaunchPending(m.dir("ccy-proj")); err != nil || pending {
		t.Fatalf("the request stays: %v %v", pending, err)
	}
}

// A first join's relaunch already pending is not overwritten, and not withdrawn: it is that
// join's (Fable).
func TestAFirstJoinsRelaunchIsNeitherOverwrittenNorWithdrawn(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.find("ccy-proj").supervised = false
	if err := membership.WriteRelaunch(m.dir("ccy-proj"), 3, m.now); err != nil {
		t.Fatal(err)
	}
	m.run("alpha", "--yes")
	body, err := os.ReadFile(filepath.Join(m.dir("ccy-proj"), membership.RelaunchFile))
	if err != nil || strings.Contains(string(body), "why") {
		t.Fatalf("the first join's request %q %v", body, err)
	}
	m.errHas(t, "✗ ccy-proj: not restarted within 300 s", "its relaunch for a team join is still pending and stays")
}

// D9, B5: a running cc session is left running and named, not a failure.
func TestARunningCcSessionIsLeftRunning(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.withDev(&session{name: "cc-two", prefix: "cc", seat: "dev", live: true})
	m.converse("cc-two", "cc")
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t,
		"- cc-two: left running: a cc session has no supervisor to restart it; /exit it, then run ccy-teams restart alpha again to start it",
		"alpha: 1 restarted, 0 failed, 1 left as it is; 1 member left out.",
	)
	if _, err := os.Lstat(filepath.Join(membership.DirOf(m.runtime, "cc", "cc-two"), membership.RelaunchFile)); err == nil {
		t.Fatal("a cc session was asked to relaunch")
	}
}

// B6: a stopped session is started as a restore starts it, resuming its recorded
// conversation by id, and is done when its seat is held.
func TestAStoppedSessionIsStartedResumingItsConversation(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.stop(m.find("ccy-proj"))
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	id := conversationOf("ccy-proj")
	want := "env CCY_SESSION_RESTORE=1 /x/ccy --teams 1@alpha --supervise --resume " + id
	if !slices.Equal(m.started, []string{want}) {
		t.Fatalf("started %q, want %q", m.started, want)
	}
	m.errHas(t,
		"  ccy-proj   seat 1 (proj.1+local.podman) in ~/proj: stopped, so it is started, resuming conversation "+id,
		"✓ ccy-proj: started, resuming conversation "+id+"; seat 1@alpha held",
	)
}

// B6, D10: no recorded conversation is a failure, named, and nothing is started; the other
// sessions go on.
func TestAStoppedSessionWithNoRecordedConversationIsNotStarted(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.withDev(&session{name: "ccy-two", prefix: "ccy", seat: "dev", container: "d1", supervised: true})
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	if len(m.started) != 0 {
		t.Fatalf("started %q", m.started)
	}
	m.errHas(t,
		"✗ ccy-two: no conversation is recorded",
		"resume it by hand: ccy --resume in "+m.Checkout(),
		"✓ ccy-proj: restarted in place",
		"alpha: 1 restarted, 1 failed, 0 left as they are; 1 member left out.",
	)
}

// B7: a started session whose seat is not held in time fails, with where to look.
func TestAStartedSessionNotHoldingItsSeatInTimeFails(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	s := m.find("ccy-proj")
	m.stop(s)
	s.holdsOnStart = false
	start := m.now
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	if waited := m.now.Sub(start); waited < 180*time.Second || waited > 182*time.Second {
		t.Fatalf("waited %v, want 180 s", waited)
	}
	m.errHas(t, "✗ ccy-proj: started, but seat 1@alpha is not held 180 s after: it may be waiting at a question; attach it (ccy-sessions)")
}

// A ccy session without the supervisor cannot be restarted in place.
func TestARunningCcySessionWithoutTheSupervisorFails(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.record("ccy-proj", "ccy", "1@alpha\narg=--no-supervise")
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "✗ ccy-proj: it runs without the ccy supervisor (--no-supervise), which is what restarts it in place; /exit it, then run ccy-teams restart alpha again to start it")
	if pending, err := membership.RelaunchPending(m.dir("ccy-proj")); err != nil || pending {
		t.Fatalf("a request was written: %v %v", pending, err)
	}
}

// B3: a seat no session record names has nothing to restart.
func TestASeatNoRecordNamesIsLeftOut(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	if err := os.Remove(filepath.Join(m.Root, "home/.local/state/ccy/sessions/ccy-proj")); err != nil {
		t.Fatal(err)
	}
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, "proj.1+local.podman", "nothing recorded to restart; Start a ccy session in alpha")
	m.errLacks(t, "Restart 1 session")
}

// B8: the question is asked, default no; a no restarts nothing.
func TestTheQuestionDefaultsToNoAndANoRestartsNothing(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	c := m.interactive("|EXIT")
	if got := m.run("alpha"); got != term.ExitOK {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "Nothing was restarted.")
	if pending, err := membership.RelaunchPending(m.dir("ccy-proj")); err != nil || pending {
		t.Fatalf("a request was written after a no: %v %v", pending, err)
	}
	args := strings.Join(c.Script.FzfArgs[0], " ")
	if !strings.Contains(args, "Restart 1 session of alpha?") || !slices.Contains(c.Script.Rows[0], "Yes, restart 1 session") {
		t.Fatalf("question %q rows %q", args, c.Script.Rows[0])
	}
	c.Script.HeadingAt(0, "Teams › alpha › Restart\nteam alpha · 1 session to restart")
}

// B8: with no terminal, --yes is the only confirmation.
func TestNoTerminalNeedsYes(t *testing.T) {
	m := newMachine(t)
	if got := m.run("alpha"); got != term.ExitUsage {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "confirm it with --yes when there is no terminal")
	if pending, err := membership.RelaunchPending(m.dir("ccy-proj")); err != nil || pending {
		t.Fatalf("a request was written: %v %v", pending, err)
	}
}

// B11: local only. Nothing but tmux, podman and the reads the table makes.
func TestNothingGoesOnTheBus(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.run("alpha", "--yes")
	for _, c := range m.Fake.Calls() {
		line := c.Line()
		switch {
		case c.Name == "tmux", c.Name == "systemctl", c.Name == "git", c.Name == "env", c.Name == "podman",
			c.Name == "systemd-run", c.Name == "systemd-escape",
			strings.HasPrefix(line, teamtest.SudoBus+"list "),
			strings.HasPrefix(line, teamtest.SudoBus+"human devices "),
			line == "sudo -k":
		default:
			t.Fatalf("a restart ran %q", line)
		}
	}
}
