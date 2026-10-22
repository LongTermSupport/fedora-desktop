package paths

import "testing"

// sampleBackup is the backup number every number-derived name is sampled for.
const sampleBackup int64 = 7

// restoreNames is every name of restore.go (SPEC-n2g's paths line), for each sample team;
// sampleNames includes it, so the v1 and derivation checks cover these names too.
func restoreNames(t *testing.T) map[string][]string {
	t.Helper()
	names := map[string][]string{}
	for _, s := range sampleTeams {
		team := mustTeam(t, s)
		for name, value := range map[string]string{
			"RestoreRecord":                    RestoreRecord(team),
			"RestoreEpochFile":                 RestoreEpochFile(team),
			"RestoreConfig":                    RestoreConfig(team),
			"BackupDir":                        BackupDir(team),
			"BackupTar":                        BackupTar(team, sampleBackup),
			"BackupLastRun":                    BackupLastRun(team),
			"RestoreLostFile":                  RestoreLostFile(team, sampleBackup),
			"HomeserverPrivateBackups":         HomeserverPrivateBackups(team),
			"HomeserverPrivateBackupMeta":      HomeserverPrivateBackupMeta(team),
			"HomeserverPreviousDatabase":       HomeserverPreviousDatabase(team),
			"HomeserverRestoreDatabase":        HomeserverRestoreDatabase(team, sampleBackup),
			"HomeserverPrivateRestoreDatabase": HomeserverPrivateRestoreDatabase(team, sampleBackup),
			"RestoreUnit":                      RestoreUnit(team),
			"RestoreCtlUnit":                   RestoreCtlUnit(team),
			"RestoreUnitCredential":            RestoreUnitCredential(team),
		} {
			names[name] = append(names[name], value)
		}
	}
	return names
}

// restoreZeroDerivations is restore.go's functions of a zero team id, which zeroDerivations
// includes.
func restoreZeroDerivations() map[string]func() string {
	var team TeamID
	return map[string]func() string{
		"RestoreRecord":                    func() string { return RestoreRecord(team) },
		"RestoreEpochFile":                 func() string { return RestoreEpochFile(team) },
		"RestoreConfig":                    func() string { return RestoreConfig(team) },
		"BackupDir":                        func() string { return BackupDir(team) },
		"BackupTar":                        func() string { return BackupTar(team, sampleBackup) },
		"BackupLastRun":                    func() string { return BackupLastRun(team) },
		"RestoreLostFile":                  func() string { return RestoreLostFile(team, sampleBackup) },
		"HomeserverPrivateBackups":         func() string { return HomeserverPrivateBackups(team) },
		"HomeserverPrivateBackupMeta":      func() string { return HomeserverPrivateBackupMeta(team) },
		"HomeserverPreviousDatabase":       func() string { return HomeserverPreviousDatabase(team) },
		"HomeserverRestoreDatabase":        func() string { return HomeserverRestoreDatabase(team, sampleBackup) },
		"HomeserverPrivateRestoreDatabase": func() string { return HomeserverPrivateRestoreDatabase(team, sampleBackup) },
		"RestoreUnit":                      func() string { return RestoreUnit(team) },
		"RestoreCtlUnit":                   func() string { return RestoreCtlUnit(team) },
		"RestoreUnitCredential":            func() string { return RestoreUnitCredential(team) },
	}
}

// The backup and restore names (SPEC-n2g's paths line, C 3.2.2, 3.12).
func TestRestoreNames(t *testing.T) {
	team := mustTeam(t, "ops")
	for got, want := range map[string]string{
		RestoreRecord(team):                       "/var/lib/teamchat-admin/ops/restore.json",
		RestoreEpochFile(team):                    "/var/lib/teamchat-admin/ops/restore-epoch.json",
		RestoreConfig(team):                       "/var/lib/teamchat-admin/ops/config.restore.toml",
		BackupDir(team):                           "/var/lib/teamchat-admin/ops/backups",
		BackupTar(team, 7):                        "/var/lib/teamchat-admin/ops/backups/7.tar",
		BackupLastRun(team):                       "/var/lib/teamchat-admin/ops/backups/last-run.json",
		RestoreLostFile(team, 12):                 "/var/lib/teamchat-ctl/ops/restore-12-lost.jsonl",
		HomeserverPrivateBackups(team):            "/var/lib/private/teamchat/ops/backups",
		HomeserverPrivateBackupMeta(team):         "/var/lib/private/teamchat/ops/backups/meta",
		HomeserverPreviousDatabase(team):          "/var/lib/private/teamchat/ops/db.previous",
		HomeserverRestoreDatabase(team, 3):        "/var/lib/teamchat/ops/db.restore-3",
		HomeserverPrivateRestoreDatabase(team, 3): "/var/lib/private/teamchat/ops/db.restore-3",
		RestoreUnit(team):                         "teamchat-restore@ops.service",
		RestoreCtlUnit(team):                      "teamchat-restore-ctl@ops.service",
		RestoreUnitCredential(team):               "/run/credentials/teamchat-restore@ops.service/config.toml",
	} {
		if got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
}

// The restore and the backup see the same database: the unit's view and root's view differ
// only by systemd's private root.
func TestTheRestoreDatabaseViewsAgree(t *testing.T) {
	team := mustTeam(t, "ops")
	unit, private := HomeserverRestoreDatabase(team, 3), HomeserverPrivateRestoreDatabase(team, 3)
	if unit[len(HomeserverState(team)):] != private[len(HomeserverPrivateState(team)):] {
		t.Fatalf("%s and %s name different entries", unit, private)
	}
	if HomeserverPrivateBackupMeta(team) != HomeserverPrivateBackups(team)+"/meta" {
		t.Fatal("meta/ is not inside the private backups directory")
	}
}

// A backup number is positive: Tuwunel's start at 1.
func TestRestoreNamesRefuseANumberThatIsNotPositive(t *testing.T) {
	team := mustTeam(t, "ops")
	for _, n := range []int64{0, -1} {
		for name, f := range map[string]func() string{
			"BackupTar":                        func() string { return BackupTar(team, n) },
			"RestoreLostFile":                  func() string { return RestoreLostFile(team, n) },
			"HomeserverRestoreDatabase":        func() string { return HomeserverRestoreDatabase(team, n) },
			"HomeserverPrivateRestoreDatabase": func() string { return HomeserverPrivateRestoreDatabase(team, n) },
		} {
			if did, got := panics(f); !did {
				t.Errorf("%s(%d) = %q, want a panic", name, n, got)
			}
		}
	}
}
