package v1

import (
	"os"
	"strings"
	"testing"
)

// E1: a v1 team name is v1's team file grammar.
func TestE1TeamGrammar(t *testing.T) {
	for _, ok := range []string{"a", "alpha", "team-2", "a23456789012345678901234"} {
		if err := CheckTeam(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "Alpha", "2team", "-a", "a_b", "a/b", "..", "a234567890123456789012345", "a b"} {
		if err := CheckTeam(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestCheckStamp(t *testing.T) {
	if err := CheckStamp(fixedStamp); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "20261124T101112", "20261124t101112Z", "../20261124T101112Z", "2026112T101112Z"} {
		if CheckStamp(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// E3-E6: the team read from a well-made tree.
func TestReadTeamWellMade(t *testing.T) {
	w := newWorld(t)
	w.write(w.teamDir()+"/room_id.reports", roomID('r'), 0o400)
	got, err := readTeam(w.deps, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got.port != w.port || got.serverName != "alpha.agent-bus.internal" || got.token != leakMarker {
		t.Errorf("%+v", got)
	}
	if got.rooms["main"] != roomID('m') || got.rooms["reports"] != roomID('r') {
		t.Errorf("rooms %v", got.rooms)
	}
	if _, ok := got.rooms["questions"]; ok {
		t.Errorf("an absent questions room read as %q", got.rooms["questions"])
	}
}

// E4: server_name is read when given.
func TestReadTeamServerName(t *testing.T) {
	w := newWorld(t)
	w.write(w.teamDir()+"/team.json", `{"team":"alpha","port":2000,"server_name":"other.agent-bus.internal"}`, 0o640)
	got, err := readTeam(w.deps, "alpha")
	if err != nil || got.serverName != "other.agent-bus.internal" || got.port != 2000 {
		t.Fatalf("%+v %v", got, err)
	}
}

// E3-E6: each fault refused, naming what is wrong; never the token.
func TestReadTeamRefusals(t *testing.T) {
	other := Account{UID: os.Getuid() + 1, GID: os.Getgid() + 1}
	cases := map[string]struct {
		setup func(w *world)
		want  string
	}{
		"no v1 user": {func(w *world) { delete(w.users, "agent-bus") }, "no v1 service user agent-bus"},
		"no team":    {func(w *world) { w.team = "beta" }, "no v1 team beta on this host (never made, or purged)"},
		"team dir a symlink": {func(w *world) {
			w.mkdir("/var/lib/agent-bus/real", 0o700)
			if err := os.Symlink(w.root+"/var/lib/agent-bus/real", w.root+"/var/lib/agent-bus/beta"); err != nil {
				w.t.Fatal(err)
			}
			w.team = "beta"
		}, "is not a directory"},
		"team dir a file":     {func(w *world) { w.team = "beta"; w.write("/var/lib/agent-bus/beta", "x", 0o600) }, "is not a directory"},
		"team dir mode":       {func(w *world) { w.mkdir(w.teamDir(), 0o750) }, "mode 0750, not 0700"},
		"team dir owner":      {func(w *world) { w.users["agent-bus"] = other }, "not the v1 user agent-bus"},
		"team.json missing":   {func(w *world) { rm(w, "/team.json") }, "team.json"},
		"team.json mode":      {func(w *world) { w.write(w.teamDir()+"/team.json", `{"port":2000}`, 0o644) }, "mode 0644, not 0640"},
		"team.json symlink":   {func(w *world) { rm(w, "/team.json"); link(w, "/room_id", "/team.json") }, "symlink"},
		"team.json group":     {func(w *world) { w.users["agent-bus"] = Account{UID: os.Getuid(), GID: os.Getgid() + 1} }, "group"},
		"team.json owner":     {func(w *world) { w.deps.RootUID = os.Getuid() + 1 }, "team.json is owned by uid"},
		"team.json not json":  {func(w *world) { w.write(w.teamDir()+"/team.json", `{port`, 0o640) }, "does not decode"},
		"team.json no port":   {func(w *world) { w.write(w.teamDir()+"/team.json", `{"team":"alpha"}`, 0o640) }, "port"},
		"team.json low port":  {func(w *world) { w.write(w.teamDir()+"/team.json", `{"port":1023}`, 0o640) }, "port"},
		"team.json high port": {func(w *world) { w.write(w.teamDir()+"/team.json", `{"port":65536}`, 0o640) }, "port"},
		"team.json bad name":  {func(w *world) { w.write(w.teamDir()+"/team.json", `{"port":2000,"server_name":"a b"}`, 0o640) }, "server_name"},
		"team.json a dir": {func(w *world) {
			rm(w, "/team.json")
			w.mkdir(w.teamDir()+"/team.json", 0o640)
		}, "not a regular file"},
		"main room missing":   {func(w *world) { rm(w, "/room_id") }, "room_id"},
		"main room malformed": {func(w *world) { w.write(w.teamDir()+"/room_id", roomID('m')+"\n", 0o600) }, "one v12 room id"},
		"side room malformed": {func(w *world) { w.write(w.teamDir()+"/room_id.questions", "!short", 0o600) }, "room_id.questions"},
		"room mode":           {func(w *world) { w.write(w.teamDir()+"/room_id", roomID('m'), 0o640) }, "looser than 0600"},
		"token missing":       {func(w *world) { rm(w, "/secrets/admin.token") }, "admin.token"},
		"token mode":          {func(w *world) { w.write(w.teamDir()+"/secrets/admin.token", leakMarker, 0o604) }, "looser than 0600"},
		"token newline":       {func(w *world) { w.write(w.teamDir()+"/secrets/admin.token", leakMarker+"\n", 0o600) }, "printable ASCII"},
		"token empty":         {func(w *world) { w.write(w.teamDir()+"/secrets/admin.token", "", 0o600) }, "printable ASCII"},
		"secrets a symlink": {func(w *world) {
			if err := os.Rename(w.root+w.teamDir()+"/secrets", w.root+w.teamDir()+"/s2"); err != nil {
				w.t.Fatal(err)
			}
			link(w, "/s2", "/secrets")
		}, "secrets is not a directory"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			c.setup(w)
			_, err := readTeam(w.deps, w.team)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err %v, want it to say %q", err, c.want)
			}
			if strings.Contains(err.Error(), leakMarker) {
				t.Fatalf("the error holds the token: %v", err)
			}
		})
	}
}

// E6: a token of the wrong shape is refused without its text in the error.
func TestTokenNeverInTheRefusal(t *testing.T) {
	w := newWorld(t)
	w.write(w.teamDir()+"/secrets/admin.token", leakMarker+" tail", 0o600)
	_, err := readTeam(w.deps, w.team)
	if err == nil || strings.Contains(err.Error(), leakMarker) {
		t.Fatalf("%v", err)
	}
}

func rm(w *world, rel string) {
	w.t.Helper()
	if err := os.Remove(w.root + w.teamDir() + rel); err != nil {
		w.t.Fatal(err)
	}
}

func link(w *world, target, rel string) {
	w.t.Helper()
	if err := os.Symlink(w.root+w.teamDir()+target, w.root+w.teamDir()+rel); err != nil {
		w.t.Fatal(err)
	}
}
