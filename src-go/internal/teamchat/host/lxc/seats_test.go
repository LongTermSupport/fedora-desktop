package lxc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxccmd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// secondHandle is the fixture checkout's seat 2 handle.
const secondHandle = "repo.2+local.lxc"

func agent2(t *testing.T) paths.GuestUser {
	t.Helper()
	u, err := paths.ParseGuestUser("agent2")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// entryAt is team t1's record entry of seat in c.
func (w *world) entryAt(c string, seat int) (lxcstate.Entry, bool) {
	w.t.Helper()
	return w.record().Find(mustBox(w.t, c), checkout, seat)
}

// callsAs is each guest op run as user, as "<container> <verb>".
func (f *fakeLXC) callsAs(user string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if c.User == user {
			out = append(out, c.Container+" "+c.Verb)
		}
	}
	return out
}

func TestTwoSeatsOfOneUserInOneContainer(t *testing.T) {
	w := newWorld(t)
	b := w.addBox("c1")
	w.declare([]string{"t1"}, w.member("t1", "c1"), w.memberAt("t1", "c1", 2))
	r, err := w.apply(PhasePresent)
	if err != nil {
		t.Fatal(err)
	}
	hasLine(t, r, lineOf(t, "CHANGED", "c1", "joined as ")+firstHandle)
	hasLine(t, r, lineAt(t, "CHANGED", "t1", "agent1", "c1", 2, "joined as ")+secondHandle)
	if !slices.Equal(w.roster.joins, []string{"member.add " + firstHandle, "member.add " + secondHandle}) {
		t.Fatalf("joins %v", w.roster.joins)
	}
	sessions := map[string]bool{}
	for seat := 1; seat <= 2; seat++ {
		id := seatAt(t, "t1", seat)
		cp, found, err := lxcstate.ReadCopy(w.host, mustBox(t, "c1"), id)
		if err != nil || !found || len(cp.Seats) != 1 || cp.Seats[0].Seat != strconv.Itoa(seat) {
			t.Fatalf("seat %d's copy %+v %v %v", seat, cp, found, err)
		}
		if _, ok := guestFile(t, b, paths.GuestUnitFile(id.Team, checkout, seat)); !ok {
			t.Fatalf("seat %d has no unit", seat)
		}
		if e, ok := w.entryAt("c1", seat); !ok || e.Status != lxcstate.StatusJoined {
			t.Fatalf("seat %d's entry %+v", seat, e)
		}
		sessions["lxc-"+id.Key().String()] = true
	}
	for _, c := range w.lxc.calls {
		if c.Verb == "guest membership" {
			delete(sessions, strings.Split(strings.Split(c.Stdin, `"session":"`)[1], `"`)[0])
		}
	}
	if len(sessions) != 0 {
		t.Fatalf("seats with no membership of their own: %v", sessions)
	}
	if n := strings.Count(strings.Join(w.lxc.verbs(), "\n"), "guest check"); n != 1 {
		t.Fatalf("guest check ran %d times for one user and team", n)
	}
}

func TestTwoUsersShareACheckout(t *testing.T) {
	w := newWorld(t)
	b := w.addBox("c1")
	env := "srv/agent1/src/repo/.claude/ccy/ccy.env.local"
	writeTree(t, b.dir, map[string]string{env: "HOOKS_DAEMON_HOSTNAME=box\n"})
	if err := os.Chown(filepath.Join(b.dir, env), agent2UID, agentGID); err != nil {
		t.Fatal(err)
	}
	second := w.memberAt("t1", "c1", 2)
	second.User = agent2(t)
	w.declare([]string{"t1"}, w.member("t1", "c1"), second)
	if _, err := w.apply(PhasePresent); err != nil {
		t.Fatal(err)
	}
	for seat, user := range map[int]string{1: "agent1", 2: "agent2"} {
		if e, ok := w.entryAt("c1", seat); !ok || e.Status != lxcstate.StatusJoined || e.User.String() != user {
			t.Fatalf("seat %d's entry %+v", seat, e)
		}
		if !slices.Contains(w.lxc.callsAs(user), "c1 guest place-bundle") {
			t.Fatalf("%s's bundle was not placed as %s: %v", strconv.Itoa(seat), user, w.lxc.callsAs(user))
		}
	}

	w = newWorld(t)
	b = w.addBox("c1")
	writeTree(t, b.dir, map[string]string{env: "HOOKS_DAEMON_HOSTNAME=box\n"})
	if err := os.Chown(filepath.Join(b.dir, env), agent2UID, agentGID); err != nil {
		t.Fatal(err)
	}
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	if _, err := w.apply(PhasePresent); err == nil || !strings.Contains(err.Error(), "not owned by an agent user") {
		t.Fatalf("an env file of a user no entry names was accepted: %v", err)
	}
}

// moved is the placed world with seat 1's entry declared for agent2.
func moved(t *testing.T) *world {
	t.Helper()
	w := placed(t)
	m := w.member("t1", "c1")
	m.User = agent2(t)
	w.declare([]string{"t1"}, m)
	return w
}

