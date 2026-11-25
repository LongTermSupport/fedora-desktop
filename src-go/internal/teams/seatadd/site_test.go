package seatadd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/system"
)

func hostVars(t *testing.T, text string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "localhost.yml")
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

// The site is the host_vars' teamchat_site, read by the strict v2 reader; other keys are not read.
func TestHostVarsSite(t *testing.T) {
	for name, c := range map[string]struct{ text, want string }{
		"declared":   {"other: !vault |\n  x\nteamchat_site: desk\n", "desk"},
		"undeclared": {"other: 1\n", ""},
	} {
		got, err := HostVarsSite(hostVars(t, c.text))()
		if err != nil || got != c.want {
			t.Errorf("%s: %q, %v; want %q", name, got, err, c.want)
		}
	}
}

func mustTeam(t *testing.T, s string) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// Register R59: what the host_vars' teamchat_teams say of a team, by the same strict reader: an
// absent entry (the team removed), a present one, or none.
func TestHostVarsTeams(t *testing.T) {
	text := "teamchat_address: 192.0.2.10\nteamchat_teams:\n" +
		"  - {team: gone, state: absent, purge: true}\n" +
		"  - {team: live, state: present, port: 8448, description: The live team., networks: [192.0.2.0/24], humans: [ann], coordinator: repo.1+desk.podman, " +
		"trust: {kind: github, org: example-org, repos: [{repo: example-org/thing, branches: [main]}]}}\n"
	read := HostVarsTeams(hostVars(t, text))
	for team, want := range map[string]system.Declared{"gone": system.DeclaredAbsent, "live": system.DeclaredPresent, "other": system.NotDeclared} {
		got, err := read(mustTeam(t, team))
		if err != nil || got != want {
			t.Errorf("%s: %v %v, want %v", team, got, err, want)
		}
	}
	if got, err := HostVarsTeams(hostVars(t, "other: 1\n"))(mustTeam(t, "gone")); err != nil || got != system.NotDeclared {
		t.Errorf("no teamchat_teams: %v %v", got, err)
	}
	if _, err := HostVarsTeams(filepath.Join(t.TempDir(), "none.yml"))(mustTeam(t, "gone")); err == nil {
		t.Error("a missing host_vars file was read")
	}
	if _, err := HostVarsTeams(hostVars(t, "teamchat_teams: 3\n"))(mustTeam(t, "gone")); err == nil {
		t.Error("a teamchat_teams that is no list was read")
	}
}

func TestHostVarsSiteRefuses(t *testing.T) {
	if _, err := HostVarsSite(filepath.Join(t.TempDir(), "none.yml"))(); err == nil {
		t.Error("a missing host_vars file was read")
	}
	_, err := HostVarsSite(hostVars(t, "teamchat_site: Not A Label\n"))()
	if err == nil || !strings.Contains(err.Error(), "teamchat_site") {
		t.Errorf("a bad site: %v", err)
	}
}
