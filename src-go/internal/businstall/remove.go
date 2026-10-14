package businstall

import (
	"errors"
	"io/fs"
)

// remove takes a team down: it stops and disables its units, takes away the firewalld rules
// the installer added, its unit drop-in and its applied stamp, and with --purge its data.
// A team that was never installed is a success: the play removes every absent entry on
// every run.
func (a *App) remove(args []string) error {
	opts, err := parseOptions(args, map[string]bool{optTeam: true, "purge": false})
	if err != nil {
		return err
	}
	team, err := opts.team("remove")
	if err != nil {
		return err
	}
	if err = a.requireRoot(); err != nil {
		return err
	}
	hs, timer := hsUnit(team), timerUnit(team)
	running, err := a.anyRunningOrEnabled(hs, timer)
	if err != nil {
		return err
	}
	if running {
		if err := a.sd().DisableNow(a.ctx(), hs, timer); err != nil {
			return failf("%v", err)
		}
		a.changedf("stopped and disabled %s and %s", hs, timer)
	}
	if err := a.removeFirewall(team); err != nil {
		return err
	}
	if err := a.removeDropin(team); err != nil {
		return err
	}
	if err := a.unstamp(team); err != nil {
		return err
	}
	if opts["purge"] == "" {
		return nil
	}
	for _, dir := range []string{stateDir, installDir} {
		if err := a.purge(dir, team); err != nil {
			return err
		}
	}
	return nil
}

// anyRunningOrEnabled is whether the homeserver or the timer is enabled or active.
func (a *App) anyRunningOrEnabled(units ...string) (bool, error) {
	for _, unit := range units {
		enabled, err := a.sd().IsEnabled(a.ctx(), unit)
		if err != nil {
			return false, failf("%v", err)
		}
		if enabled {
			return true, nil
		}
	}
	for _, unit := range units {
		state, err := a.unitState(unit)
		if err != nil {
			return false, err
		}
		if state == stateActive {
			return true, nil
		}
	}
	return false, nil
}

func (a *App) removeFirewall(team string) error {
	record, err := a.fwRecord(team)
	if err != nil {
		return failf("%v", err)
	}
	return wrapFS(a.fw().RemoveAll(a.ctx(), record))
}

func (a *App) removeDropin(team string) error {
	units, found, err := a.tryFS(unitsDir)
	if err != nil {
		return failf("%v", err)
	}
	if !found {
		return nil
	}
	info, err := units.Stat(dropinName(team))
	if errors.Is(err, fs.ErrNotExist) || err == nil && !info.IsDir() {
		return nil
	}
	if err != nil {
		return failf("%v", err)
	}
	if err := units.RemoveAll(dropinName(team)); err != nil {
		return failf("%v", err)
	}
	if err := a.sd().DaemonReload(a.ctx()); err != nil {
		return failf("%v", err)
	}
	a.changedf("removed the %s drop-in", hsUnit(team))
	return nil
}

// unstamp removes the applied stamp: the team no longer runs what the stamp says was applied
// (it was taken down, or restored from a backup whose team.json may differ), so ccy-teams
// shows it as out of date until the play applies it again.
func (a *App) unstamp(team string) error {
	applied, found, err := a.tryFS(appliedDir)
	if err != nil {
		return failf("%v", err)
	}
	if !found {
		return nil
	}
	gone, err := applied.RemoveIfPresent(team + ".json")
	if err != nil {
		return failf("%v", err)
	}
	if gone {
		a.changedf("removed the %s applied stamp", team)
	}
	return nil
}

// purge deletes the team's directory below dir, never following a symlink.
func (a *App) purge(dir, team string) error {
	parent, found, err := a.tryFS(dir)
	if err != nil {
		return failf("%v", err)
	}
	if !found {
		return nil
	}
	if _, err := parent.Lstat(team); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return failf("%v", err)
	}
	if err := parent.RemoveAll(team); err != nil {
		return failf("%v", err)
	}
	a.changedf("purged %s", parent.Display(team))
	return nil
}
