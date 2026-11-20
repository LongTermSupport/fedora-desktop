package shutdown

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

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
// handle in the owner's seat store (its pool seat's held/held.json names the session) and whose
// keeper holds the session's seat lock of the team.
func (m *machine) withV2HostSession(name, handle string) {
	m.t.Helper()
	m.withV2Holding(handle, heldBy(name))
	m.keepSeatLock(name)
	m.sessions = append(m.sessions, &session{name: name, dir: "/srv/proj", pane: paneReady})
}

// heldBy is a complete held.json naming the session.
func heldBy(session string) string {
	return `{"v":1,"session":"` + session + `","claim":"0123456789abcdef","since_ms":1760000000000}`
}

// keepSeatLock holds the session's seat lock of the v2 team as its live keeper does, until the
// test ends.
func (m *machine) keepSeatLock(session string) {
	m.t.Helper()
	n, err := paths.ParseSessionName(session)
	if err != nil {
		m.t.Fatal(err)
	}
	seat := m.seatStore().SessionSeat(n, v2Team(m.t))
	if err = os.MkdirAll(seat.Dir(), 0o700); err != nil {
		m.t.Fatal(err)
	}
	f, err := os.OpenFile(seat.Lock(), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		m.t.Fatal(err)
	}
	m.t.Cleanup(func() {
		if err := f.Close(); err != nil {
			m.t.Error(err)
		}
	})
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		m.t.Fatal(err)
	}
}

// withCrashedHolding gives the pool seat handle a complete holding naming session whose run
// ended without releasing it: its seat lock is there and free, as a crash or a reboot leaves it.
func (m *machine) withCrashedHolding(handle, session string) {
	m.t.Helper()
	m.withV2Holding(handle, heldBy(session))
	n, err := paths.ParseSessionName(session)
	if err != nil {
		m.t.Fatal(err)
	}
	seat := m.seatStore().SessionSeat(n, v2Team(m.t))
	if err = os.MkdirAll(seat.Dir(), 0o700); err != nil {
		m.t.Fatal(err)
	}
	if err = os.WriteFile(seat.Lock(), nil, 0o600); err != nil {
		m.t.Fatal(err)
	}
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

// withV2PoolSeat puts the v2 member v2HostHandle in this host's seat store pool (a checkout's
// seat of the team), held by no session.
func (m *machine) withV2PoolSeat() {
	m.t.Helper()
	dir := m.seatStore().Pool() + "/" + v2Checkout + "/" + teamtest.V2Team + "/" + v2HostHandle
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
	m.withV2PoolSeat()
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
	m.withV2PoolSeat()
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

// TC12: a v2 host seat held by a live session (its keeper holds its seat lock) that is not on
// this host's ccy tmux (one started outside tmux files its membership under its container name)
// is out of reach, never "no session": the run names who holds it and exits 1, as v1 does for a
// held seat.
func TestAV2HostSeatHeldOutsideCcyTmuxIsOutOfReach(t *testing.T) {
	m := newMachine(t)
	m.withV2(lxcstate.StatusJoined, "", teamtest.V2Member(v2HostHandle, "podman", false))
	m.withContainers(false)
	m.withV2Holding(v2HostHandle, heldBy("ccy-container-a"))
	m.keepSeatLock("ccy-container-a")
	if got := m.run(teamtest.V2Team, "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, v2HostHandle+": not shut down: held by session ccy-container-a, "+whyV2HeldOffTmux)
	m.errLacks(t, whyNoSession)
}

// TC2, TC12: a holding outlives a crashed or rebooted session (the seat store keeps it for a
// restore), and tmux reuses names. A later ccy session under the holder's name, whose seat lock
// of the team is free, is not the holder: it is never typed into, and the seat is named as a
// crashed holder's, with nothing to shut down.
func TestALeftoverHoldingDoesNotMakeAReusedNameTheTeams(t *testing.T) {
	m := newMachine(t)
	m.withV2(lxcstate.StatusJoined, "", teamtest.V2Member(v2HostHandle, "podman", false))
	m.withContainers(false)
	m.withCrashedHolding(v2HostHandle, "ccy-reused")
	m.sessions = append(m.sessions, &session{name: "ccy-reused", dir: "/srv/proj", pane: paneReady})
	if got := m.run(teamtest.V2Team, "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if got := m.typedInto("ccy-reused"); len(got) != 0 {
		t.Fatalf("a later session under a crashed holder's name was typed into: %q", got)
	}
	m.errHas(t, v2HostHandle+": "+whyV2Ended("ccy-reused", true)+", nothing to shut down")
	m.errLacks(t, "not reachable")
}

// TC4, TC12: a crashed holder (its holding kept, its seat lock free) with no live session under
// its name has no session running, never "a session outside tmux": named with what the seat
// store does with the holding, and the run exits 0.
func TestACrashedHoldersSeatHasNoSession(t *testing.T) {
	m := newMachine(t)
	m.withV2(lxcstate.StatusJoined, "", teamtest.V2Member(v2HostHandle, "podman", false))
	m.withContainers(false)
	m.withCrashedHolding(v2HostHandle, "ccy-gone")
	if got := m.run(teamtest.V2Team, "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t, v2HostHandle+": "+whyV2Ended("ccy-gone", false)+", nothing to shut down")
	m.errLacks(t, whyV2HeldOffTmux, "not reachable")
}

// TC12: a pool entry that is not a checkout's pool is never skipped: the seat store's holdings
// are unreadable, so each member is named once as not reachable, and the run exits 1.
func TestAPoolEntryThatIsNotACheckoutIsNotSkipped(t *testing.T) {
	for name, plant := range map[string]func(string) error{
		"a name no checkout has": func(pool string) error { return os.MkdirAll(pool+"/Not-A-Checkout", 0o700) },
		"a file":                 func(pool string) error { return os.WriteFile(pool+"/"+strings.Repeat("b", 32), nil, 0o600) },
	} {
		t.Run(name, func(t *testing.T) {
			m := newMachine(t)
			m.withV2(lxcstate.StatusJoined, "", teamtest.V2Member(v2HostHandle, "podman", false))
			m.withContainers(false)
			m.withV2PoolSeat()
			if err := plant(m.seatStore().Pool()); err != nil {
				t.Fatal(err)
			}
			if got := m.run(teamtest.V2Team, "--yes"); got != term.ExitFailure {
				t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
			}
			m.errHas(t, v2HostHandle+": not shut down: "+whyUnreachable+"the seat store's holdings: ", "not a checkout's pool")
			if n := strings.Count(m.ErrOut.String(), v2HostHandle+":"); n != 1 {
				t.Fatalf("the member is reported %d times:\n%s", n, m.ErrOut.String())
			}
		})
	}
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
	m.withV2PoolSeat()
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
