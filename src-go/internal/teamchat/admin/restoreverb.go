package admin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// RestoreRequest is a fresh restore (SPEC-n2g section 7): the team, the backup, and the two
// choices only a fresh restore makes, which the record keeps for a resume.
type RestoreRequest struct {
	Team             paths.TeamID
	Backup           int64
	Offline          bool
	RosterFromBackup bool
}

// Restorer is the restore engine (restore.Run, Resume and Discard over restore.Deps), run under
// the team lock the verb holds. Each answers its JSON document (RS9, RS13) when it has one,
// beside its failure: a clean-up that failed after the restore served still has an answer
// (RS8).
type Restorer interface {
	Run(ctx context.Context, held *teamlock.Lock, req RestoreRequest) ([]byte, error)
	Resume(ctx context.Context, held *teamlock.Lock, team paths.TeamID) ([]byte, error)
	Discard(ctx context.Context, held *teamlock.Lock, team paths.TeamID) ([]byte, error)
}

// The restore verb's flags beyond --team.
const (
	flagBackup     = "backup"
	flagResume     = "resume"
	flagDiscard    = "discard"
	flagLost       = "lost"
	flagOffline    = "offline"
	flagFromBackup = "roster-from-backup"
)

// maxBackupNumber is the highest backup number a restore takes (the record's).
const maxBackupNumber = 1<<53 - 1

// maxLost caps the lost-events export --lost reads (RS14).
const maxLost = 64 << 20

// restoreArgs is what the flags ask: one of a fresh restore, --resume, --discard or --lost.
type restoreArgs struct {
	mode string
	req  RestoreRequest
}

// restoreVerb is `restore` (RS1-RS15): --lost reads the export and takes no lock; every other
// form asks the wiring for the engine (refused in production until DC8, RW2) before the team
// lock, then runs it under the lock with its own guard (RG3).
func restoreVerb(ctx context.Context, e Env, args []string) term.ExitCode {
	sys, code := e.system()
	if code != term.ExitOK {
		return code
	}
	a, err := parseRestore(args)
	if err != nil {
		return failWith(e, err)
	}
	if a.mode == flagLost {
		return printLost(e, sys, a.req.Team, a.req.Backup)
	}
	if sys.Restore == nil {
		return fail(e.Stderr, ExitConfig, "this wiring has no restore engine")
	}
	engine, err := sys.Restore()
	if err != nil {
		return failWith(e, err)
	}
	guard := restore.Guard(sys.Host.Root)
	if a.mode != "" {
		guard = restore.Recorded(sys.Host.Root)
	}
	lock, err := teamlock.Take(ctx, sys.Host.LockDir, a.req.Team, "restore", host.PlayWait, sys.Host.Clock, guard)
	if err != nil {
		return failWith(e, err)
	}
	var out []byte
	switch a.mode {
	case flagResume:
		out, err = engine.Resume(ctx, lock, a.req.Team)
	case flagDiscard:
		out, err = engine.Discard(ctx, lock, a.req.Team)
	default:
		out, err = engine.Run(ctx, lock, a.req)
	}
	err = errors.Join(err, lock.Release())
	if out != nil {
		if code = answer(e, string(out)+"\n"); code != term.ExitOK {
			return code
		}
	}
	if err != nil {
		return failWith(e, err)
	}
	return term.ExitOK
}

