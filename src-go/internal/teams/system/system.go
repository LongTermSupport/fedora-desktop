// Package system decides which agent-team bus each team of a launch is live on (Plan 00209
// DESIGN-v2-D-migration.md 3.9, seam 55; LAUNCH-ccy.md decision L3): `agent-bus` (v1) or
// `teamchat` (v2). It is the one function the ccy launcher's `ccy-teams system` and its seat
// verbs share, so the launcher holds no copy of it.
//
//   - v2-live: the team's status record says it was created on v2 (`origin` v2, no `set`), or,
//     with no status record, the owner's seat store holds a pool seat for the team in the
//     session's own checkout (L3: the pool is keyed by checkout, and a seat of another checkout
//     cannot be taken by this launch anyway).
//   - A record that names a migration set is not decided: set records are not read yet (N6), so
//     such a team is refused rather than guessed.
//   - A name that is both a declared v1 team and v2-live is refused, naming both.
//   - Every other team is v1-live.
//   - Teams on both systems in one launch are refused (OD-D2 (a)) with the two choices.
package system

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// The two systems, as `ccy-teams system` prints them.
const (
	AgentBus = "agent-bus"
	Teamchat = "teamchat"
)

// ErrRefused is a launch the design refuses (exit 64): a name on both systems, teams on both
// systems, or no team. ErrUndecided is a team whose system cannot be decided yet (exit 78).
var (
	ErrRefused   = errors.New("refused")
	ErrUndecided = errors.New("undecided")
)

// Line is one team resolved: the string given, the team's id and its system.
type Line struct {
	Given  string
	ID     string
	System string
}

// Sources is what the decision reads: a team's status record, whether the session's checkout
// has a pool seat for it, and the declared v1 teams.
type Sources struct {
	Status    func(paths.TeamID) (status.Record, bool, error)
	PoolHolds func(paths.TeamID) (bool, error)
	Declared  func(context.Context) ([]string, error)
}

// Resolve decides each given team's system and refuses a launch on both.
func Resolve(ctx context.Context, given []string, src Sources) ([]Line, error) {
	if len(given) == 0 {
		return nil, fmt.Errorf("%w: name at least one team", ErrRefused)
	}
	declared, err := src.Declared(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the declared v1 teams: %w", err)
	}
	lines := make([]Line, 0, len(given))
	for _, g := range given {
		line, err := resolveOne(g, slices.Contains(declared, g), src)
		if err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	if _, err := One(lines); err != nil {
		return nil, err
	}
	return lines, nil
}

// v2Why is why a team id is v2-live ("" when it is not), from its status record or the pool.
func v2Why(given string, id paths.TeamID, src Sources) (string, error) {
	rec, found, err := src.Status(id)
	switch {
	case err != nil:
		return "", fmt.Errorf("read the status record of team %s: %w", given, err)
	case found && rec.Set != nil:
		return "", fmt.Errorf("%w: team %s's status record names migration set %s, and set records are not read yet (Plan 00209 N6); launch it on agent-bus once its migration is decided", ErrUndecided, given, rec.Set.String())
	case found && rec.Origin == status.OriginV2:
		return "a v2 status record", nil
	case found:
		return "", fmt.Errorf("%w: team %s's status record has origin %q and no set", ErrUndecided, given, rec.Origin)
	}
	holds, err := src.PoolHolds(id)
	if err != nil {
		return "", fmt.Errorf("read the seat store's pool for team %s: %w", given, err)
	}
	if holds {
		return "pool seat in this checkout", nil
	}
	return "", nil
}

// v2ID is given as a v2 team id; a string outside that grammar can only be a v1 team.
func v2ID(given string) (paths.TeamID, bool) {
	id, err := paths.ParseTeamID(given)
	return id, err == nil
}

func resolveOne(given string, declared bool, src Sources) (Line, error) {
	v1 := Line{Given: given, ID: given, System: AgentBus}
	id, ok := v2ID(given)
	if !ok {
		return v1, nil
	}
	why, err := v2Why(given, id, src)
	switch {
	case err != nil:
		return Line{}, err
	case why == "":
		return v1, nil
	case declared:
		return Line{}, fmt.Errorf("%w: %s is both a declared v1 team and a teamchat team (%s); a session cannot be on both systems. Remove the v1 declaration or the v2 seat", ErrRefused, given, why)
	}
	return Line{Given: given, ID: id.String(), System: Teamchat}, nil
}

// One is the one system every line is on (agent-bus for none), or OD-D2's refusal.
func One(lines []Line) (string, error) {
	by := map[string][]string{}
	for _, l := range lines {
		by[l.System] = append(by[l.System], l.Given)
	}
	switch {
	case len(by[AgentBus]) > 0 && len(by[Teamchat]) > 0:
		return "", fmt.Errorf("%w: these teams are on both systems (agent-bus: %s; teamchat: %s), and one session joins one system. "+
			"Run two sessions, one per system, or migrate the remaining agent-bus team to teamchat",
			ErrRefused, strings.Join(by[AgentBus], ", "), strings.Join(by[Teamchat], ", "))
	case len(by[Teamchat]) > 0:
		return Teamchat, nil
	}
	return AgentBus, nil
}

// claimSuffix marks a placement's claim directory beside a pool seat (paths.PoolClaim): it is
// not a seat.
const claimSuffix = ".new-"

// PoolHolds is the production PoolHolds: whether checkout's pool in store has a seat of the team.
func PoolHolds(store paths.Store, checkout paths.CheckoutKey) func(paths.TeamID) (bool, error) {
	return func(team paths.TeamID) (bool, error) {
		seats, err := PoolSeats(store, checkout, team)
		return len(seats) > 0, err
	}
}

// PoolSeats is the names of checkout's pool seats of team in store, in directory order.
func PoolSeats(store paths.Store, checkout paths.CheckoutKey, team paths.TeamID) ([]paths.SeatName, error) {
	entries, err := os.ReadDir(store.PoolTeam(checkout, team))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the pool: %w", err)
	}
	var seats []paths.SeatName
	for _, e := range entries {
		name, parseErr := paths.ParseSeatName(e.Name())
		if parseErr == nil && e.IsDir() && !strings.Contains(e.Name(), claimSuffix) {
			seats = append(seats, name)
		}
	}
	return seats, nil
}

// StatusRecord is the production Status: the team's record under the status root.
func StatusRecord(team paths.TeamID) (status.Record, bool, error) {
	return status.Read(paths.StatusFile(team), team)
}
