package paths

import "testing"

// hostNames is every name of host.go, for each sample team; sampleNames includes it, so the v1
// and derivation checks cover these names too.
func hostNames(t *testing.T) map[string][]string {
	t.Helper()
	names := map[string][]string{"ServerUnitTemplateFile": {ServerUnitTemplateFile}}
	for _, s := range sampleTeams {
		team := mustTeam(t, s)
		for name, value := range map[string]string{
			"UnitsRecord":               UnitsRecord(team),
			"ServerNameFile":            ServerNameFile(team),
			"ServerDropInFile":          ServerDropInFile(team),
			"HomeserverDatabase":        HomeserverDatabase(team),
			"HomeserverBackups":         HomeserverBackups(team),
			"HomeserverPrivateDatabase": HomeserverPrivateDatabase(team),
		} {
			names[name] = append(names[name], value)
		}
	}
	return names
}

// hostZeroDerivations is host.go's functions of a zero team id, which zeroDerivations includes.
func hostZeroDerivations() map[string]func() string {
	var team TeamID
	return map[string]func() string{
		"UnitsRecord":               func() string { return UnitsRecord(team) },
		"ServerNameFile":            func() string { return ServerNameFile(team) },
		"ServerDropInFile":          func() string { return ServerDropInFile(team) },
		"HomeserverDatabase":        func() string { return HomeserverDatabase(team) },
		"HomeserverBackups":         func() string { return HomeserverBackups(team) },
		"HomeserverPrivateDatabase": func() string { return HomeserverPrivateDatabase(team) },
	}
}

// The host names of `team apply` and `team remove` (C 3.2.2, 3.2.3, 3.2.4).
func TestHostNames(t *testing.T) {
	team := mustTeam(t, "ops")
	for got, want := range map[string]string{
		UnitsRecord(team):               "/var/lib/teamchat-admin/ops/units.json",
		ServerNameFile(team):            "/var/lib/teamchat-admin/ops/server-name",
		ServerDropInFile(team):          "/etc/systemd/system/teamchat-server@ops.service.d/team.conf",
		HomeserverDatabase(team):        "/var/lib/teamchat/ops/db",
		HomeserverBackups(team):         "/var/lib/teamchat/ops/backups",
		HomeserverPrivateDatabase(team): "/var/lib/private/teamchat/ops/db",
		ServerUnitTemplateFile:          "/etc/systemd/system/teamchat-server@.service",
	} {
		if got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
}