// parseRestore is the flags' one grammar (BV4, RO2): --team always; --backup N alone or with
// --offline and --roster-from-backup; --resume or --discard alone, since the record decides the
// rest; --lost with --backup N only.
func parseRestore(args []string) (restoreArgs, error) {
	flags, err := parseFlags(args, []string{flagTeam, flagBackup}, flagResume, flagDiscard, flagLost, flagOffline, flagFromBackup)
	if err != nil {
		return restoreArgs{}, err
	}
	team, err := teamFlag(flags)
	if err != nil {
		return restoreArgs{}, err
	}
	a := restoreArgs{req: RestoreRequest{Team: team, Offline: flags[flagOffline] == flagTrue, RosterFromBackup: flags[flagFromBackup] == flagTrue}}
	if a.mode, err = restoreMode(flags); err != nil {
		return restoreArgs{}, err
	}
	_, backupGiven := flags[flagBackup]
	if err = restoreFlagsFit(a.mode, backupGiven, a.req.Offline || a.req.RosterFromBackup); err != nil {
		return restoreArgs{}, err
	}
	if backupGiven {
		if a.req.Backup, err = backupNumber(flags[flagBackup]); err != nil {
			return restoreArgs{}, err
		}
	}
	return a, nil
}

// fileOwner is an account's uid as the owner a file read is checked against.
func fileOwner(id int) (uint32, error) {
	if id < 0 || id > math.MaxUint32 {
		return 0, fmt.Errorf("uid %d is out of range", id)
	}
	return uint32(id), nil
}

// restoreMode is the one of --resume, --discard and --lost given, "" for a fresh restore.
func restoreMode(flags map[string]string) (string, error) {
	mode := ""
	for _, m := range []string{flagResume, flagDiscard, flagLost} {
		if flags[m] != flagTrue {
			continue
		}
		if mode != "" {
			return "", usagef("--resume, --discard and --lost exclude each other")
		}
		mode = m
	}
	return mode, nil
}

// restoreFlagsFit is whether a mode was given what it takes: --backup, and choices
// (--offline, --roster-from-backup).
func restoreFlagsFit(mode string, backupGiven, choices bool) error {
	switch {
	case (mode == flagResume || mode == flagDiscard) && (backupGiven || choices):
		return usagef("--%s takes only --team: the record decides the rest", mode)
	case mode == flagLost && choices:
		return usagef("--lost takes only --team and --backup")
	case (mode == "" || mode == flagLost) && !backupGiven:
		return usagef("--backup is required")
	}
	return nil
}

// backupNumber is --backup's value: a decimal number from 1, as Tuwunel numbers its backups.
func backupNumber(v string) (int64, error) {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 1 || n > maxBackupNumber || strconv.FormatInt(n, 10) != v {
		return 0, usagef("--backup is a backup number from 1 to %d", int64(maxBackupNumber))
	}
	return n, nil
}

// printLost is --lost (RS14): ctl/<team>/restore-<N>-lost.jsonl read by the trust rule as a
// file of the teamchat-ctl account's, every line one JSON object re-encoded compact, so no
// control byte reaches a terminal. Nothing is printed unless every line is one; no error
// quotes a line.
func printLost(e Env, sys *System, team paths.TeamID, n int64) term.ExitCode {
	ctl, err := sys.CtlIDs()
	var uid uint32
	if err == nil {
		uid, err = fileOwner(ctl.UID)
	}
	if err != nil {
		return fail(e.Stderr, ExitConfig, "the "+paths.CtlUser+" account: "+err.Error())
	}
	path := sys.Host.Root + paths.RestoreLostFile(team, n)
	raw, found, err := state.ReadOwnedFile(path, maxLost, uid)
	if err != nil {
		return fail(e.Stderr, term.ExitFailure, err.Error())
	}
	if !found {
		return fail(e.Stderr, term.ExitFailure, fmt.Sprintf("team %s has no export of the events backup %d lost (%s)", team, n, path))
	}
	var out bytes.Buffer
	if len(raw) > 0 {
		for i, line := range bytes.Split(bytes.TrimSuffix(raw, []byte("\n")), []byte("\n")) {
			v, perr := proto.Parse(line)
			if perr != nil || v.Kind() != proto.KindObject {
				return fail(e.Stderr, term.ExitFailure, fmt.Sprintf("%s: line %d is not one JSON object", path, i+1))
			}
			out.Write(v.Compact())
			out.WriteByte('\n')
		}
	}
	return answer(e, out.String())
}
