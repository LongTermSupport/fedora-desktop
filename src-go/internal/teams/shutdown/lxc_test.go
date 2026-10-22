package shutdown

import (
	"path/filepath"
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

// Started since the last apply: its session runs out of this host's reach, so it still fails.
func TestAnLxcMemberTheRecordHeldBackWhoseContainerNowRunsIsNotReached(t *testing.T) {
	m := newMachine(t)
	m.withRecord(lxcItem("stopped"))
	m.withContainers(true)
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "proj.box+role.lxc: not shut down: its session runs inside the LXC container")
	m.errLacks(t, "offline")
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

// A member whose container runs is online, and this host's tmux cannot end it: still a failure.
func TestARunningLxcMemberIsStillNotReachedAndFails(t *testing.T) {
	m := newMachine(t)
	m.withRecord(lxcItem("joined"))
	m.withContainers(true)
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "proj.box+role.lxc: not shut down: its session runs inside the LXC container",
		"alpha: 1 exited cleanly, 0 stopped, 0 failed; 1 member not reachable from here.")
	m.errLacks(t, "offline")
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
	m.errHas(t, "proj.box+role.lxc: not shut down: lxc-ls needs sudo, which is not ready")
}
