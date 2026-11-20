package shutdown

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// lxcItem is the record line of alpha's LXC member proj.box+role.lxc, with the status given.
func lxcItem(status string) string {
	return `{"container": "box", "user": "agent", "state": "present", "status": "` + status +
		`", "handle": "proj.box+role.lxc", "checkout": "/srv/proj", "why": "container not running", "since": 1}`
}

// withRecord writes alpha's LXC member record with the one item given.
func (m *machine) withRecord(item string) {
	teamtest.MustWrite(m.t, filepath.Join(m.Host.AppliedDir, "lxc", "alpha.json"),
		`{"version": 1, "team": "alpha", "members": [`+item+`]}`)
}

// withContainers makes sudo ready and the host's LXC hold box, running or not.
func (m *machine) withContainers(running bool) {
	m.On("sudo -n -v", "", 0)
	m.On("sudo -n lxc-ls -1", "box\nother\n", 0)
	m.Fake.OnFunc([]string{"sudo", "-n", "lxc-ls", "-1", "--running"}, func(execx.Cmd) (execx.Result, error) {
		if running {
			return execx.Result{Stdout: []byte("box\n")}, nil
		}
		return execx.Result{}, nil
	})
}

// An offline member is normal lifecycle (CLAUDE/AgentNotes.md, "An offline team member never
// blocks anything"): one the last apply held back as stopped, its container still stopped, is
// named and passed over. The record alone does not say it is offline now: the list does.
func TestAnLxcMemberTheRecordHeldBackIsOfflineAndDoesNotFail(t *testing.T) {
	m := newMachine(t)
	m.withRecord(lxcItem("stopped"))
	m.withContainers(false)
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t,
		"Offline, nothing to shut down:",
		"  proj.box+role.lxc   offline (container box stopped)",
		"proj.box+role.lxc: offline (container box stopped), nothing to shut down",
		"alpha: 1 exited cleanly, 0 stopped, 0 failed; 1 member offline.",
	)
	m.errLacks(t, "not reachable", "start it")
}

// lxcServer is the v1 LXC member's tmux server and session in its container.
const lxcServer = "agent-bus-claude"

// withLxcSession puts a claude session on the member's server in box, reached through
// lxc-attach as its user, and returns it; attachErr fails every lxc-attach.
func (m *machine) withLxcSession(attachErr string) *session {
	m.On("sudo -n lxc-config lxc.lxcpath", "/var/lib/lxc\n", 0)
	s := &session{name: lxcServer, dir: "/srv/proj", pane: paneReady, server: lxcServer}
	m.sessions = append(m.sessions, s)
	m.Fake.OnFunc([]string{
		"sudo", "-n", "lxc-attach", "-P", "/var/lib/lxc", "-n", "box", "--clear-env", "--",
		"runuser", "-u", "agent", "--",
	}, func(c execx.Cmd) (execx.Result, error) {
		if attachErr != "" {
			return execx.Result{ExitCode: 1, Stderr: []byte(attachErr)}, nil
		}
		at := slices.Index(c.Args, "tmux")
		if at < 0 || c.Args[at+1] != "-L" {
			return execx.Result{ExitCode: 1, Stderr: []byte("not a tmux command on a named server")}, nil
		}
		return m.tmuxOn(c.Args[at+2], c.Args[at+3:]), nil
	})
	m.build()
	return s
}

// lxcTyped is every literal text typed into the LXC member's session, in order.
func (m *machine) lxcTyped() []string {
	var got []string
	for _, c := range m.Fake.Calls() {
		if c.Name == "sudo" && slices.Contains(c.Args, "lxc-attach") && slices.Contains(c.Args, "send-keys") && c.Args[len(c.Args)-2] == "-l" {
			got = append(got, c.Args[len(c.Args)-1])
		}
	}
	return got
}

// lxcKilled is whether anything was killed or stopped in a container.
func (m *machine) lxcKilled() bool {
	for _, c := range m.Fake.Calls() {
		if c.Name == "sudo" && (slices.Contains(c.Args, "kill-session") || slices.Contains(c.Args, "lxc-stop") || slices.Contains(c.Args, "systemctl")) {
			return true
		}
	}
	return false
}

