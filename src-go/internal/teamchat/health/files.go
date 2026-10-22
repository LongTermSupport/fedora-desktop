package health

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// teamFileMax caps team.json as host reads it.
const teamFileMax = 1 << 20

// Stamps is Teams on a host tree under Root ("" on a host), SPEC-n2f HR2: every directory under
// the status root's teams/ named by a team id and holding a stamp. Every file is read by the
// trust rule, and a stamp, team.json or migration state that cannot be trusted is an error.
type Stamps struct{ Root string }

// Stamped is every stamped team, its name from team.json and its origin `migration` when the
// migration's state.json is there, else `v2`. No teams directory is no team.
func (s Stamps) Stamped(_ context.Context) ([]Team, error) {
	entries, err := os.ReadDir(s.Root + paths.StatusRoot + "/teams")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var teams []Team
	for _, e := range entries {
		team, perr := paths.ParseTeamID(e.Name())
		if perr != nil || !e.IsDir() {
			continue
		}
		stamp, found, err := host.ReadStamp(s.Root, team)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		t, err := s.team(team, stamp)
		if err != nil {
			return nil, err
		}
		teams = append(teams, t)
	}
	return teams, nil
}

// Stamp is whether team has a stamp.
func (s Stamps) Stamp(_ context.Context, team paths.TeamID) (bool, error) {
	_, found, err := host.ReadStamp(s.Root, team)
	return found, err
}

func (s Stamps) team(team paths.TeamID, stamp host.Stamp) (Team, error) {
	file := s.Root + paths.TeamDeclaration(team)
	raw, found, err := state.ReadFile(file, teamFileMax)
	if err != nil {
		return Team{}, err
	}
	if !found {
		return Team{}, fmt.Errorf("health: %s is stamped but has no %s", team, file)
	}
	decl, err := declare.DecodeTeamFile(raw)
	if err != nil {
		return Team{}, fmt.Errorf("health: %s: %w", file, err)
	}
	if decl.ID != team {
		return Team{}, fmt.Errorf("health: %s is the team %s", file, decl.ID)
	}
	origin, err := s.origin(team)
	if err != nil {
		return Team{}, err
	}
	return Team{ID: team, Name: decl.Name, Origin: origin, AppliedMS: stamp.AppliedMS}, nil
}

// origin is `migration` when the migration's state.json is a regular file, `v2` when there is
// none; anything else there is an error.
func (s Stamps) origin(team paths.TeamID) (string, error) {
	file := s.Root + paths.MigrationDir(team) + "/state.json"
	info, err := os.Lstat(file)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return status.OriginV2, nil
	case err != nil:
		return "", err
	case !info.Mode().IsRegular():
		return "", fmt.Errorf("health: %s is not a regular file", file)
	}
	return status.OriginMigration, nil
}

// StatusFiles is host.Status over the tick, for a tree under Root ("" on a host): Refresh is
// the tick's (HR1), Remove deletes the team's two files (HR3).
type StatusFiles struct {
	Root string
	Tick *Tick
}

// Refresh is Tick.Refresh. first is not needed: a team with no record is given its origin
// whether or not it had a stamp before (n2f-H1).
func (s StatusFiles) Refresh(ctx context.Context, team paths.TeamID, _ bool) error {
	return s.Tick.Refresh(ctx, team)
}

// LineStatusRemoved is Remove's CHANGED line.
const LineStatusRemoved = "status record removed"

// Remove deletes status.json and lxc-online.json and answers LineStatusRemoved, or "" when
// there was no record. A record whose origin is the migration's is refused, as is one that
// cannot be read (n2f-H4): the migrate verbs that own its pointer are not built.
func (s StatusFiles) Remove(_ context.Context, team paths.TeamID) (string, error) {
	path := s.Root + paths.StatusFile(team)
	rec, found, err := status.Read(path, team)
	if err != nil {
		return "", err
	}
	if found && rec.Origin == status.OriginMigration {
		return "", fmt.Errorf("health: %s is a migration's record; the migrate verbs keep it", path)
	}
	if _, err = removeFile(s.Root + paths.LXCOnlineFile(team)); err != nil {
		return "", err
	}
	gone, err := removeFile(path)
	if err != nil || !gone {
		return "", err
	}
	return LineStatusRemoved, nil
}

// removeFile removes path; gone is false when there was nothing.
func removeFile(path string) (bool, error) {
	err := os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// NoBackups is Backups until C 3.12 is built (HR4, n2f-D6): no backup and no run, so 36 h
// after the apply the record says the backup has not run.
type NoBackups struct{}

// State is no backup and the last run none.
func (NoBackups) State(context.Context, paths.TeamID) (BackupState, error) {
	return BackupState{LastRun: status.RunNone}, nil
}

// ErrGuestProbeNotBuilt is every guest probe until C 3.9.2 is built (HR5, n2f-D7).
var ErrGuestProbeNotBuilt = errors.New("the LXC guest probe is not built yet")

// GuestsNotBuilt is Guests until C 3.9.2 is built: every probe fails, which the tick records as
// that member's problem with online false.
type GuestsNotBuilt struct{}

// Probe fails with ErrGuestProbeNotBuilt.
func (GuestsNotBuilt) Probe(context.Context, paths.TeamID, string) (GuestProbe, error) {
	return GuestProbe{}, ErrGuestProbeNotBuilt
}
