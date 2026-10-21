package state

import (
	"errors"
	"testing"
)

func envOf(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

// S6: PINGBUS_HOME, else an absolute XDG_CONFIG_HOME, else HOME; an empty value is unset.
func TestResolveHome(t *testing.T) {
	cases := []struct {
		vars map[string]string
		want string
	}{
		{map[string]string{"PINGBUS_HOME": "/p", "XDG_CONFIG_HOME": "/x", "HOME": "/h"}, "/p"},
		{map[string]string{"PINGBUS_HOME": "", "XDG_CONFIG_HOME": "/x", "HOME": "/h"}, "/x/pingbus"},
		{map[string]string{"XDG_CONFIG_HOME": "x", "HOME": "/h"}, "/h/.config/pingbus"},
	}
	for _, c := range cases {
		home, err := ResolveHome(envOf(c.vars))
		expectf(t, err == nil && home.Dir == c.want, "%v: %q %v", c.vars, home.Dir, err)
	}
	for _, vars := range []map[string]string{{"PINGBUS_HOME": "p"}, {"HOME": "h"}, {}} {
		_, err := ResolveHome(envOf(vars))
		wantClass(t, "a relative home", err, ClassGrammar)
	}
}

// S7, S8, S9: every file of a home and a bundle, by name.
func TestHomeAndBundlePaths(t *testing.T) {
	home := Home{Dir: "/h"}
	got := map[string]string{
		home.StopGuardPath():       "/h/stop-guard.json",
		home.GoodbyePath():         "/h/goodbye.json",
		home.WatchLogPath():        "/h/watch.log",
		home.ToldPath():            "/h/told.json",
		home.ToldLockPath():        "/h/told.lock",
		home.ToldRolesPath():       "/h/told-roles.json",
		home.RelayLockPath():       "/h/relay/lock",
		home.KeeperLockPath():      "/h/keeper.lock",
		home.KeeperLogPath():       "/h/keeper.log",
		home.AppliedPath():         "/h/applied.json",
		home.ActivePath():          "/h/active",
		home.SettingsPath():        "/h/settings.json",
		home.StatusLineInnerPath(): "/h/statusline-inner",
		home.SocketRecordPath():    "/h/socket.json",
	}
	for path, want := range got {
		expectf(t, path == want, "%s, want %s", path, want)
	}
	down, err := home.DownPath("alpha")
	expectf(t, err == nil && down == "/h/down/alpha", "%s %v", down, err)
	_, err = home.DownPath("../x")
	wantClass(t, "a down mark for no team", err, ClassGrammar)
	bundle, err := home.Bundle("alpha")
	expectf(t, err == nil && bundle.Dir == "/h/alpha", "%s %v", bundle.Dir, err)
	expectf(t, bundle.MemberPath() == "/h/alpha/member.json" && bundle.TokenPath() == "/h/alpha/token", "bundle files")
	expectf(t, bundle.ReadmePath() == "/h/alpha/README" && bundle.State().Dir == "/h/alpha/state", "bundle state")
	_, err = home.Bundle("Alpha")
	wantClass(t, "a bundle for no team", err, ClassGrammar)
	expectf(t, SessionHome().Dir == "/tmp/pingbus-home", "the ccy session home")
}

// S13: inject marks are keyed by the first 16 hex digits of SHA-256 of the socket path.
func TestInjectPaths(t *testing.T) {
	key := InjectKey("/run/s.sock")
	expectf(t, len(key) == 16 && key != InjectKey("/run/t.sock"), "%s", key)
	expectf(t, InjectKey("") == "e3b0c44298fc1c14", "the key of the empty path")
	home := Home{Dir: "/h"}
	path, err := home.InjectPath("e3b0c44298fc1c14", InjectSpawned)
	expectf(t, err == nil && path == "/h/inject/e3b0c44298fc1c14.spawned", "%s %v", path, err)
	_, err = home.InjectPath("E3B0", InjectWatch)
	wantClass(t, "a bad key", err, ClassGrammar)
	_, err = home.InjectPath("e3b0c44298fc1c14", "log")
	wantClass(t, "a bad suffix", err, ClassGrammar)
}

// S10: a checkout's seats and its sessions' reports.
func TestCheckoutPaths(t *testing.T) {
	co := Checkout{Dir: "/w"}
	seat, err := co.Seat("alpha", "1")
	expectf(t, err == nil && seat.Dir == "/w/.claude/ccy/pingbus/seats/alpha/1", "%s %v", seat.Dir, err)
	expectf(t, seat.LockPath() == seat.Dir+"/seat.lock" && seat.SaidGoodbyePath() == seat.Dir+"/said-goodbye.json", "seat files")
	expectf(t, seat.Bundle().State().Dir == seat.Dir+"/state", "a seat is a bundle")
	_, err = co.Seat("alpha", "0")
	wantClass(t, "a bad seat", err, ClassGrammar)
	_, err = co.Seat("A", "1")
	wantClass(t, "a bad team", err, ClassGrammar)
	report, err := co.ReportDir("s_1.a@b+c-d")
	expectf(t, err == nil && report == "/w/.claude/ccy/pingbus/sessions/s_1.a@b+c-d", "%s %v", report, err)
	expectf(t, ReportApplied(report) == report+"/applied.json" && ReportConversation(report) == report+"/conversation.json", "report files")
	_, err = co.ReportDir(".x")
	wantClass(t, "a bad session", err, ClassGrammar)
	expectf(t, CCYCheckout().Dir == "/workspace", "the ccy checkout")
}

// S11, S12: the desired membership directory and the session records' directory.
func TestMembershipAndSessionsDirs(t *testing.T) {
	dir, err := MembershipDir(envOf(map[string]string{"PINGBUS_MEMBERSHIP": "/run/m"}))
	expectf(t, err == nil && dir == "/run/m", "%s %v", dir, err)
	expectf(t, DesiredPath(dir) == "/run/m/membership.json" && RelaunchPath(dir) == "/run/m/relaunch.json", "membership files")
	_, err = MembershipDir(envOf(map[string]string{"PINGBUS_MEMBERSHIP": "off"}))
	expectf(t, errors.Is(err, ErrBusOff), "off: %v", err)
	_, err = MembershipDir(envOf(map[string]string{}))
	expectf(t, errors.Is(err, ErrNoMembership), "unset: %v", err)
	_, err = MembershipDir(envOf(map[string]string{"PINGBUS_MEMBERSHIP": "m"}))
	wantClass(t, "a relative membership", err, ClassGrammar)
	sessions, err := SessionsDir(envOf(map[string]string{}), 1000)
	expectf(t, err == nil && sessions == "/tmp/pingbus-sessions-1000", "%s %v", sessions, err)
	sessions, err = SessionsDir(envOf(map[string]string{"PINGBUS_SESSIONS_DIR": "/s"}), 1000)
	expectf(t, err == nil && sessions == "/s", "%s %v", sessions, err)
	_, err = SessionsDir(envOf(map[string]string{"PINGBUS_SESSIONS_DIR": "s"}), 1000)
	wantClass(t, "a relative sessions directory", err, ClassGrammar)
}
