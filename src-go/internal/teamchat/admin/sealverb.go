package admin

import (
	"context"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/carry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Carrier is the carry verbs' engine over carry.Deps (SPEC-carry): each takes the team lock
// itself and answers its JSON document when it has one, beside its failure.
type Carrier interface {
	Seal(ctx context.Context, team paths.TeamID) ([]byte, error)
	Export(ctx context.Context, team paths.TeamID) ([]byte, error)
}

// CarryEngine is the Carrier over the host's carry.Deps.
type CarryEngine struct{ Deps carry.Deps }

// Seal is carry.Seal.
func (c CarryEngine) Seal(ctx context.Context, team paths.TeamID) ([]byte, error) {
	return carry.Seal(ctx, c.Deps, team)
}

// Export is carry.Export.
func (c CarryEngine) Export(ctx context.Context, team paths.TeamID) ([]byte, error) {
	return carry.Export(ctx, c.Deps, team)
}

// teamSeal is `team seal --team T` (SC1-SC7); --drill is the drill export's (64).
func teamSeal(ctx context.Context, e Env, args []string) term.ExitCode {
	if slices.Contains(args, "--"+flagDrill) {
		return failWith(e, usagef("team seal takes no --drill: a drill is team export --team %s --drill", teamArg(args)))
	}
	return carryVerb(ctx, e, args, Carrier.Seal)
}

// teamExport is `team export --team T` (SX1-SX7), the sealed export.
func teamExport(ctx context.Context, e Env, args []string) term.ExitCode {
	return carryVerb(ctx, e, args, Carrier.Export)
}

// carryVerb runs a carry verb that takes --team only: the engine asked of the wiring (78 when it
// has none or it fails), and its answer on stdout, printed even beside a failure.
func carryVerb(ctx context.Context, e Env, args []string, run func(Carrier, context.Context, paths.TeamID) ([]byte, error)) term.ExitCode {
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
	if sys.Carry == nil {
		return fail(e.Stderr, ExitConfig, "this wiring has no carry engine")
	}
	engine, err := sys.Carry()
	if err != nil {
		return fail(e.Stderr, ExitConfig, "the carry engine: "+err.Error())
	}
	out, err := run(engine, ctx, team)
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

// teamArg is the value after --team in args, "T" when there is none to name.
func teamArg(args []string) string {
	if i := slices.Index(args, "--"+flagTeam); i >= 0 && i+1 < len(args) {
		if team, err := paths.ParseTeamID(args[i+1]); err == nil {
			return team.String()
		}
	}
	return "T"
}
