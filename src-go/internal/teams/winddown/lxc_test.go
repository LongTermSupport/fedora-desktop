package winddown

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// F3, W2: an LXC member in a running container is wound down in its session there, through
// lxc-attach as its user, on the tmux server the play's service unit starts.
func TestAnLxcMemberIsWoundDownInItsContainer(t *testing.T) {
	m := newMachine(t)
	box := m.withLxc()
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	typed := m.lxcTyped()
	if len(typed) != 3 || typed[0] != "/goal clear" || !strings.Contains(typed[1], "wind-down of team alpha") ||
		!strings.HasPrefix(typed[2], "/compact ") {
		t.Fatalf("typed %q", typed)
	}
	if len(box.entered) != 3 || box.escapes != 0 {
		t.Fatalf("entered %q escapes %d", box.entered, box.escapes)
	}
	if !m.Fake.Called("sudo -n lxc-attach -P /var/lib/lxc -n box --clear-env -- runuser -u agent -- env -i") {
		t.Fatalf("not run as the member's user in its container: %v", m.Fake.Calls())
	}
	m.errHas(t,
		"Team alpha: 2 sessions to wind down.",
		"  agent@box   LXC member proj.box+role.lxc, its session agent-bus-claude in container box",
		"✓ agent@box: /goal clear and the wind-down typed",
		"alpha: 2 wound down, 0 failed, 2 not running.",
	)
	m.errLacks(t, "not reached", "To wind them down by hand")
	if len(m.proj().entered) != 2 {
		t.Fatal("the desktop session was not wound down beside it")
	}
}

// D4: a member whose container is stopped is offline, which is normal lifecycle: it is named
// with nothing to wind down and never fails the run (CLAUDE/AgentNotes.md, "An offline team
// member never blocks anything").
func TestAnLxcMemberWhoseContainerIsStoppedIsOfflineAndPassedOver(t *testing.T) {
	m := newMachine(t)
	m.withLxc()
	m.lxcRunning = false
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if len(m.lxcTyped()) != 0 {
		t.Fatal("something was typed into a stopped container")
	}
	m.errHas(t, "- proj.box+role.lxc: not running: offline (container box stopped)",
		"alpha: 1 wound down, 0 failed, 3 not running.")
	m.errLacks(t, "not reached", "start it", "To wind them down by hand")
}

// heldBackStopped is the record of a joined member the last apply skipped, its container stopped then.
func (m *machine) heldBackStopped() {
	teamtest.MustWrite(m.t, filepath.Join(m.Host.AppliedDir, "lxc", "alpha.json"),
		`{"version": 1, "team": "alpha", "members": [{"container": "box", "user": "agent", "state": "present", `+
			`"status": "stopped", "handle": "proj.box+role.lxc", "checkout": "/srv/proj", "why": "container not running", "since": 1}]}`)
}

// The record says the container was stopped at the last apply; whether it is offline now is
// the live container list's to say: still stopped, offline.
func TestAnLxcMemberTheRecordHeldBackAsStoppedIsOfflineWhileItsContainerIsStopped(t *testing.T) {
	m := newMachine(t)
	m.withLxc()
	m.heldBackStopped()
	m.lxcRunning = false
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if len(m.lxcTyped()) != 0 {
		t.Fatal("something was typed into a stopped container")
	}
	m.errHas(t, "- proj.box+role.lxc: not running: offline (container box stopped)")
	m.errLacks(t, "not reached")
}

// Started since the last apply: its session runs, so it is wound down like a joined member.
func TestAnLxcMemberTheRecordHeldBackWhoseContainerNowRunsIsWoundDown(t *testing.T) {
	m := newMachine(t)
	m.withLxc()
	m.heldBackStopped()
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, "✓ agent@box: /goal clear and the wind-down typed", "alpha: 2 wound down, 0 failed, 2 not running.")
	m.errLacks(t, "offline")
}

// A member the last apply deferred, never joined, is offline too: named, not a failure.
func TestAnLxcMemberTheRecordDeferredIsOffline(t *testing.T) {
	m := newMachine(t)
	m.withLxc()
	teamtest.MustWrite(m.t, filepath.Join(m.Host.AppliedDir, "lxc", "alpha.json"),
		`{"version": 1, "team": "alpha", "members": [{"container": "box", "user": "agent", "state": "present", `+
			`"status": "joined", "handle": "proj.box+role.lxc", "checkout": "/srv/proj", "why": "", "since": 1}, `+
			`{"container": "box2", "user": "agent", "state": "present", `+
			`"status": "deferred", "handle": "", "checkout": "/srv/proj", "why": "container not running", "since": 1}]}`)
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, "- agent@box2: not running: offline (its container was not running at the last apply, so it has not joined yet)",
		"✓ agent@box: /goal clear and the wind-down typed")
	m.errLacks(t, "not reached", "start the container")
}

// A record status the play gives no offline meaning to stays out of reach and fails the run.
func TestAnLxcMemberTheRecordGivesAnotherStatusIsNotReached(t *testing.T) {
	m := newMachine(t)
	m.withLxc()
	teamtest.MustWrite(m.t, filepath.Join(m.Host.AppliedDir, "lxc", "alpha.json"),
		`{"version": 1, "team": "alpha", "members": [{"container": "box", "user": "agent", "state": "absent", `+
			`"status": "left", "handle": "proj.box+role.lxc", "checkout": "/srv/proj", "why": "taken off the team", "since": 1}]}`)
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "✗ proj.box+role.lxc: not reached: the last apply held it back (left: taken off the team)")
}

// D4: an LXC member no record of this host names is a container elsewhere, or joined by hand.
func TestAnLxcMemberNoRecordNamesIsNotReached(t *testing.T) {
	m := newMachine(t)
	if got := m.run("--yes", "alpha"); got != term.ExitFailure {
		t.Fatalf("exit %d: an unreachable member is not wound down", got)
	}
	m.errHas(t,
		"Not reachable from here:",
		"✗ proj.box+role.lxc: not reached: no LXC member record of this host names it",
		"alpha: 1 wound down, 0 failed, 2 not running; 1 member not reachable from here.",
		"To wind them down by hand, type /goal clear in each one's session, then:",
		"wind-down of team alpha",
	)
}

// W7: a member not run by the play's service unit has no session on that tmux server.
func TestAnLxcMemberWithNoServiceSessionFails(t *testing.T) {
	m := newMachine(t)
	m.withLxc()
	m.lxcSessions = nil
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "✗ agent@box: no agent-bus-claude tmux session runs in container box for agent "+
		"(a member started by hand, not with service: true, or its session has stopped); nothing was typed")
}

// D4: without sudo, lxc-attach cannot run, so the LXC member is not reached.
func TestAnLxcMemberWithoutSudoIsNotReached(t *testing.T) {
	m := newMachine(t)
	m.withLxc()
	m.On("sudo -n -v", "", 1)
	m.On("sudo -n true", "", 1)
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	if len(m.lxcTyped()) != 0 || len(m.proj().entered) != 2 {
		t.Fatalf("lxc %q desktop %q", m.lxcTyped(), m.proj().entered)
	}
	m.errHas(t, "✗ proj.box+role.lxc: not reached: lxc-attach needs sudo: sudo is not ready: no terminal")
}
