package host

import (
	"context"
	"errors"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// Serve is C 3.5.2 steps 7 and 8 for a team already applied (SPEC-n2g SM3), under a lock the
// caller holds: a daemon-reload, a start when the unit is not active, then the wait for it
// ready and the check of its bound addresses on the port and listen set units.json records. It
// never renders and writes nothing. The reload is unconditional: what systemd has loaded
// cannot be read from the files, and a reload of unchanged units changes nothing.
func Serve(ctx context.Context, d Deps, team paths.TeamID) error {
	unit := paths.ServerUnit(team)
	fail := func(step string, err error) error { return &Error{Verb: verbServe, Step: step, Err: err} }
	units, err := readUnits(d.Root, team)
	if err != nil {
		return fail(StepUnit, err)
	}
	if err := startUnit(ctx, d, unit); err != nil {
		return fail(StepUnit, err)
	}
	if err := ready(ctx, d, unit, units.port); err != nil {
		return fail(StepReady, err)
	}
	if err := boundAs(d, unit, units.port, units.listen); err != nil {
		return fail(StepAddresses, err)
	}
	return nil
}

// ReadUnitsVersion is the server version units.json under root records for team, the
// version its database was last served by, which a restore's staging unit runs (RU1).
func ReadUnitsVersion(root string, team paths.TeamID) (paths.ServerVersion, error) {
	units, err := readUnits(root, team)
	return units.version, err
}

// readUnits is the team's units.json under root, which must exist and decode.
func readUnits(root string, team paths.TeamID) (unitsRecord, error) {
	raw, found, err := state.ReadFile(root+paths.UnitsRecord(team), fileMax)
	if err == nil && !found {
		err = errors.New("no units.json: the team was never applied")
	}
	if err != nil {
		return unitsRecord{}, err
	}
	return decodeUnits(raw, team)
}

// startUnit reloads systemd and starts the unit unless it is active.
func startUnit(ctx context.Context, d Deps, unit string) error {
	if err := d.Systemd.DaemonReload(ctx); err != nil {
		return err
	}
	st, err := d.Systemd.State(ctx, unit)
	if err != nil || st.Active == active {
		return err
	}
	return d.Systemd.Start(ctx, unit)
}
