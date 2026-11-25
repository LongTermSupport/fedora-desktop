package system

import (
	"errors"
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// ErrTeamGone is a team whose pool seats cannot log in from this machine because the team is
// not installed here: removed (declared absent), or declared but never installed (register R59).
var ErrTeamGone = errors.New("the team's pool seats cannot be taken")

// Declared is what this machine's host_vars say of a teamchat team (teamchat_teams).
type Declared int

// The three answers: no entry for the team, an entry with state present, one with state absent.
const (
	NotDeclared Declared = iota
	DeclaredPresent
	DeclaredAbsent
)

// Life is a team's lifecycle on this machine, as every chooser of a pool seat judges it before
// it calls a seat free or takes a named one (register R59): the pool folder outlives the team,
// since `team remove --purge` deletes the team's status record and every server file but never
// the owners' seat stores.
//
//   - A status record: the team is installed here; its seats can be taken.
//   - No record, and the host_vars declare the team absent: it was removed; refused.
//   - No record, and the host_vars declare it present: it is not installed here; refused.
//   - No record and no entry: the team is served on another machine (a member placed with
//     `seat place-issued`, docs/agent-bus-cross-network.md); its seats can be taken, and a dead
//     one is found only at its login, since nothing here records another machine's teams.
//
// A source that cannot be read is an error, never a team whose seats can be taken.
type Life struct {
	Status   func(paths.TeamID) (status.Record, bool, error)
	Declared func(paths.TeamID) (Declared, error)
	// HostVars is the file Declared reads, named in a refusal.
	HostVars string
}

// Check is nil for a team whose pool seats can be taken here; ErrTeamGone names why not and
// the way on.
func (l Life) Check(team paths.TeamID) error {
	if l.Status == nil || l.Declared == nil {
		return fmt.Errorf("whether team %s is still installed cannot be judged: no status record or host_vars reader is wired", team)
	}
	_, found, err := l.Status(team)
	if err != nil {
		return fmt.Errorf("the status record of team %s (%s): %w", team, paths.StatusFile(team), err)
	}
	if found {
		return nil
	}
	declared, err := l.Declared(team)
	if err != nil {
		return fmt.Errorf("whether %s declares team %s: %w", l.HostVars, team, err)
	}
	switch declared {
	case DeclaredAbsent:
		return fmt.Errorf("%w: team %s was removed from this machine (teamchat_teams in %s declares it absent, and it has no status record at %s), "+
			"so its seats in this checkout's pool cannot log in. Use a seat of a live team; a seat of it a session still holds is released with "+
			"ccy-teams seats abandon %s --reason \"<text>\"", ErrTeamGone, team, l.HostVars, paths.StatusFile(team), team)
	case DeclaredPresent:
		return fmt.Errorf("%w: team %s is declared in %s but is not installed on this machine (no status record at %s), "+
			"so its seats cannot log in. Run the teamchat play (playbooks/imports/play-teamchat.yml) to install it", ErrTeamGone, team, l.HostVars, paths.StatusFile(team))
	case NotDeclared:
	}
	return nil
}
