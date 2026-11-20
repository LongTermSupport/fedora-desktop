package shutdown

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const v2HostHandle = "proj.9+local.podman"

// seatStore is the owner's seat store of the fake machine (its HOME's XDG state home).
func (m *machine) seatStore() paths.Store {
	m.t.Helper()
	state, err := paths.StateHome(m.Env["HOME"], "")
	if err != nil {
		m.t.Fatal(err)
	}
	store, err := paths.SeatStore(state)
	if err != nil {
		m.t.Fatal(err)
	}
	return store
}

// withV2HostSession puts a live session named name on ccy's tmux whose desired membership, in
// the owner's seat store, names the v2 team's seat seatPart.
func (m *machine) withV2HostSession(name, seatPart string) {
	m.t.Helper()
	n, err := paths.ParseSessionName(name)
	if err != nil {
		m.t.Fatal(err)
	}
	data, err := state.Desired{
		Session: n, Generation: 1, WrittenMS: 1_760_000_000_000,
		Seats:    []state.DesiredSeat{{Team: v2Team(m.t), Seat: seatPart}},
		Farewell: []state.Farewell{}, Inbound: state.InboundAccept,
	}.Encode()
	if err != nil {
		m.t.Fatal(err)
	}
	file := m.seatStore().DesiredFile(n)
	if err = os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		m.t.Fatal(err)
	}
	if err = os.WriteFile(file, data, 0o600); err != nil {
		m.t.Fatal(err)
	}
	m.sessions = append(m.sessions, &session{name: name, dir: "/srv/proj", pane: paneReady})
}

// withV2PoolSeat puts the v2 member handle in this host's seat store pool (a checkout's seat of
// the team), held by no session.
func (m *machine) withV2PoolSeat(handle string) {
	m.t.Helper()
	dir := m.seatStore().Pool() + "/" + strings.Repeat("a", 32) + "/" + teamtest.V2Team + "/" + handle
	if err := os.MkdirAll(dir, 0o700); err != nil {
		m.t.Fatal(err)
	}
}

// TC2, TC3: a v2 host seat, a live ccy session whose desired membership names the team's seat, is
// closed on ccy's tmux as any host seat is: /compact, then /exit.
func TestAV2HostSeatsSessionIsClosed(t *testing.T) {
	m := newMachine(t)
	m.withV2(lxcstate.StatusJoined, "", teamtest.V2Member(v2HostHandle, "podman", false))
	m.withContainers(true)
	m.withV2Session()
	m.withV2HostSession("ccy-v2host", "9")
	if got := m.run(teamtest.V2Team, "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if got := m.typedInto("ccy-v2host"); len(got) != 2 || !strings.HasPrefix(got[0], "/compact ") || got[1] != "/exit" {
		t.Fatalf("typed into the host seat %q", got)
	}
	if len(m.typedInto("ccy-proj")) != 0 {
		t.Fatal("a session of another team was typed into")
	}
	m.errHas(t,
		"Team ops: 2 sessions to shut down.",
		"  ccy-v2host   seat "+v2HostHandle+", its ccy session on this host",
		"ccy-v2host: exited cleanly",
		"ops: 2 exited cleanly, 0 stopped, 0 failed.",
	)
	m.errLacks(t, "not reachable")
}

// TC4: a v2 host member whose seat is in this host's pool but held by no live session has no
// session: named, never a failure.
func TestAV2HostSeatWithNoSessionHasNothingToShutDown(t *testing.T) {
	m := newMachine(t)
	m.withV2(lxcstate.StatusJoined, "", teamtest.V2Member(v2HostHandle, "podman", false))
	m.withContainers(false)
	m.withV2PoolSeat(v2HostHandle)
	if got := m.run(teamtest.V2Team, "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, v2HostHandle+": "+whyNoSession+" on this host, nothing to shut down")
}

// TC12: a v2 host member that no live session holds and that is not a seat of this host's pool
// runs elsewhere, if anywhere: out of reach, with why.
func TestAV2HostMemberOfAnotherMachineIsOutOfReach(t *testing.T) {
	m := newMachine(t)
	m.withV2(lxcstate.StatusJoined, "", teamtest.V2Member(v2HostHandle, "podman", false))
	m.withContainers(false)
	if got := m.run(teamtest.V2Team, "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, v2HostHandle+": not shut down: "+whyV2Elsewhere)
}

// A desired membership that cannot be read is not "no session": the session is named as not
// reachable, with why.
func TestAnUnreadableDesiredMembershipIsNotReachable(t *testing.T) {
	m := newMachine(t)
	m.withV2(lxcstate.StatusJoined, "", teamtest.V2Member(v2HostHandle, "podman", false))
	m.withContainers(false)
	m.withV2HostSession("ccy-v2host", "9")
	n, err := paths.ParseSessionName("ccy-v2host")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(m.seatStore().DesiredFile(n), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := m.run(teamtest.V2Team, "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, "session ccy-v2host: not shut down: unreachable: ")
	if len(m.typedInto("ccy-v2host")) != 0 {
		t.Fatal("a session whose membership could not be read was typed into")
	}
}
