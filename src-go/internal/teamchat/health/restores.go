package health

import (
	"context"
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// RestoreState is a team's recorded restore (SPEC-n2g HB2): nil Restore when no restore.json
// exists, otherwise its step, backup and pre (0 for null) with the problem the record shows.
type RestoreState struct {
	Restore *status.Restore
	Problem status.Problem
}

// Restores answers a team's recorded restore (restore.Records on the host). A record that
// cannot be read is an error.
type Restores interface {
	State(ctx context.Context, team paths.TeamID) (RestoreState, error)
}

// restore is step 4's restore record, read before the homeserver's steps so that a team with a
// restore recorded is never restarted (HB3).
func (t *Tick) restore(ctx context.Context, team paths.TeamID, f *facts) error {
	state, err := t.cfg.Restores.State(ctx, team)
	if err != nil {
		return fmt.Errorf("health: the restore record of %s: %w", team, err)
	}
	f.restore = state.Restore
	if state.Restore != nil {
		f.problems = append(f.problems, state.Problem)
	}
	return nil
}
