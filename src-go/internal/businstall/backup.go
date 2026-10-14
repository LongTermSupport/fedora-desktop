package businstall

import (
	"bytes"
	"errors"
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tarstate"
)

const (
	metaRel    = "backups/meta"
	backupTime = "2006-01-02 15:04 UTC"
)

var stateTarName = regexp.MustCompile(`^agent-bus-state-(.*)\.tar$`)

func stateTarFile(id string) string { return "agent-bus-state-" + id + ".tar" }

// latestBackup is the highest numbered backup in Tuwunel's backup directory; 0 when there is none.
func latestBackup(teamFS *fsx.FS) (int, error) {
	entries, err := teamFS.ReadDir(metaRel)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	highest := 0
	for _, entry := range entries {
		if !backupIDPattern.MatchString(entry.Name()) {
			continue
		}
		if n, convErr := strconv.Atoi(entry.Name()); convErr == nil && n > highest {
			highest = n
		}
	}
	return highest, nil
}

// backupNow has the homeserver write a database backup, and keeps the team's state beside it
// in root's own directory. The state tars are not in backups/ with the database backup: the
// agent-bus user can write there, and restore extracts them as root.
func (a *App) backupNow(args []string) error {
	opts, err := parseOptions(args, map[string]bool{optTeam: true})
	if err != nil {
		return err
	}
	team, err := opts.team("backup-now")
	if err != nil {
		return err
	}
	if err = a.requireRoot(); err != nil {
		return err
	}
	teamFS, err := a.requireTeamDir(team)
	if err != nil {
		return err
	}
	unit := hsUnit(team)
	if err = a.requireRunning(unit); err != nil {
		return err
	}
	doc, err := a.readTeamDoc(teamFS, "team.json")
	if err != nil {
		return err
	}
	// SIGUSR2 kills a Tuwunel that has not yet installed its handler, and the timer is
	// persistent, so a missed run fires the moment the unit starts: signal only a
	// homeserver that answers.
	if err = a.waitReady(unit, doc.Port); err != nil {
		return err
	}
	latest, err := a.signalForBackup(teamFS, unit)
	if err != nil {
		return err
	}
	if err := a.keepState(team, teamFS, latest); err != nil {
		return err
	}
	a.changedf("backup %d of team %s", latest, team)
	return nil
}

// signalForBackup signals the unit and waits for the new backup's number.
func (a *App) signalForBackup(teamFS *fsx.FS, unit string) (int, error) {
	before, err := latestBackup(teamFS)
	if err != nil {
		return 0, failf("%v", err)
	}
	if err = a.sd().SignalMain(a.ctx(), unit, "SIGUSR2"); err != nil {
		return 0, failf("%v", err)
	}
	for range backupWaitTries {
		latest, latestErr := latestBackup(teamFS)
		if latestErr != nil {
			return 0, failf("%v", latestErr)
		}
		if latest > before {
			return latest, nil
		}
		a.cfg.Sleep(time.Second)
	}
	return 0, failf("no new backup in %s after %d s (is admin_signal_execute set?)", teamFS.Display(metaRel), backupWaitTries)
}

// keepState writes the team's state tar for backup n into root's directory and drops the
// tars of backups Tuwunel has pruned.
func (a *App) keepState(team string, teamFS *fsx.FS, n int) error {
	var buf bytes.Buffer
	if err := tarstate.Write(&buf, teamFS.Root(), tarstate.Limits{}); err != nil {
		return failf("%v", err)
	}
	root, err := a.rootOwner()
	if err != nil {
		return failf("%v", err)
	}
	varLib, err := a.fsAt(varLibDir)
	if err != nil {
		return failf("%v", err)
	}
	if _, err = varLib.EnsureDir("agent-bus-install", privateMode, root); err != nil {
		return failf("%v", err)
	}
	install, err := a.fsAt(installDir)
	if err != nil {
		return failf("%v", err)
	}
	if _, err = install.EnsureDir(team, privateMode, root); err != nil {
		return failf("%v", err)
	}
	teamInstall, err := a.fsAt(installDir + "/" + team)
	if err != nil {
		return failf("%v", err)
	}
	if _, err = teamInstall.EnsureDir("backups", privateMode, root); err != nil {
		return failf("%v", err)
	}
	tars, err := a.fsAt(installDir + "/" + team + "/backups")
	if err != nil {
		return failf("%v", err)
	}
	if _, err := tars.Quiet().PutBytes("state tar", buf.Bytes(), stateTarFile(strconv.Itoa(n)), privateFile, root); err != nil {
		return failf("%v", err)
	}
	return pruneStateTars(tars, teamFS)
}

// pruneStateTars removes the state tars whose database backup no longer exists. Only tars
// this installer names (a backup number) are touched.
func pruneStateTars(tars, teamFS *fsx.FS) error {
	entries, err := tars.ReadDir(".")
	if err != nil {
		return failf("%v", err)
	}
	for _, entry := range entries {
		m := stateTarName.FindStringSubmatch(entry.Name())
		if m == nil || !backupIDPattern.MatchString(m[1]) || !entry.Type().IsRegular() {
			continue
		}
		if _, err := teamFS.Lstat(metaRel + "/" + m[1]); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return failf("%v", err)
		}
		if _, err := tars.RemoveIfPresent(entry.Name()); err != nil {
			return failf("%v", err)
		}
	}
	return nil
}

// backups lists every backup restore accepts: a database backup with its state tar beside
// it, oldest first, with the time the state tar was written (UTC).
func (a *App) backups(args []string) error {
	opts, err := parseOptions(args, map[string]bool{optTeam: true})
	if err != nil {
		return err
	}
	team, err := opts.team("backups")
	if err != nil {
		return err
	}
	if err = a.requireRoot(); err != nil {
		return err
	}
	teamFS, err := a.requireTeamDir(team)
	if err != nil {
		return err
	}
	tars, found, err := a.tryFS(installDir + "/" + team + "/backups")
	if err != nil {
		return failf("%v", err)
	}
	if !found {
		return nil
	}
	entries, err := teamFS.ReadDir(metaRel)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return failf("%v", err)
	}
	for _, id := range completeBackups(entries, tars) {
		info, err := tars.Stat(stateTarFile(id))
		if err != nil {
			return failf("%v", err)
		}
		a.stdout.printf("BACKUP\t%s\t%s\n", id, info.ModTime().UTC().Format(backupTime))
	}
	return nil
}

// completeBackups is the ids of the backups with a state tar beside them, oldest first.
func completeBackups(entries []fs.DirEntry, tars *fsx.FS) []string {
	var ids []string
	for _, entry := range entries {
		if !backupIDPattern.MatchString(entry.Name()) {
			continue
		}
		if info, err := tars.Stat(stateTarFile(entry.Name())); err == nil && info.Mode().IsRegular() {
			ids = append(ids, entry.Name())
		}
	}
	slices.SortFunc(ids, func(x, y string) int {
		if len(x) != len(y) {
			return len(x) - len(y)
		}
		return strings.Compare(x, y)
	})
	return ids
}
