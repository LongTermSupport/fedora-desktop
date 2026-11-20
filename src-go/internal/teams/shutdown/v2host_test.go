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

// v2Checkout is the fake machine's one checkout of the seat store's pool.
var v2Checkout = strings.Repeat("a", 32)

// withV2HostSession puts a live session named name on ccy's tmux that holds the v2 team's seat
// handle in the owner's seat store (its pool seat's held/held.json names the session).
func (m *machine) withV2HostSession(name, handle string) {
	m.t.Helper()
	m.withV2Holding(handle, `{"v":1,"session":"`+name+`","claim":"0123456789abcdef","since_ms":1760000000000}`)
	m.sessions = append(m.sessions, &session{name: name, dir: "/srv/proj", pane: paneReady})
}

// withV2Holding gives the pool seat handle a held/ whose held.json is record ("" for none).
func (m *machine) withV2Holding(handle, record string) {
	m.t.Helper()
	held := m.seatStore().Pool() + "/" + v2Checkout + "/" + teamtest.V2Team + "/" + handle + "/held"
	if err := os.MkdirAll(held, 0o700); err != nil {
		m.t.Fatal(err)
	}
	if record == "" {
		return
	}
	if err := os.WriteFile(held+"/held.json", []byte(record), 0o600); err != nil {
		m.t.Fatal(err)
	}
}

// withLeftoverDesired puts a live session named name on ccy's tmux that holds no seat, under
// which an earlier session of the same name left a desired membership naming the team's seat
// seatPart (launch writes it and nothing removes it; tmux reuses names).
func (m *machine) withLeftoverDesired(name, seatPart string) {
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
	dir := m.seatStore().Pool() + "/" + v2Checkout + "/" + teamtest.V2Team + "/" + handle
	if err := os.MkdirAll(dir, 0o700); err != nil {
		m.t.Fatal(err)
	}
}

// TC2, TC3: a v2 host seat, a live ccy session that holds the team's seat in the seat store, is
// closed on ccy's tmux as any host seat is: /compact, then /exit.
func TestAV2HostSeatsSessionIsClosed(t *testing.T) {
	m := newMachine(t)
	m.withV2(lxcstate.StatusJoined, "", teamtest.V2Member(v2HostHandle, "podman", false))
	m.withContainers(true)
	m.withV2Session()
	m.withV2HostSession("ccy-v2host", v2HostHandle)
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

// TC2: a desired membership is not a holding. A v2 session's desired.json outlives it, and tmux
// reuses names, so a later session under the same name that holds no seat of the team (here a
// session of no bus) is never typed into: the member's free pool seat has no session.
func TestALeftoverDesiredMembershipDoesNotMakeASessionTheTeams(t *testing.T) {
	m := newMachine(t)
	m.withV2(lxcstate.StatusJoined, "", teamtest.V2Member(v2HostHandle, "podman", false))
	m.withContainers(false)
	m.withV2PoolSeat(v2HostHandle)
	m.withLeftoverDesired("ccy-reused", "9")
	if got := m.run(teamtest.V2Team, "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if got := m.typedInto("ccy-reused"); len(got) != 0 {
		t.Fatalf("a session that holds no seat of the team was typed into: %q", got)
	}
	m.errHas(t, v2HostHandle+": "+whyNoSession+" on this host, nothing to shut down")
	m.errLacks(t, "ccy-reused")
}

// TC12: a v2 host seat held by a session that is not on this host's ccy tmux (one started outside
// tmux files its membership under its container name) is out of reach, never "no session": the
// run names who holds it and exits 1, as v1 does for a held seat.
func TestAV2HostSeatHeldOutsideCcyTmuxIsOutOfReach(t *testing.T) {
	m := newMachine(t)
	m.withV2(lxcstate.StatusJoined, "", teamtest.V2Member(v2HostHandle, "podman", false))
	m.withContainers(false)
	m.withV2Holding(v2HostHandle, `{"v":1,"session":"ccy-container-a","claim":"0123456789abcdef","since_ms":1760000000000}`)
	if got := m.run(teamtest.V2Team, "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, v2HostHandle+": not shut down: held by session ccy-container-a, "+whyV2HeldOffTmux)
	m.errLacks(t, whyNoSession)
}

// TC12: a holding whose record cannot be read names no session, so its seat is out of reach:
// reported once, as not reachable, never also as "no session", and the run exits 1.
func TestAnUnreadableHoldingIsNotReachableOnce(t *testing.T) {
	m := newMachine(t)
	m.withV2(lxcstate.StatusJoined, "", teamtest.V2Member(v2HostHandle, "podman", false))
	m.withContainers(false)
	m.withV2Holding(v2HostHandle, "{not json")
	if got := m.run(teamtest.V2Team, "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, v2HostHandle+": not shut down: "+whyV2HeldUnread)
	if n := strings.Count(m.ErrOut.String(), v2HostHandle+":"); n != 1 {
		t.Fatalf("the member is reported %d times:\n%s", n, m.ErrOut.String())
	}
	m.errLacks(t, whyNoSession)
}

// TC12: a seat store whose holdings cannot be read decides nothing: every member it would have
// decided is reported once, as not reachable with why, and the run exits 1.
func TestUnreadableHoldingsAreNotReachableOnce(t *testing.T) {
	m := newMachine(t)
	m.withV2(lxcstate.StatusJoined, "", teamtest.V2Member(v2HostHandle, "podman", false))
	m.withContainers(false)
	m.withV2PoolSeat(v2HostHandle)
	if err := os.MkdirAll(m.seatStore().Pool()+"/"+v2Checkout+"/Not A Team", 0o700); err != nil {
		t.Fatal(err)
	}
	if got := m.run(teamtest.V2Team, "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, v2HostHandle+": not shut down: "+whyUnreachable+"the seat store's holdings: ")
	if n := strings.Count(m.ErrOut.String(), v2HostHandle+":"); n != 1 {
		t.Fatalf("the member is reported %d times:\n%s", n, m.ErrOut.String())
	}
}
