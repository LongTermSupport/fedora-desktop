package shutdown

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	v2HostHandle  = "proj.9+local.podman"
	v2OtherHandle = "proj.8+local.podman"
)

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

// withV2HostSession puts a live session named ccy-v2host on ccy's tmux that holds the v2 team's
// seat v2HostHandle in the owner's seat store (its pool seat's held/held.json names the
// session) and whose keeper holds the session's seat lock of the team; release frees that
// lock, as the keeper's end does.
func (m *machine) withV2HostSession() (release func()) {
	m.t.Helper()
	const name = "ccy-v2host"
	m.withV2Holding(v2HostHandle, heldBy(name))
	release = m.keepSeatLock(name)
	m.sessions = append(m.sessions, &session{name: name, dir: "/srv/proj", pane: paneReady})
	return release
}

// heldBy is a complete held.json naming the session.
func heldBy(session string) string {
	return `{"v":1,"session":"` + session + `","claim":"0123456789abcdef","since_ms":1760000000000}`
}

// keepSeatLock holds the session's seat lock of the v2 team as its live keeper does, until
// release or the test's end.
func (m *machine) keepSeatLock(session string) (release func()) {
	m.t.Helper()
	f, err := os.OpenFile(m.sessionSeatLock(session), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		m.t.Fatal(err)
	}
	var once sync.Once
	release = func() {
		once.Do(func() {
			if err := f.Close(); err != nil {
				m.t.Error(err)
			}
		})
	}
	m.t.Cleanup(release)
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		m.t.Fatal(err)
	}
	return release
}

// sessionSeatLock is the path of the session's seat lock of the v2 team, its directory made.
func (m *machine) sessionSeatLock(session string) string {
	m.t.Helper()
	n, err := paths.ParseSessionName(session)
	if err != nil {
		m.t.Fatal(err)
	}
	seat := m.seatStore().SessionSeat(n, v2Team(m.t))
	if err = os.MkdirAll(seat.Dir(), 0o700); err != nil {
		m.t.Fatal(err)
	}
	return seat.Lock()
}

