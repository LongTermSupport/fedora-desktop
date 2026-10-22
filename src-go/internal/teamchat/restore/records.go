package restore

import (
	"context"
	"errors"
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// Recorded is the guard of --resume and --discard (RG3): the opposite of Guard, it refuses
// (ErrRefused) while no restore.json exists in root/<team>/.
func Recorded(root string) teamlock.Guard {
	guard := Guard(root)
	return func(team paths.TeamID) error {
		err := guard(team)
		switch {
		case errors.Is(err, ErrPending):
			return nil
		case err == nil:
			return refusedf("no restore of team %s is recorded: there is nothing to resume or discard", team)
		}
		return err
	}
}

// Records is the health tick's view of the recorded restores under the host prefix Root (HB2):
// restore.json read by the trust rule as a file of OwnerUID's (root's, 0 on a host).
type Records struct {
	Root     string
	OwnerUID uint32
}

// State is team's recorded restore and the problem it is, or none with no restore.json.
func (r Records) State(_ context.Context, team paths.TeamID) (health.RestoreState, error) {
	raw, found, err := state.ReadOwnedFile(r.Root+paths.RestoreRecord(team), maxRecord, r.OwnerUID)
	if err != nil {
		return health.RestoreState{}, fmt.Errorf("the restore.json of team %s: %w", team, err)
	}
	if !found {
		return health.RestoreState{}, nil
	}
	rec, err := DecodeRecord(raw, team)
	if err != nil {
		return health.RestoreState{}, fmt.Errorf("the restore.json of team %s: %w", team, err)
	}
	return health.RestoreState{
		Restore: &status.Restore{Step: string(rec.Step), Backup: rec.Backup, Pre: rec.Pre},
		Problem: recordProblem(team, rec),
	}, nil
}

// recordProblem is what a recorded restore is on the status record: staged waits for a resume
// or a discard, every other step was interrupted and goes on with a resume.
func recordProblem(team paths.TeamID, rec Record) status.Problem {
	verb := paths.AdminBinary + " restore --team " + team.String()
	p := status.Problem{What: "restore interrupted", Fix: verb + " --resume"}
	if rec.Step == StepStaged {
		p = status.Problem{What: "restore staged", Fix: verb + " --resume, or " + verb + " --discard"}
	}
	if rec.Offline {
		p.What += " (offline)"
	}
	return p
}
