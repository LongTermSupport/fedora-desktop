package carry

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// Check is `team carry-check --team T` (section 6, O5) under the team lock, waited for
// host.PlayWait (SL1), with a guard that refuses a restore and every carry record (SG1, SG3).
// It runs only on a team whose carried.json exists (either kind): the homeserver is started on
// loopback alone (Deps.Loopback), carry.check asks it for every event of the seal's tail that
// carried.json holds and for the roster event, and the unit is then stopped (RS18), whatever
// the outcome. Each tail event must be found and the roster event's rev and restore epoch must
// be the seal's; otherwise the check fails (1) naming the room or the event, beside its answer.
// Nothing is cleaned up in a defer but the lock's release (SM5).
func Check(ctx context.Context, d Deps, team paths.TeamID) ([]byte, error) {
	if d.Loopback == nil {
		return nil, errors.New("this wiring cannot start a carried team on loopback")
	}
	guard := teamlock.All(restore.Guard(d.Root), Guard(d.Root))
	lock, err := teamlock.Take(ctx, d.LockDir, team, "team.carry-check", host.PlayWait, d.Clock, guard)
	if err != nil {
		return nil, err
	}
	c := &checking{s: &sealing{d: d, team: team}}
	var out []byte
	func() {
		defer func() { err = errors.Join(err, lock.Release()) }()
		out, err = c.run(ctx)
	}()
	return out, err
}

type checking struct{ s *sealing }

func (c *checking) run(ctx context.Context) ([]byte, error) {
	rec, err := c.carried()
	if err != nil {
		return nil, err
	}
	if err = c.notApplied(); err != nil {
		return nil, err
	}
	teamFile, err := c.s.rootFile(paths.TeamDeclaration(c.s.team), fileMax)
	if err != nil {
		return nil, err
	}
	u, err := c.s.units()
	if err != nil {
		return nil, err
	}
	port, err := paths.ParsePort(u.Port)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", paths.UnitsRecord(c.s.team), err)
	}
	found, probeErr := c.probe(ctx, CheckArgs{TeamFile: teamFile, Port: port, Tail: rec.Tail})
	if err = errors.Join(probeErr, c.stop(ctx)); err != nil {
		return nil, err
	}
	return c.verdict(rec, found)
}

// carried is carried.json, by the trust rule as root's own file; a team without one was never
// imported, which refuses the check.
func (c *checking) carried() (CarriedRecord, error) {
	team := c.s.team
	raw, found, err := state.ReadOwnedFile(c.s.d.Root+paths.CarriedRecord(team), maxRecord, c.s.d.StateOwnerUID)
	switch {
	case err != nil:
		return CarriedRecord{}, err
	case !found:
		return CarriedRecord{}, refusedf("team %s was not imported (no %s): team carry-check runs only after team import", team, paths.CarriedRecord(team))
	}
	return DecodeCarriedRecord(raw, team)
}

// notApplied refuses a team `team apply` has stamped (sc-D14): the check would stop a serving
// team and remove the drop-in apply made.
func (c *checking) notApplied() error {
	team := c.s.team
	stamp := paths.StampFile(team)
	_, err := os.Lstat(c.s.d.Root + stamp)
	switch {
	case err == nil:
		return refusedf("team %s is applied (%s exists): team carry-check runs only between team import and team apply", team, stamp)
	case errors.Is(err, fs.ErrNotExist):
		return nil
	default:
		return err
	}
}

// probe starts the homeserver on loopback and asks carry.check for the tail.
func (c *checking) probe(ctx context.Context, a CheckArgs) (Presence, error) {
	if err := c.s.d.Loopback(ctx, c.s.team); err != nil {
		return Presence{}, fmt.Errorf("the loopback start of team %s: %w", c.s.team, err)
	}
	p, err := c.s.d.Child.Check(ctx, c.s.team, a)
	if err != nil {
		return Presence{}, fmt.Errorf("the check of team %s: %w", c.s.team, err)
	}
	if len(p.Found) != len(a.Tail) {
		return Presence{}, fmt.Errorf("carry.check answered %d tail events, not %d", len(p.Found), len(a.Tail))
	}
	return p, nil
}

// stop is the unit stopped after the check, for either kind: the terminator, never started,
// then the homeserver, neither disabled, and each then stopped by RS18. Only then is the
// recorded drop-in the start wrote removed, with its directory, and systemd reloaded, so the
// tree is as it was before the check (sc-D14); a unit not stopped keeps the drop-in it runs on.
func (c *checking) stop(ctx context.Context) error {
	team := c.s.team
	if _, err := host.StopTeamUnits(ctx, c.s.d.Systemd, team, false); err != nil {
		return fmt.Errorf("the stop of team %s after its check: %w", team, err)
	}
	if err := stoppedUnits(ctx, c.s.d.Systemd, team, "team carry-check --team "+team.String()); err != nil {
		return err
	}
	if _, err := host.RemoveDropIn(c.s.d.Root + paths.ServerDropInFile(team)); err != nil {
		return fmt.Errorf("the removal of team %s's recorded drop-in after its check: %w", team, err)
	}
	if err := c.s.d.Systemd.DaemonReload(ctx); err != nil {
		return fmt.Errorf("the reload after team %s's check: %w", team, err)
	}
	return nil
}

// verdict is the answer, and the check's failure when the copy is not whole.
func (c *checking) verdict(rec CarriedRecord, p Presence) ([]byte, error) {
	team := c.s.team
	var missing []string
	rooms := make([]proto.Value, 0, len(rec.Tail))
	for i, e := range rec.Tail {
		rooms = append(rooms, proto.Obj(proto.M("room", proto.Str(e.Room)), proto.M("event_id", proto.Str(e.EventID)), proto.M("found", proto.Bool(p.Found[i]))))
		if !p.Found[i] {
			missing = append(missing, e.Room)
		}
	}
	var problems []error
	if len(missing) > 0 {
		problems = append(problems, fmt.Errorf("the carried copy of team %s is not whole: the seal's newest event is missing in room %s", team, strings.Join(missing, ", ")))
	}
	if p.Epoch.Rev != rec.Roster.Rev || p.Epoch.RestoreEpoch != rec.Roster.RestoreEpoch {
		problems = append(problems, fmt.Errorf("the roster event of team %s (rev %d, restore epoch %d) is not the seal's (rev %d, restore epoch %d)",
			team, p.Epoch.Rev, p.Epoch.RestoreEpoch, rec.Roster.Rev, rec.Roster.RestoreEpoch))
	}
	out := proto.Obj(
		proto.M("v", proto.Int(recordVersion)), proto.M(keyTeam, proto.Str(team.String())), proto.M(keyKind, proto.Str(string(rec.Kind))),
		proto.M("rooms", proto.Arr(rooms...)),
		proto.M("roster_event", proto.Obj(proto.M("rev", proto.Int(p.Epoch.Rev)), proto.M("restore_epoch", proto.Int(p.Epoch.RestoreEpoch)))),
		proto.M("ok", proto.Bool(len(problems) == 0)),
	).Compact()
	return out, errors.Join(problems...)
}
