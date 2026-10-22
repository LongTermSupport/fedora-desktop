package health

import (
	"context"
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// Refresh is host.Status.Refresh (SPEC-n2f HR1): the tick's steps for one stamped team while
// the caller holds its team lock. It takes no lock and never restarts the unit (a missed probe
// is recorded missed, with no re-probe), and writes the record only if its seq is the one it
// read. A team with no stamp, a removed one and another writer's record are errors (n2f-H2).
func (t *Tick) Refresh(ctx context.Context, team paths.TeamID) error {
	stamped, err := t.cfg.Teams.Stamped(ctx)
	if err != nil {
		return fmt.Errorf("health: the stamped teams: %w", err)
	}
	for _, s := range stamped {
		if s.ID == team {
			return t.refresh(ctx, s)
		}
	}
	return fmt.Errorf("health: %s has no stamp", team)
}

func (t *Tick) refresh(ctx context.Context, team Team) error {
	f, err := t.gather(ctx, team, true)
	if err != nil {
		return err
	}
	out := t.locked(ctx, team, f)
	switch out.Result {
	case ResultWritten:
		return nil
	case ResultDropped:
		return fmt.Errorf("health: the status record of %s was not written: %s", team.ID, out.Why)
	}
	return out.Err
}
