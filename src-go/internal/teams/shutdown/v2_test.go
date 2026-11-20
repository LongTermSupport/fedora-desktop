package shutdown

import (
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const v2Box = "proj.1+desk.lxc"

func v2Team(t *testing.T) paths.TeamID {
	t.Helper()
	team, err := paths.ParseTeamID(teamtest.V2Team)
	if err != nil {
		t.Fatal(err)
	}
	return team
}

// v2SessionName is the v2 seat's tmux session; v2Socket its own server's socket.
func v2SessionName(t *testing.T) string {
	t.Helper()
	return paths.GuestTmuxSession(v2Team(t), "/srv/proj", 1)
}

func v2Socket(t *testing.T) string {
	t.Helper()
	return paths.GuestTmuxSocket(paths.LXCSeatKey(v2Team(t), "/srv/proj", 1))
}

// withV2 puts teamtest.V2Team on the machine: its LXC seat in box, as the record holds it with
// st and why, more members, and its homeserver unit active.
func (m *machine) withV2(st, why string, members ...status.Member) {
	m.WriteV2Status(m.t, teamtest.V2Team, true, append([]status.Member{teamtest.V2Member(v2Box, status.KindLXC, true)}, members...)...)
	handle := v2Box
	if st == lxcstate.StatusDeferred {
		handle = ""
	}
	m.WriteV2Lxc(m.t, teamtest.V2Team, teamtest.V2Seat(m.t, "box", "agent", "/srv/proj", 1, handle, st, why))
	m.On("systemctl is-active teamchat-server@"+teamtest.V2Team+".service", "active\n", 0)
	m.build()
}

// withV2Session puts a claude session on the v2 seat's own server in box, reached through
// lxc-attach as its user, and returns it.
func (m *machine) withV2Session() *session {
	m.On("sudo -n lxc-config lxc.lxcpath", "/var/lib/lxc\n", 0)
	s := &session{name: v2SessionName(m.t), dir: "/srv/proj", pane: paneReady, server: v2Socket(m.t)}
	m.sessions = append(m.sessions, s)
	m.Fake.OnFunc([]string{
		"sudo", "-n", "lxc-attach", "-P", "/var/lib/lxc", "-n", "box", "--clear-env", "--",
		"runuser", "-u", "agent", "--",
	}, func(c execx.Cmd) (execx.Result, error) {
		at := slices.Index(c.Args, "tmux")
		if at < 0 || c.Args[at+1] != "-S" {
			return execx.Result{ExitCode: 1, Stderr: []byte("not a tmux command on a socket")}, nil
		}
		return m.tmuxOn(c.Args[at+2], c.Args[at+3:]), nil
	})
	m.build()
	return s
}

// TC2, TC8, TC9, TC17: a v2 team is found on the board, and its LXC seat whose container runs is
// closed in its session on the seat's own server: /compact, its end read from its pane, then
// /exit; nothing is killed or stopped.
func TestARunningV2LxcSeatIsCompactedThenExitedInItsContainer(t *testing.T) {
	m := newMachine(t)
	m.withV2(lxcstate.StatusJoined, "")
	m.withContainers(true)
	m.withV2Session()
	name := v2SessionName(t)
	if got := m.run(teamtest.V2Team, "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if got := m.lxcTyped(); len(got) != 2 || !strings.HasPrefix(got[0], "/compact ") || got[1] != "/exit" {
		t.Fatalf("typed %q", got)
	}
	if m.lxcKilled() || len(m.typedInto("ccy-proj")) != 0 {
		t.Fatal("something was killed, stopped, or typed into another team's session")
	}
	m.errHas(t,
		"Team ops: 1 session to shut down.",
		"  "+name+"   seat "+v2Box+", its session "+name+" in container box",
		name+": exited cleanly; compacted first (its end read from its pane",
		"ops: 1 exited cleanly, 0 stopped, 0 failed.",
	)
}

// TC3, TC4: an offline entry whose container now runs is looked for too; one whose session is
// not there has nothing to shut down.
func TestAV2SeatWithNoSessionInItsContainerHasNothingToShutDown(t *testing.T) {
	m := newMachine(t)
	m.withV2(lxcstate.StatusOffline, "offline: unit down")
	m.withContainers(true)
	m.end(m.withV2Session())
	if got := m.run(teamtest.V2Team, "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if len(m.lxcTyped()) != 0 {
		t.Fatal("something was typed into the container")
	}
	// TC7: nothing is asked, and the session-less seat is listed under its heading.
	m.errHas(t, "Team ops has no running session.", "No session running, nothing to shut down:",
		v2Box+": no session running in container box, nothing to shut down")
}

// TC4: a seat whose container is stopped, or that the last apply deferred, is offline: named,
// never a failure.
func TestAV2SeatOfflineOrDeferredIsNamedAndDoesNotFail(t *testing.T) {
	for _, tc := range []struct{ st, why, want string }{
		{lxcstate.StatusJoined, "", v2Box + ": offline (container box stopped)"},
		{lxcstate.StatusDeferred, "seat held", "box:/srv/proj seat 1: offline (deferred at the last apply: seat held)"},
	} {
		m := newMachine(t)
		m.withV2(tc.st, tc.why)
		m.withContainers(false)
		if got := m.run(teamtest.V2Team, "--yes"); got != term.ExitOK {
			t.Fatalf("%s: exit %d\n%s", tc.st, got, m.ErrOut.String())
		}
		m.errHas(t, tc.want)
	}
}

// TC12: a v2 member that is not an LXC seat is named as out of reach, with why.
func TestAV2HostMemberIsOutOfReach(t *testing.T) {
	m := newMachine(t)
	m.withV2(lxcstate.StatusJoined, "", teamtest.V2Member("proj.9+local.podman", "ccy", false))
	m.withContainers(true)
	m.withV2Session()
	if got := m.run(teamtest.V2Team, "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, "proj.9+local.podman", whyV2Host)
}
