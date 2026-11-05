package paths

import "strconv"

// The names of the backups and the restore (SPEC-n2g, C 3.2.2, 3.12). A backup number is
// Tuwunel's, which starts at 1; a number that is not positive panics as a zero id does.

// RestoreRecord is the record of a restore under way; while anything of this name exists, every
// lock-taking verb but the restore's own resume and discard refuses (C 3.12.2).
func RestoreRecord(t TeamID) string { return AdminState(t) + "/restore.json" }

// RestoreEpochFile is the highest epoch any restore put in place, with the oldest backup a
// restore may still use (n2g-D20).
func RestoreEpochFile(t TeamID) string { return AdminState(t) + "/restore-epoch.json" }

// RestoreConfig is the staging homeserver's config, the team's with its database path set to
// the restore's database (RS2), loaded by the staging unit as a credential.
func RestoreConfig(t TeamID) string { return AdminState(t) + "/config.restore.toml" }

// BackupDir is root's backup tars of the team and the last run's record.
func BackupDir(t TeamID) string { return AdminState(t) + "/backups" }

// BackupTar is backup n's tar (BK1).
func BackupTar(t TeamID, n int64) string {
	return BackupDir(t) + "/" + backupNumber(n) + ".tar"
}

// BackupLastRun is the record of the team's last backup run (n2g-D3).
func BackupLastRun(t TeamID) string { return BackupDir(t) + "/last-run.json" }

// RestoreLostFile is the events a restore of backup n set aside, exported by the child (RC3).
func RestoreLostFile(t TeamID, n int64) string {
	return CtlState(t) + "/restore-" + backupNumber(n) + "-lost.jsonl"
}

// HomeserverPrivateBackups is Tuwunel's own backup engine output, where systemd keeps it.
func HomeserverPrivateBackups(t TeamID) string { return HomeserverPrivateState(t) + "/backups" }

// HomeserverPrivateBackupMeta is Tuwunel's file per backup number, where systemd keeps it.
func HomeserverPrivateBackupMeta(t TeamID) string { return HomeserverPrivateBackups(t) + "/meta" }

// HomeserverPreviousDatabase is the live database set aside while a restore swaps it (RS6).
func HomeserverPreviousDatabase(t TeamID) string { return HomeserverPrivateState(t) + "/db.previous" }

// HomeserverRestoreDatabase is the database backup n is restored into, as the staging unit
// sees it: the database_path of RestoreConfig.
func HomeserverRestoreDatabase(t TeamID, n int64) string {
	return HomeserverState(t) + "/" + restoreDatabase(n)
}

// HomeserverPrivateRestoreDatabase is HomeserverRestoreDatabase where systemd keeps it, as root
// sees it.
func HomeserverPrivateRestoreDatabase(t TeamID, n int64) string {
	return HomeserverPrivateState(t) + "/" + restoreDatabase(n)
}

// RestoreUnit is the transient unit the backup is restored and secured under (RU1).
func RestoreUnit(t TeamID) string { return Product + "-restore@" + t.text() + ".service" }

// RestoreCtlUnit is the transient unit the teamchat-ctl child secures the staged copy from,
// in the staging unit's network (RU2).
func RestoreCtlUnit(t TeamID) string { return Product + "-restore-ctl@" + t.text() + ".service" }

// RestoreUnitCredential is RestoreConfig as the staging unit's command reads it: a transient
// unit expands no %d, so the path is named in full (n2g-D18).
func RestoreUnitCredential(t TeamID) string {
	return "/run/credentials/" + RestoreUnit(t) + "/config.toml"
}

func restoreDatabase(n int64) string { return "db.restore-" + backupNumber(n) }

func backupNumber(n int64) string {
	if n <= 0 {
		panic("paths: a backup number that is not positive")
	}
	return strconv.FormatInt(n, 10)
}
