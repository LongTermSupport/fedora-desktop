package admin

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
)

// RestoreChild is restore.Child (RC1-RC8): each op asked of the teamchat-ctl child, through
// Live for the live server, or through the child Unit starts from the secure unit's argv for
// the staged one (RU2).
type RestoreChild struct {
	Live Child
	Unit func(argv []string) Child
}

// Secure implements restore.Child: restore.secure through unit (RC1).
func (c RestoreChild) Secure(ctx context.Context, team paths.TeamID, unit []string, a restore.SecureArgs) ([]string, error) {
	child, err := c.unit(unit)
	if err != nil {
		return nil, err
	}
	files, err := fileArgs(team, a.Rooms, a.Roster)
	if err != nil {
		return nil, err
	}
	members := append([]proto.Member{proto.M(keyPort, proto.Int(int64(a.Port))), proto.M(keyToken, proto.Str(a.Token))}, files...)
	members = append(members,
		proto.M(keyRetire, lineArray(a.Retire)), proto.M(keyRevoke, lineArray(a.Revoke)),
		proto.M(keyRevokeHumans, lineArray(a.RevokeHumans)), proto.M(keyLock, lineArray(a.Lock)),
	)
	ans, err := askRestore(ctx, child, OpRestoreSecure, team, a.TeamFile, members...)
	return ans.Changed, err
}

// Check implements restore.Child: restore.check through unit against the staged server, or
// through Live, with null port and token, against the live one (RC8).
func (c RestoreChild) Check(ctx context.Context, team paths.TeamID, unit []string, a restore.CheckArgs) (restore.Epoch, error) {
	if (unit == nil) != a.Live() {
		return restore.Epoch{}, errors.New("restore.check reads the live server with no unit, and the staged one only through its unit")
	}
	child, port, token := c.Live, proto.Null(), proto.Null()
	if unit != nil {
		var err error
		if child, err = c.unit(unit); err != nil {
			return restore.Epoch{}, err
		}
		port, token = proto.Int(int64(a.Port)), proto.Str(a.Token)
	}
	ans, err := askRestore(ctx, child, OpRestoreCheck, team, a.TeamFile, proto.M(keyPort, port), proto.M(keyToken, token))
	if err != nil {
		return restore.Epoch{}, err
	}
	return *ans.Epoch, nil
}

// Swap implements restore.Child: restore.swap (RC2).
func (c RestoreChild) Swap(ctx context.Context, team paths.TeamID, a restore.SwapArgs) ([]string, error) {
	files, err := fileArgs(team, a.Rooms, a.Roster)
	if err != nil {
		return nil, err
	}
	ans, err := askRestore(ctx, c.Live, OpRestoreSwap, team, a.TeamFile, append([]proto.Member{proto.M(keyToken, proto.Str(a.Token))}, files...)...)
	return ans.Changed, err
}

// Export implements restore.Child: restore.export (RC3).
func (c RestoreChild) Export(ctx context.Context, team paths.TeamID, a restore.ExportArgs) (restore.Export, error) {
	ans, err := askRestore(ctx, c.Live, OpRestoreExport, team, a.TeamFile,
		proto.M(keyBackup, proto.Int(a.Backup)), proto.M(keySinceMS, proto.Int(a.SinceMS)))
	if err != nil {
		return restore.Export{}, err
	}
	return *ans.Export, nil
}

// Finish implements restore.Child: restore.finish (RC4).
func (c RestoreChild) Finish(ctx context.Context, team paths.TeamID, teamFile []byte) ([]string, error) {
	ans, err := askRestore(ctx, c.Live, OpRestoreFinish, team, teamFile)
	return ans.Changed, err
}

// Revert implements restore.Child: restore.revert (RC5).
func (c RestoreChild) Revert(ctx context.Context, team paths.TeamID, teamFile []byte) ([]string, error) {
	ans, err := askRestore(ctx, c.Live, OpRestoreRevert, team, teamFile)
	return ans.Changed, err
}

// unit is the child started through the secure unit's argv.
func (c RestoreChild) unit(argv []string) (Child, error) {
	if argv == nil {
		return nil, errors.New("this restore op runs only through the secure unit")
	}
	if c.Unit == nil {
		return nil, &configError{"the restore has no way to start the secure unit's child"}
	}
	return c.Unit(argv), nil
}

// fileArgs are rooms and roster, each in its own file's encoding (RC1, RC2).
func fileArgs(team paths.TeamID, rooms proto.RoomIDs, roster apply.RosterFile) ([]proto.Member, error) {
	r, err := proto.Parse(apply.EncodeRooms(team, rooms))
	if err != nil {
		return nil, fmt.Errorf("the rooms arg: %w", err)
	}
	f, err := proto.Parse(roster.Encode())
	if err != nil {
		return nil, fmt.Errorf("the roster arg: %w", err)
	}
	return []proto.Member{proto.M(keyRooms, r), proto.M(keyRoster, f)}, nil
}

// askRestore asks child op with the team file and members as its args. The answer must fit the
// op; a failed one is a ChildError, returned beside the lines changed before it.
func askRestore(ctx context.Context, child Child, op string, team paths.TeamID, teamFile []byte, members ...proto.Member) (Answer, error) {
	tf, err := proto.Parse(teamFile)
	if err == nil && tf.Kind() != proto.KindObject {
		err = errors.New("not an object")
	}
	if err != nil {
		return Answer{}, fmt.Errorf("the team file: %w", err)
	}
	a, err := child.Ask(ctx, Request{Op: op, Team: team, Args: proto.Obj(append([]proto.Member{proto.M(keyTeamFile, tf)}, members...)...)})
	if err == nil {
		err = answerFits(op, a)
	}
	if err != nil {
		return Answer{}, fmt.Errorf("the %s child: %w", paths.CtlUser, err)
	}
	if !a.OK {
		return Answer{Changed: a.Changed}, &ChildError{Class: a.Error.Class, Message: a.Error.Message}
	}
	return a, nil
}

// systemdRun is the one program a unit child's argv may start (RU1, RU2).
const systemdRun = "/usr/bin/systemd-run"

// unitEnv is a unit child's whole environment (RU3).
var unitEnv = []string{"PATH=" + childPath, "LANG=C"}

// UnitChild is a Child started as root through a unit's argv (RU2): systemd-run starts it as
// teamchat-ctl in the staging unit's namespace, and its answer is judged as ExecChild's (CS4).
type UnitChild struct {
	Runner execx.Runner
	Argv   []string
}

// Ask implements Child.
func (c UnitChild) Ask(ctx context.Context, req Request) (Answer, error) {
	if len(c.Argv) == 0 || c.Argv[0] != systemdRun {
		return Answer{}, &configError{"the secure unit's argv does not start with " + systemdRun}
	}
	return askRun(ctx, nil, c.Runner, execx.Cmd{
		Name: c.Argv[0], Args: slices.Clone(c.Argv[1:]), Dir: "/", Env: slices.Clone(unitEnv), CleanEnv: true,
	}, req)
}
