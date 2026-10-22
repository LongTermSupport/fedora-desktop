package paths

import (
	"errors"
	"strings"
	"testing"
)

// memberNames is every name of member.go (SPEC-n3 PA1), for the sample seat and both layouts;
// sampleNames includes it, so the v1 and derivation checks cover these names too.
func memberNames(t *testing.T) map[string][]string {
	t.Helper()
	ops, key := mustTeam(t, "ops"), sampleSeatKey(t)
	socket, err := InboxSocket("/run/user/1000", 4242)
	if err != nil {
		t.Fatal(err)
	}
	login, err := GuestLoginRunRoot(1001)
	if err != nil {
		t.Fatal(err)
	}
	home, err := HomeLayout("/home/u", "/run/user/1000")
	if err != nil {
		t.Fatal(err)
	}
	aside, err := StateSeat("/home/u/.local/state/teamchat/seats/sessions/s1", ops)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string][]string{
		"StateSeat":          {aside.Dir()},
		"FloorAccountData":   {FloorAccountData},
		"InboxSocket":        {socket},
		"GuestUnitFile":      {GuestUnitFile(ops, sampleCheckout, 1)},
		"GuestUnitWantsLink": {GuestUnitWantsLink(ops, sampleCheckout, 1)},
		"GuestTmuxSession":   {GuestTmuxSession(ops, sampleCheckout, 1)},
		"GuestTmuxSocket":    {GuestTmuxSocket(key)},
		"GuestLoginRunRoot":  {login},
		"GuestUnitRunRoot":   {GuestUnitRunRoot(key)},
	}
	for _, l := range []Layout{CCYLayout(), home} {
		names["Seat.StoreAside"] = append(names["Seat.StoreAside"], l.Seat(ops).StoreAside(1760000000000))
		names["Seat.Password"] = append(names["Seat.Password"], l.Seat(ops).Password())
	}
	return names
}

// memberZeroDerivations is member.go's functions of a zero team or seat key, which
// zeroDerivations includes.
func memberZeroDerivations() map[string]func() string {
	var (
		team TeamID
		key  SeatKey
	)
	return map[string]func() string{
		"StateSeat": func() string {
			s, err := StateSeat("/home/u", TeamID{})
			if err != nil {
				return err.Error()
			}
			return s.Dir()
		},
		"GuestUnitFile":      func() string { return GuestUnitFile(team, sampleCheckout, 1) },
		"GuestUnitWantsLink": func() string { return GuestUnitWantsLink(team, sampleCheckout, 1) },
		"GuestTmuxSession":   func() string { return GuestTmuxSession(team, sampleCheckout, 1) },
		"GuestTmuxSocket":    func() string { return GuestTmuxSocket(key) },
		"GuestUnitRunRoot":   func() string { return GuestUnitRunRoot(key) },
	}
}

