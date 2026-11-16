package admin

import (
	"context"
	"errors"
	"strconv"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore/drill"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Driller is the restore drill (issue #324) over the machine: Run is drill.Run on the declared
// drill port, Clean is drill.Clean, each scratch team under its own lock.
type Driller interface {
	Run(ctx context.Context, port int) (drill.Outcome, error)
	Clean(ctx context.Context) ([]paths.TeamID, error)
}

// The drill verb's flags.
const (
	flagPort  = "port"
	flagClean = "clean"
)

// The drill port's range: never a privileged port.
const (
	minDrillPort = 1024
	maxDrillPort = 65535
)

// drillHelp is `restore-drill --help`, its payload.
const drillHelp = `usage: ` + paths.AdminBinary + ` restore-drill --port <port> [--clean]
       ` + paths.AdminBinary + ` restore-drill --clean

Proves restore on this machine: a scratch team (rd- and 12 hex) is applied on the declared drill
port, which listens on loopback only, seeded, backed up, changed, restored once forced to fail and
once to the end, each clause of host checks H-C5 and H-C15 checked, then removed with purge. The
proof record restore needs is written only when every clause attempted passed; the pool seat and
the held session are recorded skipped, so restore stays refused until they can pass. The record
is evidence about this machine and is never copied to another.

The drill refuses to run on a machine that holds any other team, or an earlier drill's leftover:
--clean removes the leftovers (a recorded restore discarded, then the team purged) and nothing
else. stdout is one JSON document. Exit 0 the drill passed (or --clean alone cleaned), 1 a check
or an operation failed (no record written), 64 usage, 70 refused.
`

// restoreDrill is `restore-drill --port P [--clean]`: --clean removes the leftovers of an earlier
// drill, then --port runs a drill on the declared drill port. Its one JSON document says what
// was cleaned and, for a drill, its team, each clause's verdict, what failed and whether the
// proof record was written. A refusal is 70, a failed check 1 with the document printed.
func restoreDrill(ctx context.Context, e Env, args []string) term.ExitCode {
	port, clean, err := drillArgs(args)
	if errors.Is(err, errHelp) {
		return answer(e, drillHelp)
	}
	sys, code := e.system()
	if code != term.ExitOK {
		return code
	}
	if err != nil {
		return failWith(e, err)
	}
	if sys.Drill == nil {
		return fail(e.Stderr, ExitConfig, "this wiring has no restore drill")
	}
	d, err := sys.Drill(ctx)
	if err != nil {
		return failWith(e, err)
	}
	members := []proto.Member{proto.M("v", proto.Int(wireMajor))}
	var cleaned []paths.TeamID
	if clean {
		if cleaned, err = d.Clean(ctx); err != nil {
			return failWith(e, err)
		}
	}
	members = append(members, proto.M("cleaned", teamArray(cleaned)))
	if port == 0 {
		return answer(e, string(proto.Obj(members...).Compact())+"\n")
	}
	out, err := d.Run(ctx, port)
	if err != nil && !errors.Is(err, drill.ErrFailed) {
		return failWith(e, err)
	}
	members = append(members, outcomeMembers(out)...)
	if code = answer(e, string(proto.Obj(members...).Compact())+"\n"); code != term.ExitOK {
		return code
	}
	if err != nil {
		return failWith(e, err)
	}
	return term.ExitOK
}

// errHelp is --help asked.
var errHelp = errors.New("help")

// drillArgs is the grammar: --port P and --clean, at least one; port is 0 when not given.
func drillArgs(args []string) (int, bool, error) {
	flags, err := parseFlags(args, []string{flagPort}, flagClean, "help")
	if err != nil {
		return 0, false, err
	}
	if flags["help"] == flagTrue {
		if len(flags) > 1 {
			return 0, false, usagef("--help takes no other flag")
		}
		return 0, false, errHelp
	}
	clean := flags[flagClean] == flagTrue
	v, given := flags[flagPort]
	if !given {
		if !clean {
			return 0, false, usagef("--port P runs the drill, --clean removes an earlier drill's leftovers")
		}
		return 0, true, nil
	}
	port, err := strconv.Atoi(v)
	if err != nil || strconv.Itoa(port) != v || port < minDrillPort || port > maxDrillPort {
		return 0, false, usagef("--port is the declared drill port, a number from %d to %d", minDrillPort, maxDrillPort)
	}
	return port, clean, nil
}

func teamArray(ids []paths.TeamID) proto.Value {
	items := make([]proto.Value, 0, len(ids))
	for _, id := range ids {
		items = append(items, proto.Str(id.String()))
	}
	return proto.Arr(items...)
}

// outcomeMembers is a drill's outcome in the document, each clause in the record's order.
func outcomeMembers(out drill.Outcome) []proto.Member {
	var checks, failed []proto.Member
	for _, c := range restore.Clauses {
		if v, ok := out.Checks[c]; ok {
			checks = append(checks, proto.M(string(c), proto.Str(string(v))))
		}
		if f, ok := out.Failed[c]; ok {
			failed = append(failed, proto.M(string(c), lineArray(f)))
		}
	}
	return []proto.Member{
		proto.M("team", proto.Str(out.Team.String())), proto.M("checks", proto.Obj(checks...)),
		proto.M("failed", proto.Obj(failed...)), proto.M("proof", proto.Bool(out.Written)),
	}
}
