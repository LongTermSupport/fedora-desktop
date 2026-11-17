package admin

import (
	"context"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/carry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Carrier is the carry verbs' engine over carry.Deps (SPEC-carry): each takes the team lock
// itself and answers its JSON document when it has one, beside its failure.
type Carrier interface {
	Seal(ctx context.Context, team paths.TeamID) ([]byte, error)
	Export(ctx context.Context, team paths.TeamID) ([]byte, error)
	Drill(ctx context.Context, team paths.TeamID) ([]byte, error)
	Import(ctx context.Context, team paths.TeamID, flags carry.ImportFlags) ([]byte, error)
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

// Drill is carry.Drill.
func (c CarryEngine) Drill(ctx context.Context, team paths.TeamID) ([]byte, error) {
	return carry.Drill(ctx, c.Deps, team)
}

// Import is carry.Import.
func (c CarryEngine) Import(ctx context.Context, team paths.TeamID, flags carry.ImportFlags) ([]byte, error) {
	return carry.Import(ctx, c.Deps, team, flags)
}

// flagSHA256 is the import's --sha256, the whole-archive digest the export answered (SI1).
const flagSHA256 = "sha256"

// teamImport is `team import --team T --sha256 <hex> [--drill] [--resume|--discard]` (SI1-SI13):
// --sha256 is required and 64 lower-case hex digits, and --resume with --discard is two ways
// on; each is 64. The inbox is fixed, so no path is taken (sc-D6).
func teamImport(ctx context.Context, e Env, args []string) term.ExitCode {
	return carryFlags(ctx, e, args, []string{flagSHA256}, []string{flagDrill, flagResume, flagDiscard}, func(flags map[string]string) (carryRun, error) {
		f := carry.ImportFlags{
			SHA256: flags[flagSHA256], Drill: flags[flagDrill] == flagTrue,
			Resume: flags[flagResume] == flagTrue, Discard: flags[flagDiscard] == flagTrue,
		}
		if !isSHA256(f.SHA256) {
			return nil, usagef("--sha256 is required: the export's whole-archive digest, 64 lower-case hex digits")
		}
		if f.Resume && f.Discard {
			return nil, usagef("--resume and --discard are two ways on: give one")
		}
		return func(c Carrier, ctx context.Context, team paths.TeamID) ([]byte, error) { return c.Import(ctx, team, f) }, nil
	})
}

// isSHA256 is 64 lower-case hex digits.
func isSHA256(s string) bool {
	return len(s) == 64 && strings.Trim(s, "0123456789abcdef") == ""
}

// teamSeal is `team seal --team T` (SC1-SC7); --drill is the drill export's (64).
func teamSeal(ctx context.Context, e Env, args []string) term.ExitCode {
	if slices.Contains(args, "--"+flagDrill) {
		return failWith(e, usagef("team seal takes no --drill: a drill is team export --team %s --drill", teamArg(args)))
	}
	return carryVerb(ctx, e, args, Carrier.Seal)
}

// teamExport is `team export --team T` (SX1-SX7), the sealed export, and with --drill the drill
// export (SD1-SD5).
func teamExport(ctx context.Context, e Env, args []string) term.ExitCode {
	if slices.Contains(args, "--"+flagDrill) {
		return carryVerb(ctx, e, args, Carrier.Drill, flagDrill)
	}
	return carryVerb(ctx, e, args, Carrier.Export)
}

// carryVerb runs a carry verb that takes --team and the bool flags given: the engine asked of
// the wiring (78 when it has none or it fails), and its answer on stdout, printed even beside a
// failure.
func carryVerb(ctx context.Context, e Env, args []string, run carryRun, bools ...string) term.ExitCode {
	return carryFlags(ctx, e, args, nil, bools, func(map[string]string) (carryRun, error) { return run, nil })
}

// carryRun is one carry verb's call of the engine.
type carryRun func(Carrier, context.Context, paths.TeamID) ([]byte, error)

// carryFlags is carryVerb for a verb with value flags past --team: build turns the flags into
// the engine's call, or a usage error.
func carryFlags(ctx context.Context, e Env, args, values, bools []string, build func(map[string]string) (carryRun, error)) term.ExitCode {
	sys, code := e.system()
	if code != term.ExitOK {
		return code
	}
	flags, err := parseFlags(args, append([]string{flagTeam}, values...), bools...)
	if err != nil {
		return failWith(e, err)
	}
	team, err := teamFlag(flags)
	if err != nil {
		return failWith(e, err)
	}
	run, err := build(flags)
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
