package restore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// The caps of the root files the engine reads (BK2's for the same files).
const (
	smallFileMax  = 64 << 10
	rosterFileMax = 1 << 20
	// nameFileMax caps server-name, one DNS name.
	nameFileMax = 256
)

// RO1's probe: a server that answers at any of 30 probes a second apart serves.
const (
	offlineProbes = 30
	probeEvery    = time.Second
)

// originMigration is a status record of a team area D migrated (n2g-D9).
const originMigration = "migration"

// Validated is what step 01 read and decided: the applied team and its team.json, the live
// config, backup N's tar and the merged roster.
type Validated struct {
	Team     declare.Team
	TeamFile []byte
	Config   []byte
	Tar      Tar
	Merged   Merged
}

// Validate is step 01 of a fresh restore (RV1-RV7). Nothing is written before every rule
// passes, except RV7's start of a team that is merely stopped.
func Validate(ctx context.Context, d Deps, req Request) (*Validated, error) {
	team := req.Team
	if req.Backup < 1 || req.Backup > maxBackup {
		return nil, fmt.Errorf("restore: backup %d is not a backup number", req.Backup)
	}
	k, err := d.FS.RootKind(paths.RestoreRecord(team))
	if err != nil {
		return nil, err
	}
	if k != KindAbsent {
		return nil, refusedf("a restore of team %s is recorded: run restore --team %s --resume, or --discard", team, team)
	}
	if err = leftovers(ctx, d, team); err != nil {
		return nil, err
	}
	v, err := readApplied(d, team)
	if err != nil {
		return nil, err
	}
	if err = v.decide(d, req); err != nil {
		return nil, err
	}
	approved, err := d.Approvals.Load(team)
	if err != nil {
		return nil, err
	}
	if declare.Hold(approved, declare.Input{Team: v.Team}) != nil {
		return nil, refusedf("team %s: an approval is not applied: run team apply first", team)
	}
	return v, live(ctx, d, req, v)
}

// decide is RV3's origin, RV4 and RV5: the tar, the floors and the merge.
func (v *Validated) decide(d Deps, req Request) error {
	team := req.Team
	rec, err := d.StatusRecord(team)
	if err != nil {
		return err
	}
	if rec != nil && rec.Origin == originMigration {
		return refusedf("team %s was migrated, and its restore needs area D's --accept-carry-loss, which is not built", team)
	}
	if v.Tar, err = readTar(d, team, req.Backup); err != nil {
		return err
	}
	floor, err := readFloor(d, team)
	if err != nil {
		return err
	}
	if err = freshBackup(d, team, req.Backup, floor); err != nil {
		return err
	}
	v.Merged, err = merged(d, req, v.Tar, floor, rec)
	return err
}

// leftovers is RV2: anything an earlier restore leaves is named, and nothing is started over
// it.
func leftovers(ctx context.Context, d Deps, team paths.TeamID) error {
	var found []string
	entries, err := d.FS.ReadDir(team, ".")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name == namePrevious || strings.HasPrefix(e.Name, "db.restore-") {
			found = append(found, paths.HomeserverPrivateState(team)+"/"+e.Name)
		}
	}
	for _, path := range []string{paths.RestoreConfig(team), paths.RestoreBlock(team)} {
		k, kindErr := d.FS.RootKind(path)
		if kindErr != nil {
			return kindErr
		}
		if k != KindAbsent {
			found = append(found, path)
		}
	}
	ctl, err := os.ReadDir(d.Root + paths.CtlState(team))
	if err != nil {
		return fmt.Errorf("cannot list %s: %w", paths.CtlState(team), err)
	}
	for _, e := range ctl {
		if strings.HasSuffix(e.Name(), ".previous") {
			found = append(found, paths.CtlState(team)+"/"+e.Name())
		}
	}
	for _, unit := range []string{paths.RestoreUnit(team), paths.RestoreCtlUnit(team)} {
		st, err := d.Units.State(ctx, unit)
		if err != nil {
			return err
		}
		if !st.NotLoaded() {
			found = append(found, "the loaded unit "+unit)
		}
	}
	if len(found) > 0 {
		return refusedf("an earlier restore of team %s left %s: inspect and remove it first", team, strings.Join(found, ", "))
	}
	return nil
}

// readApplied is RV3's files: the stamp, team.json (whose id is the team) and server-name, and
// the live config.
func readApplied(d Deps, team paths.TeamID) (*Validated, error) {
	_, stamped, err := host.ReadStamp(d.Root, team)
	if err != nil {
		return nil, err
	}
	if !stamped {
		return nil, refusedf("team %s is not applied: run team apply first", team)
	}
	v := &Validated{}
	if v.TeamFile, err = readRoot(d, paths.TeamDeclaration(team), smallFileMax); err != nil {
		return nil, err
	}
	if v.Team, err = declare.DecodeTeamFile(v.TeamFile); err != nil {
		return nil, fmt.Errorf("%s: %w", paths.TeamDeclaration(team), err)
	}
	if v.Team.ID != team {
		return nil, fmt.Errorf("%s is not team %s's", paths.TeamDeclaration(team), team)
	}
	name, err := readRoot(d, paths.ServerNameFile(team), nameFileMax)
	if err != nil {
		return nil, err
	}
	if string(name) != v.Team.ServerName {
		return nil, fmt.Errorf("%s does not hold team.json's server_name", paths.ServerNameFile(team))
	}
	v.Config, err = readRoot(d, paths.ServerConfig(team), smallFileMax)
	return v, err
}