// PA1: the member names n3a adds, per seat since n3b-D2.
func TestMemberNames(t *testing.T) {
	ops, key := mustTeam(t, "ops"), sampleSeatKey(t)
	socket, err := InboxSocket("/run/user/1000", 4242)
	if err != nil {
		t.Fatal(err)
	}
	login, err := GuestLoginRunRoot(1001)
	if err != nil {
		t.Fatal(err)
	}
	aside, err := StateSeat("/srv/seats", ops)
	if err != nil {
		t.Fatal(err)
	}
	unit := "teamchat-session-ops-thing-1-" + sampleKeyText
	for got, want := range map[string]string{
		aside.Store():                         "/srv/seats/ops/store.json",
		FloorAccountData:                      "teamchat.floor",
		socket:                                "/run/user/1000/cc-socks/4242.sock",
		GuestUnitFile(ops, sampleCheckout, 1): "/etc/systemd/system/" + unit + ".service",
		GuestUnitWantsLink(ops, sampleCheckout, 1):      "/etc/systemd/system/multi-user.target.wants/" + unit + ".service",
		GuestTmuxSession(ops, sampleCheckout, 1):        unit,
		GuestTmuxSocket(key):                            "/run/teamchat-session-" + sampleKeyText + "/tmux.sock",
		login:                                           "/run/user/1001/teamchat/run",
		GuestUnitRunRoot(key):                           "/run/teamchat-session-" + sampleKeyText + "/teamchat/run",
		CCYLayout().Seat(ops).StoreAside(1760000000000): "/run/teamchat/seats/ops/store.json.aside-1760000000000",
		CCYLayout().Seat(ops).Password():                "/run/teamchat/seats/ops/password",
	} {
		if got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
}

// The unit's run root is the one LXCLayout gives the unit's XDG_RUNTIME_DIR, and the login run
// root the one a hand-run seat's login session gives, so the kit collector reads where the
// session wrote (n3-D8, LX5).
func TestTheGuestRunRootsAreTheLayoutsRunRoots(t *testing.T) {
	key := sampleSeatKey(t)
	unit, err := LXCLayout("/home/u", GuestRuntimeDir(key), key)
	if err != nil {
		t.Fatal(err)
	}
	hand, err := LXCLayout("/home/u", "/run/user/1001", key)
	if err != nil {
		t.Fatal(err)
	}
	login, err := GuestLoginRunRoot(1001)
	if err != nil || login != hand.RunRoot() || GuestUnitRunRoot(key) != unit.RunRoot() {
		t.Fatalf("run roots %s and %s, want %s and %s (%v)", login, GuestUnitRunRoot(key), hand.RunRoot(), unit.RunRoot(), err)
	}
}

// C 3.9.1: the unit name at its longest (a 22-character team, a 24-character repo label, seat
// 9999) is 94 bytes, and its tmux session holds no `.` or `:`.
func TestTheLongestSessionUnit(t *testing.T) {
	team := mustTeam(t, "a"+strings.Repeat("b", 21))
	checkout := "/srv/" + strings.Repeat("r", 40)
	unit := SessionUnit(team, checkout, 9999)
	if len(unit) != 94 {
		t.Fatalf("the longest unit %q is %d bytes, want 94", unit, len(unit))
	}
	if session := GuestTmuxSession(team, checkout, 9999); strings.ContainsAny(session, ".:") || len(session) != 86 {
		t.Fatalf("the longest tmux session %q", session)
	}
}

// C 3.9.1: the runtime directory is the key alone, so B's sockets below it stay within
// sun_path's 107 bytes.
func TestTheGuestRuntimeDirectoryKeepsBsSocketsShort(t *testing.T) {
	key := sampleSeatKey(t)
	l, err := LXCLayout("/home/u", GuestRuntimeDir(key), key)
	if err != nil {
		t.Fatal(err)
	}
	inbox, err := InboxSocket(GuestRuntimeDir(key), 4194304)
	if err != nil {
		t.Fatal(err)
	}
	link := l.Run(mustRun(t)).LinkSock()
	for _, socket := range []string{link, inbox, GuestTmuxSocket(key)} {
		if len(socket) > 107 {
			t.Errorf("%s is %d bytes, over sun_path's 107", socket, len(socket))
		}
	}
	if len(link) != 78 || len(inbox) != 60 {
		t.Fatalf("link %d and inbox %d bytes, want C 3.9.1's 78 and 60", len(link), len(inbox))
	}
}

// The kit scan finds a unit's runtime directory in the guest's /run by this grammar alone.
func TestParseGuestRuntimeDirName(t *testing.T) {
	key := sampleSeatKey(t)
	name := strings.TrimPrefix(GuestRuntimeDir(key), "/run/")
	if got, err := ParseGuestRuntimeDirName(name); err != nil || got != key {
		t.Fatalf("ParseGuestRuntimeDirName(%q) = %q, %v", name, got, err)
	}
	for _, bad := range []string{
		"", "teamchat-session-", "teamchat-session-dev", "teamchat-session-" + strings.ToUpper(sampleKeyText),
		"teamchat-session-" + sampleKeyText + "0", "teamchat-" + sampleKeyText, "user", "x" + name,
	} {
		if _, err := ParseGuestRuntimeDirName(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseGuestRuntimeDirName(%q) = %v, want ErrInvalid", bad, err)
		}
	}
}

func TestMemberNamesRefuseBadInput(t *testing.T) {
	for _, c := range []struct {
		dir string
		pid int
	}{{"run/user/1000", 1}, {"/run/user/1000/", 1}, {"/", 1}, {"/run/user/1000", 0}, {"/run/user/1000", -3}} {
		if _, err := InboxSocket(c.dir, c.pid); !errors.Is(err, ErrInvalid) {
			t.Errorf("InboxSocket(%q, %d) = %v, want ErrInvalid", c.dir, c.pid, err)
		}
	}
	for _, uid := range []int{0, -1} {
		if _, err := GuestLoginRunRoot(uid); !errors.Is(err, ErrInvalid) {
			t.Errorf("GuestLoginRunRoot(%d) = %v, want ErrInvalid", uid, err)
		}
	}
	for _, dir := range []string{"seats", "/seats/", "/a/../seats", "/"} {
		if _, err := StateSeat(dir, mustTeam(t, "ops")); !errors.Is(err, ErrInvalid) {
			t.Errorf("StateSeat(%q) = %v, want ErrInvalid", dir, err)
		}
	}
	for _, ms := range []int64{0, -1} {
		if did, got := panics(func() string { return CCYLayout().Seat(mustTeam(t, "ops")).StoreAside(ms) }); !did {
			t.Errorf("StoreAside(%d) = %q, want a panic", ms, got)
		}
	}
	if did, got := panics(func() string { return SessionUnit(mustTeam(t, "ops"), "relative", 1) }); !did {
		t.Errorf("SessionUnit of a relative checkout = %q, want a panic", got)
	}
}