func TestUserMoveWaitsForTheOldUser(t *testing.T) {
	w := moved(t)
	r, err := w.apply(PhasePresent)
	if err != nil {
		t.Fatal(err)
	}
	hasLine(t, r, lineAt(t, "DEFERRED", "t1", "agent2", "c1", 1, whyOldUser))
	if e := w.entry("c1"); e.User.String() != "agent1" || e.Status != lxcstate.StatusJoined {
		t.Fatalf("the entry changed before the old user left: %+v", e)
	}
	if len(w.roster.joins) != 0 || len(w.lxc.callsAs("agent2")) != 0 {
		t.Fatalf("the new user was joined %v or attached %v", w.roster.joins, w.lxc.callsAs("agent2"))
	}
}

// movedOut is the moved world after its absent phase.
func movedOut(t *testing.T) *world {
	t.Helper()
	w := moved(t)
	if _, err := w.apply(PhaseAbsent); err != nil {
		t.Fatal(err)
	}
	return w
}

func TestUserMoveLeavesUnderTheOldUser(t *testing.T) {
	w := moved(t)
	r, err := w.apply(PhaseAbsent)
	if err != nil {
		t.Fatal(err)
	}
	hasLine(t, r, lineOf(t, "CHANGED", "c1", lineRemoved+lineMovedTo+"agent2"))
	if !slices.Contains(w.lxc.callsAs("agent1"), "c1 guest remove") {
		t.Fatalf("the old user's files were not removed: %v", w.lxc.callsAs("agent1"))
	}
	e := w.entry("c1")
	if e.User.String() != "agent2" || e.Status != lxcstate.StatusDeferred || e.Handle != firstHandle || e.Why != whyMoved {
		t.Fatalf("entry %+v", e)
	}
	if _, found, err := lxcstate.ReadCopy(w.host, mustBox(t, "c1"), seatID(t, "t1")); err != nil || found {
		t.Fatalf("the old user's copy was kept: %v %v", found, err)
	}
}

func TestUserMoveReturnsUnderTheNewUser(t *testing.T) {
	w := movedOut(t)
	w.roster.joins = nil
	if _, err := w.apply(PhasePresent); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(w.roster.joins, []string{"member.return " + firstHandle}) {
		t.Fatalf("joins %v", w.roster.joins)
	}
	if e := w.entry("c1"); e.User.String() != "agent2" || e.Status != lxcstate.StatusJoined {
		t.Fatalf("entry %+v", e)
	}
	if !slices.Contains(w.lxc.callsAs("agent2"), "c1 guest place-bundle") {
		t.Fatalf("the bundle was not placed as the new user: %v", w.lxc.callsAs("agent2"))
	}
}

func TestUserMoveFromADeletedUser(t *testing.T) {
	w := moved(t)
	b := w.lxc.boxes["c1"]
	writeTree(t, b.dir, map[string]string{"etc/passwd": "root:x:0:0:root:/root:/bin/bash\nagent2:x:1002:1001::/srv/agent2:/bin/bash\n"})
	r, err := w.apply(PhaseAbsent)
	if err != nil {
		t.Fatal(err)
	}
	hasLine(t, r, lineOf(t, "CHANGED", "c1", lineRemoved+lineNoUser+lineMovedTo+"agent2"))
	if len(w.lxc.callsAs("agent1")) != 0 {
		t.Fatalf("attached as a deleted user: %v", w.lxc.callsAs("agent1"))
	}
	if _, ok := guestFile(t, b, unitFileOf(t, "t1")); ok {
		t.Fatal("the deleted user's unit was kept")
	}
	if e := w.entry("c1"); e.User.String() != "agent2" || e.Status != lxcstate.StatusDeferred {
		t.Fatalf("entry %+v", e)
	}
}

func TestUserMoveInAMissingContainer(t *testing.T) {
	w := moved(t)
	delete(w.lxc.boxes, "c1")
	r, err := w.apply(PhaseAbsent)
	if err != nil {
		t.Fatal(err)
	}
	hasLine(t, r, lineOf(t, "CHANGED", "c1", "parked; entry dropped ("+whyMissing+")"+lineMovedTo+"agent2"))
	if e := w.entry("c1"); e.User.String() != "agent2" || e.Status != lxcstate.StatusDeferred || e.Handle != firstHandle {
		t.Fatalf("entry %+v", e)
	}
	if _, found, err := lxcstate.ReadCopy(w.host, mustBox(t, "c1"), seatID(t, "t1")); err != nil || found {
		t.Fatalf("a missing container's copy was kept: %v %v", found, err)
	}
}

func TestRetiredJoinIsDeferred(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	w.roster.joinErr = fmt.Errorf("member.add: %w", apply.ErrRetired)
	r, err := w.apply(PhasePresent)
	if err != nil {
		t.Fatal(err)
	}
	hasLine(t, r, lineOf(t, "DEFERRED", "c1", "seat 1"+whyDeactivated))
	if slices.Contains(w.lxc.verbs(), "c1 guest place-bundle") {
		t.Fatal("a refused join placed a bundle")
	}
	if e := w.entry("c1"); e.Status != lxcstate.StatusDeferred {
		t.Fatalf("entry %+v", e)
	}
}

