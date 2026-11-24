package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	v1 "github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/migrate/v1"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// HistoryExport is the export of a v1 team's room history and its verification (Plan 00178
// SPEC-v1-export.md): *v1.Exporter's methods.
type HistoryExport interface {
	Export(ctx context.Context, req v1.Request) (v1.Result, error)
	Verify(team, stamp string) (string, error)
}

// The migrate export verbs' names and flags.
const (
	verbMigrateExport       = "migrate export"
	verbMigrateExportVerify = "migrate export-verify"
	flagCopyTo              = "copy-to"
	flagCopyOwner           = "copy-owner"
	flagExport              = "export"
)

// namesV1Team are the verbs whose --team names a v1 team, which the carry gate (a v2 team's
// records) does not judge: a v1 team need not have a v2 team of the same name.
var namesV1Team = map[string]bool{verbMigrateExport: true, verbMigrateExportVerify: true}

// migrateExport is `migrate export --team <v1-team> [--copy-to <dir> --copy-owner <user>]`
// (E1, E17, E19, E20): the record's path and one line per room on stdout, progress on stderr; a
// usage error is 64, any refusal or failure 1.
func migrateExport(ctx context.Context, e Env, args []string) term.ExitCode {
	flags, err := parseFlags(args, []string{flagTeam, flagCopyTo, flagCopyOwner})
	if err != nil {
		return failWith(e, err)
	}
	team, err := v1TeamFlag(flags)
	if err != nil {
		return failWith(e, err)
	}
	_, hasTo := flags[flagCopyTo]
	_, hasOwner := flags[flagCopyOwner]
	if hasTo != hasOwner {
		return fail(e.Stderr, term.ExitUsage, "--copy-to and --copy-owner come together or not at all")
	}
	x, code := e.historyExport()
	if code != term.ExitOK {
		return code
	}
	res, err := x.Export(ctx, v1.Request{Team: team, CopyTo: flags[flagCopyTo], CopyOwner: flags[flagCopyOwner], Progress: e.Stderr})
	if err != nil {
		return fail(e.Stderr, term.ExitFailure, err.Error())
	}
	var b strings.Builder
	b.WriteString(res.Record + "\n")
	for _, r := range res.Rooms {
		fmt.Fprintf(&b, "%s %s %d\n", r.Purpose, r.RoomID, r.Events)
	}
	return answer(e, b.String())
}

// migrateExportVerify is `migrate export-verify --team <v1-team> [--export <stamp>]` (E18): the
// record's path on stdout when every file matches, else each mismatch on its own line and 1.
func migrateExportVerify(_ context.Context, e Env, args []string) term.ExitCode {
	flags, err := parseFlags(args, []string{flagTeam, flagExport})
	if err != nil {
		return failWith(e, err)
	}
	team, err := v1TeamFlag(flags)
	if err != nil {
		return failWith(e, err)
	}
	stamp, given := flags[flagExport]
	if given {
		if err := v1.CheckStamp(stamp); err != nil {
			return fail(e.Stderr, term.ExitUsage, "--export: "+err.Error())
		}
	}
	x, code := e.historyExport()
	if code != term.ExitOK {
		return code
	}
	path, err := x.Verify(team, stamp)
	if m, ok := errors.AsType[*v1.MismatchError](err); ok && m != nil {
		return fail(e.Stderr, term.ExitFailure, "the record "+m.Record+" does not match its digest:\n  "+strings.Join(m.Problems, "\n  "))
	}
	if err != nil {
		return fail(e.Stderr, term.ExitFailure, err.Error())
	}
	return answer(e, path+"\n")
}

// v1TeamFlag is --team, required, a v1 team name (E1).
func v1TeamFlag(flags map[string]string) (string, error) {
	team, given := flags[flagTeam]
	if !given {
		return "", usagef("--team is required")
	}
	if err := v1.CheckTeam(team); err != nil {
		return "", usagef("--team: %v", err)
	}
	return team, nil
}

// historyExport is the System's export seam; one that cannot be built is a failure (1).
func (e Env) historyExport() (HistoryExport, term.ExitCode) {
	sys, code := e.system()
	if code != term.ExitOK {
		return nil, code
	}
	if sys.HistoryExport == nil {
		return nil, fail(e.Stderr, term.ExitFailure, errUnwired.Error())
	}
	x, err := sys.HistoryExport()
	if err != nil {
		return nil, fail(e.Stderr, term.ExitFailure, "the history export: "+err.Error())
	}
	return x, term.ExitOK
}