// readRoot is one of root's own files by the trust rule; a missing one is an error naming it.
func readRoot(d Deps, path string, limit int64) ([]byte, error) {
	raw, found, err := state.ReadOwnedFile(d.Root+path, limit, d.StateOwnerUID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%s is missing", path)
	}
	return raw, nil
}

// readTar is RV4's tar, BK3-validated, of this team's server.
func readTar(d Deps, team paths.TeamID, n int64) (Tar, error) {
	tar, err := d.Tar(team, n)
	if err != nil {
		return Tar{}, fmt.Errorf("backup %d of team %s: %w", n, team, err)
	}
	name, err := readRoot(d, paths.ServerNameFile(team), nameFileMax)
	if err != nil {
		return Tar{}, err
	}
	if tar.ServerName != string(name) {
		return Tar{}, refusedf("backup %d's server_name is not team %s's", n, team)
	}
	return tar, nil
}

// freshBackup is RV4's rules for a fresh restore: meta/N is there, and N is not below
// min_backup.
func freshBackup(d Deps, team paths.TeamID, n int64, floor EpochFloor) error {
	k, err := d.FS.Kind(team, metaName(team, n))
	if err != nil {
		return err
	}
	if k != KindFile {
		return refusedf("backup %d of team %s has no meta/%d among the homeserver's backups", n, team, n)
	}
	if n < floor.MinBackup {
		return refusedf("backup %d of team %s is older than backup %d, the oldest a --roster-from-backup restore left "+
			"usable: its seqs cannot be ordered against today's roster", n, team, floor.MinBackup)
	}
	return nil
}

// metaName is meta/<n> relative to the state directory.
func metaName(team paths.TeamID, n int64) string {
	return metaDir(team) + "/" + strconv.FormatInt(n, 10)
}

func metaDir(team paths.TeamID) string {
	return strings.TrimPrefix(paths.HomeserverPrivateBackupMeta(team), paths.HomeserverPrivateState(team)+"/")
}

// readFloor is restore-epoch.json, both fields 0 when absent (RM10).
func readFloor(d Deps, team paths.TeamID) (EpochFloor, error) {
	raw, found, err := state.ReadOwnedFile(d.Root+paths.RestoreEpochFile(team), maxEpochFloor, d.StateOwnerUID)
	if err != nil || !found {
		return EpochFloor{}, err
	}
	return DecodeEpochFloor(raw)
}

// merged is the merged roster (RM, RV5): of the current roster and the tar's, or, under
// --roster-from-backup, of the tar's and the status record's members.
func merged(d Deps, req Request, tar Tar, floor EpochFloor, rec *status.Record) (Merged, error) {
	o := MergeOptions{Team: req.Team, High: floor.Epoch, Offline: req.Offline}
	if req.FromBackup {
		if rec == nil {
			return Merged{}, fmt.Errorf("team %s has no status record, which --roster-from-backup reads", req.Team)
		}
		return MergeFromBackup(tar.Roster, rec.Members, o, d.Rand)
	}
	raw, found, err := state.ReadOwnedFile(d.Root+paths.RosterFile(req.Team), rosterFileMax, d.CtlUID)
	if err == nil && !found {
		err = errors.New("it is missing")
	}
	if err != nil {
		return Merged{}, fmt.Errorf("%s: %w (restore --roster-from-backup restores without it)", paths.RosterFile(req.Team), err)
	}
	current, err := apply.DecodeRosterFile(raw, req.Team)
	if err != nil {
		return Merged{}, fmt.Errorf("%w (restore --roster-from-backup restores without it)", err)
	}
	return Merge(current, tar.Roster, o)
}

// live is RV7 and RO1: a stopped, healthy unit is started first; online the server must
// answer, offline it must not answer at any probe within 30 s.
func live(ctx context.Context, d Deps, req Request, v *Validated) error {
	team, unit := req.Team, paths.ServerUnit(req.Team)
	st, err := d.Units.State(ctx, unit)
	if err != nil {
		return err
	}
	var why error
	if st.Load != loadNotFound && st.Active == activeInactive && st.NRestarts == 0 {
		why = d.Serve(ctx, team)
	}
	if req.Offline {
		return notServing(ctx, d, v.Team.Port)
	}
	if why == nil {
		if st, err = d.Units.State(ctx, unit); err != nil {
			return err
		}
		why = d.Ready.ServerVersion(ctx, v.Team.Port)
		if st.Active != activeActive {
			why = fmt.Errorf("the unit is %s", st.Active)
		}
	}
	if why != nil {
		return refusedf("the homeserver of team %s does not answer (%v): restore --team %s --backup %d --offline restores "+
			"without it, but every event after %s is lost and not exported; there is no pre-restore backup",
			team, why, team, req.Backup, stamp(v.Tar.TimeMS))
	}
	return nil
}

// notServing is RO1's probe: a server that answers at any probe within 30 s serves.
func notServing(ctx context.Context, d Deps, port int) error {
	for i := range offlineProbes {
		if i > 0 {
			if err := d.Clock.Sleep(ctx, probeEvery); err != nil {
				return err
			}
		}
		if d.Ready.ServerVersion(ctx, port) == nil {
			return refusedf("the homeserver serves: restore without --offline")
		}
	}
	return nil
}

// stamp is a time in milliseconds as UTC RFC 3339.
func stamp(ms int64) string { return time.UnixMilli(ms).UTC().Format(time.RFC3339) }
