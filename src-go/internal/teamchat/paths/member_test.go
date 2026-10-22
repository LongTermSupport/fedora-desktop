package paths

import (
	"errors"
	"testing"
)

// memberNames is every name of member.go (SPEC-n3 PA1), for the sample guest user and both
// layouts; sampleNames includes it, so the v1 and derivation checks cover these names too.
func memberNames(t *testing.T) map[string][]string {
	t.Helper()
	user := sampleGuestUser(t)
	socket, err := InboxSocket("/run/user/1000", 4242)
	if err != nil {
		t.Fatal(err)
	}
	parents, err := GuestRuntimeParents(user, 1001)
	if err != nil {
		t.Fatal(err)
	}
	home, err := HomeLayout("/home/u", "/run/user/1000")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string][]string{
		"FloorAccountData":    {FloorAccountData},
		"InboxSocket":         {socket},
		"GuestUnitFile":       {GuestUnitFile(user)},
		"GuestUnitWantsLink":  {GuestUnitWantsLink(user)},
		"GuestRuntimeParents": parents,
	}
	for _, l := range []Layout{CCYLayout(), home} {
		names["Seat.StoreAside"] = append(names["Seat.StoreAside"], l.Seat(mustTeam(t, "ops")).StoreAside(1760000000000))
		names["Seat.Password"] = append(names["Seat.Password"], l.Seat(mustTeam(t, "ops")).Password())
	}
	return names
}

// memberZeroDerivations is member.go's functions of a zero guest user, which zeroDerivations
// includes.
func memberZeroDerivations() map[string]func() string {
	var user GuestUser
	return map[string]func() string{
		"GuestUnitFile":      func() string { return GuestUnitFile(user) },
		"GuestUnitWantsLink": func() string { return GuestUnitWantsLink(user) },
		"GuestRuntimeParents": func() string {
			parents, err := GuestRuntimeParents(user, 1001)
			if err != nil {
				return err.Error()
			}
			return parents[0]
		},
	}
}

// PA1: the member names n3a adds.
func TestMemberNames(t *testing.T) {
	user := sampleGuestUser(t)
	socket, err := InboxSocket("/run/user/1000", 4242)
	if err != nil {
		t.Fatal(err)
	}
	parents, err := GuestRuntimeParents(user, 1001)
	if err != nil || len(parents) != 2 {
		t.Fatalf("GuestRuntimeParents = %v, %v", parents, err)
	}
	for got, want := range map[string]string{
		FloorAccountData:         "teamchat.floor",
		socket:                   "/run/user/1000/cc-socks/4242.sock",
		GuestUnitFile(user):      "/etc/systemd/system/teamchat-session-" + user.String() + ".service",
		GuestUnitWantsLink(user): "/etc/systemd/system/multi-user.target.wants/teamchat-session-" + user.String() + ".service",
		parents[0]:               "/run/user/1001/teamchat/run",
		parents[1]:               "/run/teamchat-session-" + user.String() + "/teamchat/run",
		CCYLayout().Seat(mustTeam(t, "ops")).StoreAside(1760000000000): "/run/teamchat/seats/ops/store.json.aside-1760000000000",
		CCYLayout().Seat(mustTeam(t, "ops")).Password():                "/run/teamchat/seats/ops/password",
	} {
		if got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
}

// The unit's runtime parent is the run root HomeLayout gives the unit's XDG_RUNTIME_DIR, so the
// kit collector reads where the session wrote (n3-D8).
func TestTheUnitRuntimeParentIsTheUnitLayoutsRunRoot(t *testing.T) {
	user := sampleGuestUser(t)
	parents, err := GuestRuntimeParents(user, 1001)
	if err != nil {
		t.Fatal(err)
	}
	unit, err := HomeLayout("/home/u", GuestRuntimeDir(user))
	if err != nil {
		t.Fatal(err)
	}
	login, err := HomeLayout("/home/u", "/run/user/1001")
	if err != nil {
		t.Fatal(err)
	}
	if parents[0] != login.RunRoot() || parents[1] != unit.RunRoot() {
		t.Fatalf("parents %v, want %s and %s", parents, login.RunRoot(), unit.RunRoot())
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
		if _, err := GuestRuntimeParents(sampleGuestUser(t), uid); !errors.Is(err, ErrInvalid) {
			t.Errorf("GuestRuntimeParents(uid %d) = %v, want ErrInvalid", uid, err)
		}
	}
	for _, ms := range []int64{0, -1} {
		if did, got := panics(func() string { return CCYLayout().Seat(mustTeam(t, "ops")).StoreAside(ms) }); !did {
			t.Errorf("StoreAside(%d) = %q, want a panic", ms, got)
		}
	}
}
