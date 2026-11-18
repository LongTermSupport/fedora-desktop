package host

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

type removing struct {
	d       Deps
	team    paths.TeamID
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
	r := &removing{d: d, team: team, purge: purge}
	lock, err := teamlock.Take(ctx, d.LockDir, team, "team.remove", d.LockWait, d.Clock, d.RemoveGuard)
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
	if err := r.lxcSeats(); err != nil {
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

// lxcSeats refuses, before any change, a team whose LXC record holds a seat that has not left
// (C 3.9.2): its guest kit, bundle and session unit are removed, and its token revoked, only by
// `lxc apply --phase absent` while the homeserver runs, which a remove would make impossible.
// Seats only left are parked already, their guest files cleaned by a later absent phase.
func (r *removing) lxcSeats() error {
	rec, _, err := lxcstate.ReadRecord(r.d.Root, r.team)
	if err != nil {
		return r.fail(StepLXCSeats, err)
	}
	var live []string
	for _, e := range rec.Members {
		if e.Status != lxcstate.StatusLeft {
			live = append(live, e.Container.String()+"/"+paths.LXCRepoLabel(e.Checkout)+"-"+strconv.Itoa(e.Seat))
		}
	}
	if len(live) == 0 {
		return nil
	}
	return &Error{Verb: verbRemove, Step: StepLXCSeats, Drift: true, Err: fmt.Errorf(
		"team %s's LXC record holds seats that have not left (%s): declare the team absent in teamchat_teams and run the play, "+
			"whose `lxc apply --phase absent` parks and removes them before it removes the team", r.team, strings.Join(live, ", "))}
}

// stopUnit stops and disables the team's terminator, then its homeserver.
func (r *removing) stopUnit(ctx context.Context) error {
	lines, err := StopTeamUnits(ctx, r.d.Systemd, r.team, true)
	r.changed = append(r.changed, lines...)
	return err
}

// StopTeamUnits stops the team's TLS terminator, then its homeserver, so nobody is let in to a
// homeserver that is going down, disabling each after its stop when disable is set (team remove,
// and the seal, SPEC-carry SC4; a drill export only stops, SD3). A unit inactive or failed is
// not stopped again. It answers the CHANGED lines, those before a failure included.
func StopTeamUnits(ctx context.Context, sd Systemd, team paths.TeamID, disable bool) ([]string, error) {
	var lines []string
	for _, u := range []struct{ unit, what string }{
		{paths.TLSUnit(team), "tls unit"}, {paths.ServerUnit(team), "unit"},
	} {
		st, err := sd.State(ctx, u.unit)
		if err != nil {
			return lines, err
		}
		if st.Active != "inactive" && st.Active != "failed" {
			if err := sd.Stop(ctx, u.unit); err != nil {
				return lines, err
			}
			lines = append(lines, u.what+" stopped")
		}
		if disable && st.Enabled {
			if err := sd.Disable(ctx, u.unit); err != nil {
				return lines, err
			}
			lines = append(lines, u.what+" disabled")
		}
	}
	return lines, nil
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
// is the owner's), reloads systemd, and deletes the stamp. The reload runs on every remove of an
// installed team, so one that failed, or a crash before it, is finished by the next.
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
	if err := r.removeOne(paths.TLSDropInFile(r.team), "tls drop-in removed"); err != nil {
		return err
	}
	if err := r.removeOne(paths.ServerDropInFile(r.team), "drop-in removed"); err != nil {
		return err
	}
	return r.d.Systemd.DaemonReload(ctx)
}

// removeOne deletes a drop-in, then its directory when nothing else is in it.
func (r *removing) removeOne(file, line string) error {
	gone, err := RemoveDropIn(r.path(file))
	if gone {
		r.note(line)
	}
	return err
}

// RemoveDropIn deletes the drop-in at dropIn, then its directory when nothing else is in it
// (another file there is the owner's), and answers whether there was a drop-in. It does not
// reload systemd: the caller does, once its removals are done.
func RemoveDropIn(dropIn string) (bool, error) {
	gone, err := removeFile(dropIn)
	if err != nil || !gone {
		return gone, err
	}
	if err = os.Remove(filepath.Dir(dropIn)); err != nil && !errors.Is(err, syscall.ENOTEMPTY) {
		return true, err
	}
	return true, nil
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
func (r *removing) purgeState(context.Context) error {
	if !r.purge {
		return nil
	}
	if err := r.homeserverState(); err != nil {
		return err
	}
	for _, step := range []struct{ path, line string }{
		{paths.CtlState(r.team), "ctl state removed"},
		{paths.ApprovedFile(r.team), "approval removed"},
		{paths.TLSDir(r.team), "leaf removed"},
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

// homeserverState removes the homeserver's StateDirectory= itself: systemd's link to the
// private directory (removed as a link, never followed, or whole if it is a directory), then the
// private directory. It asks systemd nothing: the template unit has no ExecStart= without the
// team's drop-in, which an earlier step or an earlier plain remove deleted, so systemd refuses
// `systemctl clean` on it.
func (r *removing) homeserverState() error {
	removed := false
	for _, p := range []string{paths.HomeserverState(r.team), paths.HomeserverPrivateState(r.team)} {
		there, err := exists(r.path(p))
		if err != nil {
			return err
		}
		if !there {
			continue
		}
		if err := os.RemoveAll(r.path(p)); err != nil {
			return err
		}
		removed = true
	}
	if removed {
		r.note("homeserver state cleaned")
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
