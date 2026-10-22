package restore

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// ErrPending is a verb refused because a restore of the team is recorded (RG2); root exits 70
// on it.
var ErrPending = errors.New("restore: a restore of the team is under way")

// PendingError is ErrPending for one team, naming the two ways out.
type PendingError struct {
	Team paths.TeamID
}

func (e *PendingError) Error() string {
	return "a restore of team " + e.Team.String() + " is under way and must be finished first: run " +
		"restore --team " + e.Team.String() + " --resume, or restore --team " + e.Team.String() + " --discard"
}

// Is makes a PendingError ErrPending.
func (e *PendingError) Is(target error) bool { return target == ErrPending }

// Guard is the guard of every lock-taking verb under the host prefix root ("" on a host): it
// refuses while anything named restore.json exists in root/<team>/, a link included (RG2).
func Guard(root string) teamlock.Guard {
	return func(team paths.TeamID) error {
		path := root + paths.RestoreRecord(team)
		_, err := os.Lstat(path)
		switch {
		case err == nil:
			return &PendingError{Team: team}
		case errors.Is(err, fs.ErrNotExist):
			return nil
		}
		return fmt.Errorf("cannot tell whether a restore of team %s is under way: %w", team, err)
	}
}