// TC2, TC8: a v1 LXC member whose container runs is closed in its session there, as any seat:
// /compact, its end read from its pane, then /exit; nothing is killed or stopped.
func TestARunningLxcMemberIsCompactedThenExitedInItsContainer(t *testing.T) {
	m := newMachine(t)
	m.withRecord(lxcItem("joined"))
	m.withContainers(true)
	m.withLxcSession("")
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if got := m.lxcTyped(); len(got) != 2 || !strings.HasPrefix(got[0], "/compact ") || got[1] != "/exit" {
		t.Fatalf("typed %q", got)
	}
	if m.lxcKilled() {
		t.Fatal("something in the container was killed or stopped")
	}
	m.errHas(t,
		"Team alpha: 2 sessions to shut down.",
		"  agent@box   seat proj.box+role.lxc, its session agent-bus-claude in container box",
		"A session still not ready at its bound is stopped (its tmux session killed) if it runs on this host, or left "+
			"running in its LXC container and reported failed. One that has not ended",
		"agent@box: exited cleanly; compacted first (its end read from its pane: it runs in container box, "+
			"whose conversation this host does not read)",
		"alpha: 2 exited cleanly, 0 stopped, 0 failed.",
	)
	m.errLacks(t, "not reachable", "offline")
}

// Started since the last apply: its container now runs, so its session is closed there too.
func TestAnLxcMemberTheRecordHeldBackWhoseContainerNowRunsIsClosed(t *testing.T) {
	m := newMachine(t)
	m.withRecord(lxcItem("stopped"))
	m.withContainers(true)
	m.withLxcSession("")
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, "agent@box: exited cleanly")
	m.errLacks(t, "offline")
}

// TC4: a running container with no session in it (sessions start on demand) has nothing to shut
// down, and is never a failure.
func TestAnLxcMemberWithNoSessionInItsContainerHasNothingToShutDown(t *testing.T) {
	m := newMachine(t)
	m.withRecord(lxcItem("joined"))
	m.withContainers(true)
	m.withLxcSession("")
	m.end(m.find(lxcServer))
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if len(m.lxcTyped()) != 0 {
		t.Fatal("something was typed into the container")
	}
	m.errHas(t, "proj.box+role.lxc: no session running in container box, nothing to shut down")
}

// TC9, TC12: a session in a container that does not end after /exit is left running, never
// killed, and fails as exit refused.
func TestAnLxcMemberThatDoesNotExitIsLeftRunning(t *testing.T) {
	m := newMachine(t)
	m.withRecord(lxcItem("joined"))
	m.withContainers(true)
	m.withLxcSession("").ignoreExit = true
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	if m.lxcKilled() {
		t.Fatal("something in the container was killed or stopped")
	}
	m.errHas(t, "agent@box: failed: exit refused: it did not end within 60 s of /exit, so it was left running in "+
		"container box: attach it there and type /exit",
		"alpha: 1 exited cleanly, 0 stopped, 1 failed.")
}

// TC12: a container lxc-attach cannot reach is named as unreachable, and fails the run.
func TestAnLxcMemberWhoseContainerCannotBeAttachedIsUnreachable(t *testing.T) {
	m := newMachine(t)
	m.withRecord(lxcItem("joined"))
	m.withContainers(true)
	m.withLxcSession("lxc-attach: failed to get init pid")
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "proj.box+role.lxc: not shut down: unreachable: ", "failed to get init pid",
		"1 member not reachable from here.")
}

func TestAJoinedLxcMemberWhoseContainerIsStoppedIsOffline(t *testing.T) {
	m := newMachine(t)
	m.withRecord(lxcItem("joined"))
	m.withContainers(false)
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, "proj.box+role.lxc: offline (container box stopped), nothing to shut down")
	m.errLacks(t, "not reachable")
}

// A member the last apply deferred has never joined: offline, not a failure.
func TestADeferredLxcMemberIsOffline(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.withRecord(`{"container": "box2", "user": "agent", "state": "present", "status": "deferred", "handle": "", ` +
		`"checkout": "/srv/proj", "why": "container not running", "since": 1}`)
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, "agent@box2: offline (its container was not running at the last apply, so it has not joined yet), nothing to shut down")
}

// Without sudo the containers cannot be listed, so whether it is online is not known: not reached.
func TestAJoinedLxcMemberWithoutSudoIsNotReached(t *testing.T) {
	m := newMachine(t)
	m.withRecord(lxcItem("joined"))
	m.On("sudo -n -v", "", 1)
	m.On("sudo -n true", "", 1)
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "proj.box+role.lxc: not shut down: lxc-attach needs sudo: sudo is not ready: no terminal")
}