// withCrashedHolding gives the pool seat handle a complete holding naming session whose run
// ended without releasing it: its seat lock is there and free, as a crash or a reboot leaves it.
func (m *machine) withCrashedHolding(handle, session string) {
	m.t.Helper()
	m.withV2Holding(handle, heldBy(session))
	if err := os.WriteFile(m.sessionSeatLock(session), nil, 0o600); err != nil {
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
	m.withV2HostSession()
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

// TC2, TC12: a holding outlives a crashed or rebooted session (the seat store keeps it), and
// tmux reuses names. A live ccy session under the holder's name, whose seat lock of the team is
// free, may be a later, unrelated session or the holder restarting: which cannot be told, so it
// is never typed into, and the seat is not reachable, exit 1.
func TestALeftoverHoldingDoesNotMakeAReusedNameTheTeams(t *testing.T) {
	m := newMachine(t)
	m.withV2(lxcstate.StatusJoined, "", teamtest.V2Member(v2HostHandle, "podman", false))
	m.withContainers(false)
	m.withCrashedHolding(v2HostHandle, "ccy-reused")
	m.sessions = append(m.sessions, &session{name: "ccy-reused", dir: "/srv/proj", pane: paneReady})
	if got := m.run(teamtest.V2Team, "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if got := m.typedInto("ccy-reused"); len(got) != 0 {
		t.Fatalf("a later session under a crashed holder's name was typed into: %q", got)
	}
	m.errHas(t, v2HostHandle+": not shut down: held by session ccy-reused, whose seat lock is free, while a live "+
		"ccy session carries that name")
	m.errLacks(t, whyNoSession, "not the team's")
}

// TC4, TC12: a crashed holder (its holding kept, its seat lock free) with no live session under
// its name has no session running, never "a session outside tmux": named with the holding's
// session and that the seat stays held until the seat sweep releases it, and the run exits 0.
func TestACrashedHoldersSeatHasNoSession(t *testing.T) {
	m := newMachine(t)
	m.withV2(lxcstate.StatusJoined, "", teamtest.V2Member(v2HostHandle, "podman", false))
	m.withContainers(false)
	m.withCrashedHolding(v2HostHandle, "ccy-gone")
	if got := m.run(teamtest.V2Team, "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	m.errHas(t,
		v2HostHandle+": "+whyNoSession+" on this host: the seat's holding names session ccy-gone, whose seat lock is free",
		"the seat stays held in this host's seat store until the seat sweep releases it",
		", nothing to shut down")
	m.errLacks(t, whyV2HeldOffTmux, "not reachable", "keeps the seat for")
}

// TC12: a v2 launch writes the holding before its keeper makes the seat lock, so a holding with
// no seat lock yet may be a session still starting, the team's own: whether there is a session
// or not, nothing is typed and the seat is not reachable, exit 1, never "no session running".
func TestAHolderWithNoSeatLockYetIsNotReachable(t *testing.T) {
	for name, live := range map[string]bool{"a live session of that name": true, "no session of that name": false} {
		t.Run(name, func(t *testing.T) {
			m := newMachine(t)
			m.withV2(lxcstate.StatusJoined, "", teamtest.V2Member(v2HostHandle, "podman", false))
			m.withContainers(false)
			m.withV2Holding(v2HostHandle, heldBy("ccy-starting"))
			if live {
				m.sessions = append(m.sessions, &session{name: "ccy-starting", dir: "/srv/proj", pane: paneStart})
			}
			if got := m.run(teamtest.V2Team, "--yes"); got != term.ExitFailure {
				t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
			}
			if got := m.typedInto("ccy-starting"); len(got) != 0 {
				t.Fatalf("typed into %q", got)
			}
			m.errHas(t, v2HostHandle+": not shut down: held by session ccy-starting, whose seat lock is not there yet "+
				"(still starting?)")
			m.errLacks(t, whyNoSession, "crash")
		})
	}
}

// TC12: a seat lock the probe refuses (a link, or a mode or owner its container set) says
// nothing of its keeper: the seat is not reachable, exit 1, and nothing is typed.
func TestAHolderWhoseSeatLockIsRefusedIsNotReachable(t *testing.T) {
	m := newMachine(t)
	m.withV2(lxcstate.StatusJoined, "", teamtest.V2Member(v2HostHandle, "podman", false))
	m.withContainers(false)
	m.withV2Holding(v2HostHandle, heldBy("ccy-v2host"))
	if err := os.Symlink("/dev/null", m.sessionSeatLock("ccy-v2host")); err != nil {
		t.Fatal(err)
	}
	m.sessions = append(m.sessions, &session{name: "ccy-v2host", dir: "/srv/proj", pane: paneReady})
	if got := m.run(teamtest.V2Team, "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if got := m.typedInto("ccy-v2host"); len(got) != 0 {
		t.Fatalf("typed into %q", got)
	}
	m.errHas(t, v2HostHandle+": not shut down: held by session ccy-v2host, whose seat lock cannot be probed")
	m.errLacks(t, whyNoSession)
}

// TC12: seat locks that cannot be read decide nothing: every host member left is named once as
// not reachable, exit 1, and nothing is typed.
func TestUnreadableSeatLocksAreNotReachableOnce(t *testing.T) {
	m := newMachine(t)
	m.withV2(lxcstate.StatusJoined, "", teamtest.V2Member(v2HostHandle, "podman", false))
	m.withContainers(false)
	m.withV2HostSession()
	m.s.Keeper = func(paths.Store, paths.SessionName, paths.TeamID) (seat.Keeper, error) {
		return seat.KeeperNoLock, errors.New("flock: no locks available")
	}
	if got := m.run(teamtest.V2Team, "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if got := m.typedInto("ccy-v2host"); len(got) != 0 {
		t.Fatalf("typed into %q", got)
	}
	m.errHas(t, v2HostHandle+": not shut down: "+whyUnreachable+"the seat store's seat locks: ", "no locks available")
	if n := strings.Count(m.ErrOut.String(), v2HostHandle+":"); n != 1 {
		t.Fatalf("the member is reported %d times:\n%s", n, m.ErrOut.String())
	}
}

// promptThen is a prompt runner that runs then before it answers, as a holder that ends while
// the question waits.
type promptThen struct {
	execx.Runner
	then func()
}

func (p promptThen) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) {
	p.then()
	return p.Runner.Run(ctx, c)
}

// TC2, TC12: the live keeper is asked again just before the close. A holder whose seat lock is
// freed while the question waits is not typed into: it is not reachable, changed while
// confirming, and the run exits 1.
func TestAHolderThatEndsWhileConfirmingIsNotTypedInto(t *testing.T) {
	m := newMachine(t)
	m.withV2(lxcstate.StatusJoined, "", teamtest.V2Member(v2HostHandle, "podman", false))
	m.withContainers(false)
	release := m.withV2HostSession()
	c := m.interactive("|YES")
	m.Host.Picker.Runner = promptThen{Runner: c.Script, then: release}
	if got := m.run(teamtest.V2Team); got != term.ExitFailure {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if got := m.typedInto("ccy-v2host"); len(got) != 0 {
		t.Fatalf("a holder whose seat lock was freed while confirming was typed into: %q", got)
	}
	m.errHas(t, v2HostHandle+": not shut down: held by session ccy-v2host, whose seat lock changed while confirming: "+
		"it is free now")
}

// TC12: a pool entry that is not a checkout's pool is never skipped: it is named once as not
// reachable, and the run exits 1; the seat sweep reports it the same way and goes on, so the
// valid checkouts are still read: a live holder there is closed and a free seat has no session.
func TestAPoolEntryThatIsNotACheckoutIsNotSkipped(t *testing.T) {
	for name, plant := range map[string]func(string) error{
		"a name no checkout has": func(pool string) error { return os.MkdirAll(pool+"/Not-A-Checkout", 0o700) },
		"a file":                 func(pool string) error { return os.WriteFile(pool+"/"+strings.Repeat("b", 32), nil, 0o600) },
	} {
		t.Run(name, func(t *testing.T) {
			m := newMachine(t)
			m.withV2(lxcstate.StatusJoined, "", teamtest.V2Member(v2HostHandle, "podman", false),
				teamtest.V2Member(v2OtherHandle, "podman", false))
			m.withContainers(false)
			m.withV2HostSession()
			if err := os.MkdirAll(m.seatStore().Pool()+"/"+v2Checkout+"/"+teamtest.V2Team+"/"+v2OtherHandle, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := plant(m.seatStore().Pool()); err != nil {
				t.Fatal(err)
			}
			if got := m.run(teamtest.V2Team, "--yes"); got != term.ExitFailure {
				t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
			}
			if got := m.typedInto("ccy-v2host"); len(got) != 2 || got[1] != "/exit" {
				t.Fatalf("the live holder in a valid checkout was not closed: %q", got)
			}
			m.errHas(t, "ccy-v2host: exited cleanly", "not a checkout's pool",
				v2OtherHandle+": "+whyNoSession+" on this host, nothing to shut down")
			if n := strings.Count(m.ErrOut.String(), "not a checkout's pool"); n != 2 {
				t.Fatalf("the bad entry is not named once in the preview and once in the result:\n%s", m.ErrOut.String())
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

// TC12: every state a seat lock can be in has its own words, both where a holder is named not
// reachable and where a recheck finds its lock changed.
func TestEverySeatLockStateHasItsOwnWords(t *testing.T) {
	keepers := []seat.Keeper{seat.KeeperNoLock, seat.KeeperEnded, seat.KeeperRunning, seat.KeeperRefused}
	for name, words := range map[string]func(seat.Keeper) string{"whyV2NotLive": whyV2NotLive, "keeperNow": keeperNow} {
		seen := map[string]seat.Keeper{}
		for _, k := range keepers {
			w := words(k)
			if prev, dup := seen[w]; dup || w == "" {
				t.Errorf("%s(%v) = %q, the same as for %v", name, k, w, prev)
			}
			seen[w] = k
		}
	}
}

// TC12: a seat lock that cannot be read when it is asked again just before the close is not
// typed into: the holder is not reachable, with why.
func TestARecheckThatCannotReadTheLockClosesNothing(t *testing.T) {
	m := newMachine(t)
	m.withV2(lxcstate.StatusJoined, "", teamtest.V2Member(v2HostHandle, "podman", false))
	m.withContainers(false)
	m.withV2HostSession()
	asked := 0
	m.s.Keeper = func(store paths.Store, n paths.SessionName, team paths.TeamID) (seat.Keeper, error) {
		if asked++; asked > 1 {
			return seat.KeeperNoLock, errors.New("flock: no locks available")
		}
		return seat.KeeperOf(store, n, team)
	}
	if got := m.run(teamtest.V2Team, "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if got := m.typedInto("ccy-v2host"); len(got) != 0 {
		t.Fatalf("typed into %q", got)
	}
	m.errHas(t, v2HostHandle+": not shut down: held by session ccy-v2host, whose seat lock changed while confirming: "+
		"it cannot be read now: flock: no locks available")
}

// TC12: a seat store that cannot be found, or whose pool cannot be listed, decides nothing:
// each host member is named once as not reachable, with why, and the run exits 1.
func TestASeatStoreThatCannotBeReadDecidesNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		plant func(m *machine)
		why   string
	}{
		"no seat store": {func(m *machine) {
			m.s.SeatStore = func() (paths.Store, error) { return paths.Store{}, errors.New("no state home") }
		}, "the seat store: no state home"},
		"a pool that is a file": {func(m *machine) {
			pool := m.seatStore().Pool()
			if err := os.MkdirAll(filepath.Dir(pool), 0o700); err != nil {
				m.t.Fatal(err)
			}
			if err := os.WriteFile(pool, nil, 0o600); err != nil {
				m.t.Fatal(err)
			}
		}, "the seat store's pool: "},
	} {
		t.Run(name, func(t *testing.T) {
			m := newMachine(t)
			m.withV2(lxcstate.StatusJoined, "", teamtest.V2Member(v2HostHandle, "podman", false))
			m.withContainers(false)
			tc.plant(m)
			if got := m.run(teamtest.V2Team, "--yes"); got != term.ExitFailure {
				t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
			}
			m.errHas(t, v2HostHandle+": not shut down: "+whyUnreachable+tc.why)
			if n := strings.Count(m.ErrOut.String(), v2HostHandle+":"); n != 1 {
				t.Fatalf("the member is reported %d times:\n%s", n, m.ErrOut.String())
			}
		})
	}
}
