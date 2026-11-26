package loadgen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

func TestSetupMakesTheTeam(t *testing.T) {
	team := labTeam(t, newStepClock(), 3)
	if team.V != TeamFileVersion || team.ServerName != labName || team.Team != TeamName {
		t.Fatalf("team = %+v", team)
	}
	roles := []string{}
	for _, a := range team.Agents {
		roles = append(roles, a.Role)
	}
	if want := []string{proto.RoleCoordinator, proto.RoleWorker, proto.RoleWorker}; !slices.Equal(roles, want) {
		t.Fatalf("agent roles %v, want %v", roles, want)
	}
	if team.Human.Role != RoleHuman || team.HumanPassword == "" || team.Human.Token == "" {
		t.Fatalf("human %+v", team.Human)
	}
	rooms := []string{team.Rooms.Main, team.Rooms.Reports, team.Rooms.Questions}
	if slices.Contains(rooms, "") || rooms[0] == rooms[1] || rooms[1] == rooms[2] || rooms[0] == rooms[2] {
		t.Fatalf("rooms %+v", team.Rooms)
	}
	for _, a := range append(slices.Clone(team.Agents), team.Human) {
		cl := member(t, team, a.Token)
		for _, room := range rooms {
			got, err := cl.Membership(t.Context(), room, a.UserID)
			if err != nil || got != "join" {
				t.Errorf("%s in %s: %q (%v), want join", a.UserID, room, got, err)
			}
		}
	}
	raw, err := member(t, team, team.Human.Token).GetState(t.Context(), team.Rooms.Main, proto.RosterType, "")
	if err != nil {
		t.Fatalf("roster: %v", err)
	}
	v, err := proto.Parse(raw)
	if err != nil {
		t.Fatalf("roster: %v", err)
	}
	r, err := proto.DecodeRoster(v, labName, TeamName, team.Rooms.Main)
	if err != nil {
		t.Fatalf("DecodeRoster: %v", err)
	}
	if !slices.Equal(r.Humans, []string{team.Human.UserID}) || len(r.Members) != 3 || r.Members[team.Agents[0].UserID].Role != proto.RoleCoordinator {
		t.Fatalf("roster %+v", r)
	}
}

func TestSetupRefusesAServerNotFresh(t *testing.T) {
	c := newStepClock()
	srv := lab(t, labName, c)
	setUp(t, srv, c, 2)
	_, err := Setup(t.Context(), SetupOptions{Base: srv.URL(), ServerName: labName, RegKey: srv.RegistrationKey(), Agents: 2, Clock: c})
	if err == nil || !strings.Contains(err.Error(), "a fresh lab server") {
		t.Fatalf("a second setup: %v", err)
	}
}

func TestSetupRefusesAServerOfAnotherName(t *testing.T) {
	c := newStepClock()
	srv := lab(t, "other.test", c)
	_, err := Setup(t.Context(), SetupOptions{Base: srv.URL(), ServerName: labName, RegKey: srv.RegistrationKey(), Agents: 2, Clock: c})
	if err == nil || !strings.Contains(err.Error(), "is not on "+labName) {
		t.Fatalf("Setup: %v", err)
	}
}

func TestSetupRefusesBadOptions(t *testing.T) {
	c := newStepClock()
	srv := lab(t, labName, c)
	cases := []struct {
		o    SetupOptions
		says string
	}{
		{SetupOptions{Base: srv.URL(), ServerName: labName, RegKey: srv.RegistrationKey(), Agents: MinAgents - 1}, "--agents"},
		{SetupOptions{Base: srv.URL(), ServerName: labName, RegKey: srv.RegistrationKey(), Agents: MaxAgents + 1}, "--agents"},
		{SetupOptions{Base: srv.URL(), ServerName: "alpha.teamchat.internal", RegKey: srv.RegistrationKey(), Agents: 2}, "not under .test"},
		{SetupOptions{Base: srv.URL(), ServerName: labName, RegKey: "", Agents: 2}, "registration key"},
		{SetupOptions{Base: srv.URL(), ServerName: labName, RegKey: "wrong", Agents: 2}, "register the team's admin"},
	}
	for _, tc := range cases {
		tc.o.Clock = c
		if _, err := Setup(t.Context(), tc.o); err == nil || !strings.Contains(err.Error(), tc.says) {
			t.Errorf("Setup(agents %d, server %q) = %v, want refused saying %q", tc.o.Agents, tc.o.ServerName, err, tc.says)
		}
	}
}

func TestTeamFileRoundTrip(t *testing.T) {
	team := labTeam(t, newStepClock(), 2)
	dir := filepath.Join(t.TempDir(), "team")
	if err := WriteTeam(dir, team); err != nil {
		t.Fatalf("WriteTeam: %v", err)
	}
	for path, want := range map[string]os.FileMode{dir: 0o700, filepath.Join(dir, TeamFile): 0o600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Errorf("%s: %v (%v), want mode %v", path, info, err, want)
		}
	}
	got, err := ReadTeam(dir)
	if err != nil {
		t.Fatalf("ReadTeam: %v", err)
	}
	if got.Human != team.Human || !slices.Equal(got.Agents, team.Agents) || got.Rooms != team.Rooms || got.HumanPassword != team.HumanPassword {
		t.Fatalf("read %+v, wrote %+v", got, team)
	}
	if err := WriteTeam(dir, team); err == nil || !strings.Contains(err.Error(), "a new directory") {
		t.Fatalf("a second write into the directory: %v", err)
	}
}

func TestReadTeamRefusesAFileOffTheRule(t *testing.T) {
	team := labTeam(t, newStepClock(), 2)
	write := func(t *testing.T, mutate func(*Team)) string {
		t.Helper()
		bad := team
		mutate(&bad)
		data, err := json.Marshal(bad)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, TeamFile), data, 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	cases := []struct {
		name   string
		mutate func(*Team)
		says   string
	}{
		{"another homeserver", func(b *Team) { b.Homeserver = "http://192.0.2.4:8008" }, "not a loopback IP"},
		{"another server name", func(b *Team) { b.ServerName = "alpha.teamchat.internal" }, "not under .test"},
		{"another version", func(b *Team) { b.V = 2 }, "version"},
		{"no agents", func(b *Team) { b.Agents = nil }, "agents"},
		{"no rooms", func(b *Team) { b.Rooms.Questions = "" }, "rooms"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ReadTeam(write(t, c.mutate)); err == nil || !strings.Contains(err.Error(), c.says) {
				t.Fatalf("ReadTeam: %v, want refused saying %q", err, c.says)
			}
		})
	}
	if _, err := ReadTeam(t.TempDir()); err == nil {
		t.Fatal("ReadTeam of a directory with no team file passed")
	}
}
