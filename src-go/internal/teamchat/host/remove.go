package host

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

type removing struct {
	d       Deps
	team    paths.TeamID
	unit    string
	purge   bool
	changed []string
}

// Remove is `team remove [--purge]` (C 3.5.3) under the team lock: the unit stopped and
// disabled, the recorded firewall rules removed, the drop-in and the stamp deleted, and the
// status record handed to Deps.Status (which keeps a migration-origin record's pointer). With
// purge also the homeserver's state, ctl/<team>/, the approval and root/<team>/, that last. A
// team that is not installed is a success with no lines. It answers the CHANGED lines, those
// before a failure included.
func Remove(ctx context.Context, d Deps, team paths.TeamID, purge bool) ([]string, error) {
	r := &removing{d: d, team: team, unit: paths.ServerUnit(team), purge: purge}
	lock, err := TakeLock(ctx, d.LockDir, team, "team.remove", d.LockWait, d.Clock)
	if err != nil {
		return nil, r.fail(StepLock, err)
	}
	err = r.run(ctx)
	if rerr := lock.Release(); rerr != nil && err == nil {
		err = r.fail(StepLock, rerr)
	}
	return r.changed, err
}

func (r *removing) fail(step string, err error) error {
	return &Error{Verb: verbRemove, Step: step, Err: err}
}

func (r *removing) note(line string) { r.changed = append(r.changed, line) }

func (r *removing) path(p string) string { return r.d.Root + p }

func (r *removing) run(ctx context.Context) error {
	installed, err := r.installed()
	if err != nil || !installed {
		return err
	}
	steps := []struct {
		name string
		do   func(context.Context) error
	}{
		{StepUnit, r.stopUnit},
		{StepFirewall, r.firewall},
		{StepFiles, r.files},
		{StepStatus, r.status},
		{StepPurge, r.purgeState},
	}
	for _, s := range steps {
		if err := s.do(ctx); err != nil {
			return r.fail(s.name, err)
		}
	}
	return nil
}

// installed is whether any of root/<team>/, ctl/<team>/, the drop-in or the stamp is there.
func (r *removing) installed() (bool, error) {
	for _, p := range []string{
		paths.AdminState(r.team), paths.CtlState(r.team), paths.ServerDropInFile(r.team), paths.StampFile(r.team),
	} {
		there, err := exists(r.path(p))
		if err != nil {
			return false, r.fail(StepInstalled, err)
		}
		if there {
			return true, nil
		}
	}
	return false, nil
}

func (r *removing) stopUnit(ctx context.Context) error {
	st, err := r.d.Systemd.State(ctx, r.unit)
	if err != nil {
		return err
	}
	if st.Active != "inactive" && st.Active != "failed" {
		if err := r.d.Systemd.Stop(ctx, r.unit); err != nil {
			return err
		}
		r.note("unit stopped")
	}
	if st.Enabled {
		if err := r.d.Systemd.Disable(ctx, r.unit); err != nil {
			return err
		}
		r.note("unit disabled")
	}
	return nil
}

// firewall removes the rules firewall.json records, each in both configurations, then the
// record; a rule it does not record is never touched.
func (r *removing) firewall(ctx context.Context) error {
	record := r.path(paths.FirewallRecord(r.team))
	rules, found, err := readRules(record, r.team)
	if err != nil || !found {
		return err
	}
	for _, rule := range rules {
		n, err := ensureRule(ctx, r.d.Firewall, rule, false)
		if n > 0 {
			r.note("firewall removed " + rule.String())
		}
		if err != nil {
			return err
		}
	}
	return os.Remove(record)
}

// files deletes the drop-in, then its directory when nothing else is in it (another file there
// is the owner's), reloads systemd, and deletes the stamp.
func (r *removing) files(ctx context.Context) error {
	if err := r.removeDropIn(ctx); err != nil {
		return err
	}
	gone, err := removeFile(r.path(paths.StampFile(r.team)))
	if err != nil || !gone {
		return err
	}
	r.note("stamp removed")
	return nil
}

func (r *removing) removeDropIn(ctx context.Context) error {
	dropIn := r.path(paths.ServerDropInFile(r.team))
	gone, err := removeFile(dropIn)
	if err != nil || !gone {
		return err
	}
	if err = os.Remove(filepath.Dir(dropIn)); err != nil && !errors.Is(err, syscall.ENOTEMPTY) {
		return err
	}
	if err = r.d.Systemd.DaemonReload(ctx); err != nil {
		return err
	}
	r.note("drop-in removed")
	return nil
}

func (r *removing) status(ctx context.Context) error {
	line, err := r.d.Status.Remove(ctx, r.team)
	if line != "" {
		r.note(line)
	}
	return err
}

// purgeState removes the team's state, root/<team>/ last: while it is there the team is still
// installed, so a purge that failed is finished by the next.
func (r *removing) purgeState(ctx context.Context) error {
	if !r.purge {
		return nil
	}
	db, err := exists(r.path(paths.HomeserverPrivateState(r.team)))
	if err != nil {
		return err
	}
	if db {
		if err := r.d.Systemd.CleanState(ctx, r.unit); err != nil {
			return err
		}
		r.note("homeserver state cleaned")
	}
	for _, step := range []struct{ path, line string }{
		{paths.CtlState(r.team), "ctl state removed"},
		{paths.ApprovedFile(r.team), "approval removed"},
		{paths.AdminState(r.team), "root state removed"},
	} {
		there, err := exists(r.path(step.path))
		if err != nil {
			return err
		}
		if !there {
			continue
		}
		if err := os.RemoveAll(r.path(step.path)); err != nil {
			return err
		}
		r.note(step.line)
	}
	return nil
}

// removeFile deletes path and answers whether it was there.
func removeFile(path string) (bool, error) {
	err := os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// exists is whether anything is at path, not following a link.
func exists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
