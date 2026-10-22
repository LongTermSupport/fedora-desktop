package admin

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/backup"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// BackupWait is how long `backup` waits for the team lock (BV1).
const BackupWait = 10 * time.Minute

var errBackupsUnwired = errors.New("backups are not wired yet")

// backupVerb is `backup --team T` (BV1) and `backup --all` (BV2, the timer's).
func backupVerb(ctx context.Context, e Env, args []string) term.ExitCode {
	sys, code := e.system()
	if code != term.ExitOK {
		return code
	}
	team, all, err := backupArgs(args)
	if err != nil {
		return failWith(e, err)
	}
	if sys.Backup == nil {
		return fail(e.Stderr, term.ExitFailure, errBackupsUnwired.Error())
	}
	if all {
		return backupAll(ctx, e, sys)
	}
	res, taken, err := backupOne(ctx, sys, team)
	if taken {
		if code = answer(e, string(backupDoc(team, res).Compact())+"\n"); code != term.ExitOK {
			return code
		}
	}
	if err != nil {
		return failWith(e, err)
	}
	return term.ExitOK
}

// backupArgs is BV4: exactly one of --team T and --all.
func backupArgs(args []string) (paths.TeamID, bool, error) {
	flags, err := parseFlags(args, []string{flagTeam}, "all")
	if err != nil {
		return paths.TeamID{}, false, err
	}
	_, hasTeam := flags[flagTeam]
	all := flags["all"] == flagTrue
	switch {
	case all && hasTeam:
		return paths.TeamID{}, false, usagef("--all and --team exclude each other")
	case all:
		return paths.TeamID{}, true, nil
	}
	team, err := teamFlag(flags)
	return team, false, err
}

// backupAll is BV2 (n2g-D5): every stamped team in turn, one stderr line each, nothing on
// stdout, 1 when any failed.
func backupAll(ctx context.Context, e Env, sys *System) term.ExitCode {
	teams, err := health.Stamps{Root: sys.Host.Root}.Stamped(ctx)
	if err != nil {
		return failWith(e, err)
	}
	code := term.ExitOK
	for _, t := range teams {
		res, _, err := backupOne(ctx, sys, t.ID)
		line := t.ID.String() + " backup " + strconv.FormatInt(res.Backup, 10)
		if err != nil {
			line, code = t.ID.String()+" failed: "+err.Error(), term.ExitFailure
		}
		if _, werr := fmt.Fprintln(e.Stderr, oneLine(line)); werr != nil {
			return term.ExitFailure
		}
	}
	return code
}

// backupOne is one team's backup under its lock: Take, then the status record refreshed
// whether or not it was taken. taken says the backup is on disk even when err is not nil.
func backupOne(ctx context.Context, sys *System, team paths.TeamID) (backup.Result, bool, error) {
	lock, err := teamlock.Take(ctx, sys.Host.LockDir, team, "backup", BackupWait, sys.Host.Clock, sys.Host.Guard)
	if err != nil {
		return backup.Result{}, false, err
	}
	res, err := backup.Take(ctx, *sys.Backup, lock, nil)
	taken := err == nil
	if serr := sys.Host.Status.Refresh(ctx, team, false); serr != nil {
		err = errors.Join(err, fmt.Errorf("the status record: %w", serr))
	}
	return res, taken, errors.Join(err, lock.Release())
}

// backupDoc is BV1's answer.
func backupDoc(team paths.TeamID, res backup.Result) proto.Value {
	pruned := make([]proto.Value, 0, len(res.Pruned))
	for _, n := range res.Pruned {
		pruned = append(pruned, proto.Int(n))
	}
	return proto.Obj(
		proto.M("v", proto.Int(wireMajor)), proto.M("team", proto.Str(team.String())),
		proto.M("backup", proto.Int(res.Backup)), proto.M("time_ms", proto.Int(res.TimeMS)),
		proto.M("pruned", proto.Arr(pruned...)),
	)
}

// backupsVerb is `backups --team T` (BV3): no lock, reads only.
func backupsVerb(_ context.Context, e Env, args []string) term.ExitCode {
	sys, code := e.system()
	if code != term.ExitOK {
		return code
	}
	flags, err := parseFlags(args, []string{flagTeam})
	if err != nil {
		return failWith(e, err)
	}
	team, err := teamFlag(flags)
	if err != nil {
		return failWith(e, err)
	}
	if sys.Backup == nil {
		return fail(e.Stderr, term.ExitFailure, errBackupsUnwired.Error())
	}
	l, err := backup.List(*sys.Backup, team)
	if err != nil {
		return failWith(e, err)
	}
	return answer(e, string(l.Value(team).Compact())+"\n")
}