func TestJoinRecordedBeforeAnotherContainerFails(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.addBox("c2")
	w.declare([]string{"t1"}, w.member("t1", "c1"), w.memberAt("t1", "c2", 2))
	w.lxc.answer = func(c guestCall) error {
		if c.Container == "c2" && c.Verb == "guest place-bundle" {
			return &lxccmd.GuestError{Op: c.Verb, Exit: 1, Code: "io"}
		}
		return nil
	}
	_, err := w.apply(PhasePresent)
	if herr, ok := errors.AsType[*host.Error](err); !ok || herr.Drift {
		t.Fatalf("err %v", err)
	}
	if e, ok := w.entryAt("c1", 1); !ok || e.Status != lxcstate.StatusJoined {
		t.Fatalf("c1's entry %+v", e)
	}
	e, ok := w.entryAt("c2", 2)
	if !ok || e.Handle != secondHandle || e.Status == lxcstate.StatusJoined {
		t.Fatalf("c2's joined handle was not recorded: %+v %v", e, ok)
	}

	w.lxc.answer = nil
	gone := w.memberAt("t1", "c2", 2)
	gone.State = declare.StateAbsent
	w.declare([]string{"t1"}, w.member("t1", "c1"), gone)
	if _, err := w.apply(PhaseAbsent); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(w.roster.parks, []string{"t1 " + secondHandle}) {
		t.Fatalf("parks %v", w.roster.parks)
	}
	if _, ok := w.entryAt("c2", 2); ok {
		t.Fatal("the removed seat kept its entry")
	}
}

func TestEqualHandlesRefused(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.addBox("c2")
	w.declare([]string{"t1"}, w.member("t1", "c1"), w.member("t1", "c2"))
	_, err := w.apply(PhasePresent)
	if err == nil || !strings.Contains(err.Error(), "derive one handle "+firstHandle) {
		t.Fatalf("err %v", err)
	}
	if len(w.roster.joins) != 0 {
		t.Fatalf("joins %v", w.roster.joins)
	}

	w = placed(t)
	w.addBox("c2")
	w.declare([]string{"t1"}, w.member("t1", "c1"), w.member("t1", "c2"))
	w.roster.joins = nil
	_, err = w.apply(PhasePresent)
	if err == nil || !strings.Contains(err.Error(), "its handle "+firstHandle+" is held by agent1@c1/") {
		t.Fatalf("err %v", err)
	}
	if e := w.entry("c1"); e.Status != lxcstate.StatusJoined {
		t.Fatalf("the holder's entry %+v", e)
	}
	if _, ok := w.record().Find(mustBox(t, "c2"), checkout, 1); ok {
		t.Fatal("the refused seat was recorded")
	}
}

func TestKeyOwnersClaim(t *testing.T) {
	c := mustBox(t, "c1")
	one, two := seatID(t, "t1"), seatID(t, "t2")
	o := keyOwners{}
	if err := o.claim(c, one); err != nil {
		t.Fatal(err)
	}
	if err := o.claim(c, one); err != nil {
		t.Fatalf("a seat claimed twice by itself: %v", err)
	}
	if err := o.claim(mustBox(t, "c2"), one); err != nil {
		t.Fatalf("one seat key in two containers: %v", err)
	}
	o[Target{Container: c, Key: two.Key()}] = one
	err := o.claim(c, two)
	if err == nil || !strings.Contains(err.Error(), "have one seat key "+two.Key().String()) {
		t.Fatalf("a planted collision was not refused: %v", err)
	}
}

func TestBadSeatsRefusedBeforeAKeyIsMade(t *testing.T) {
	for name, bad := range map[string]func(m *declare.LXCMember){
		"seat 0":              func(m *declare.LXCMember) { m.Seat = 0 },
		"seat 10000":          func(m *declare.LXCMember) { m.Seat = paths.MaxLXCSeat + 1 },
		"a relative checkout": func(m *declare.LXCMember) { m.Checkout = "src/repo" },
		"the root checkout":   func(m *declare.LXCMember) { m.Checkout = "/" },
		"an unclean checkout": func(m *declare.LXCMember) { m.Checkout = "/srv/agent1/src/../repo" },
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.addBox("c1")
			m := w.member("t1", "c1")
			bad(&m)
			w.declare([]string{"t1"}, m)
			_, err := w.apply(PhasePresent)
			if herr, ok := errors.AsType[*host.Error](err); !ok || herr.Step != stepValidate || !herr.Drift {
				t.Fatalf("err %v", err)
			}

			w.declare([]string{"t1", "t2"}, m, w.member("t2", "c1"))
			if _, err := w.apply(PhasePresent, "t2"); err != nil {
				t.Fatalf("a team outside --only-teams failed the apply by its own bad entry: %v", err)
			}
		})
	}
}

func TestSeatDeclaredTwiceRefused(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.declare([]string{"t1"}, w.member("t1", "c1"), w.member("t1", "c1"))
	_, err := w.apply(PhasePresent)
	if err == nil || !strings.Contains(err.Error(), "declared twice") {
		t.Fatalf("err %v", err)
	}
}
