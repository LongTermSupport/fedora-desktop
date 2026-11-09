package admin

import (
	"context"
	"errors"
	"fmt"
	"os/user"
	"strconv"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/element"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// HealthTick is the health tick the `health` verb runs (*health.Tick).
type HealthTick interface {
	RunAll(ctx context.Context) ([]health.Outcome, error)
	One(ctx context.Context, team paths.TeamID) (health.Outcome, error)
}

// healthVerb is `health --all` (VB1, the timer's) and `health <team>` (VB2): one stderr line per
// team, exit 0 unless a team failed; a team dropped (its lock busy, n2f-D9) is no failure.
func healthVerb(ctx context.Context, e Env, args []string) term.ExitCode {
	sys, code := e.system()
	if code != term.ExitOK {
		return code
	}
	var team paths.TeamID
	all := len(args) == 1 && args[0] == "--all"
	if !all {
		id, err := paths.ParseTeamID(firstOf(args))
		if len(args) != 1 || err != nil {
			return fail(e.Stderr, term.ExitUsage, "health takes --all or one team")
		}
		team = id
	}
	tick, err := sys.Health()
	if err != nil {
		return fail(e.Stderr, ExitConfig, "the health tick: "+err.Error())
	}
	var outcomes []health.Outcome
	if all {
		outcomes, err = tick.RunAll(ctx)
	} else {
		var one health.Outcome
		one, err = tick.One(ctx, team)
		outcomes = []health.Outcome{one}
	}
	if err != nil {
		return failWith(e, err)
	}
	return printOutcomes(e, outcomes)
}

func firstOf(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

func printOutcomes(e Env, outcomes []health.Outcome) term.ExitCode {
	code := term.ExitOK
	for _, o := range outcomes {
		line := o.Team.String() + " " + o.Result
		switch o.Result {
		case health.ResultDropped:
			line += ": " + o.Why
		case health.ResultFailed:
			line += ": " + fmt.Sprint(o.Err)
			code = term.ExitFailure
		}
		if _, err := fmt.Fprintln(e.Stderr, oneLine(line)); err != nil {
			return term.ExitFailure
		}
	}
	return code
}

// elementVerb is `element check` (VB3) and `element apply [--consent]` (VB4, n2f-D8): the
// desktop user's profiles for the real uid, its home from the account database, the approvals
// and names root wrote. An input or a run element refuses is 64, any other failure 1.
func elementVerb(applying bool) func(context.Context, Env, []string) term.ExitCode {
	return func(_ context.Context, e Env, args []string) term.ExitCode {
		sys, code := e.system()
		if code != term.ExitOK {
			return code
		}
		var bools []string
		if applying {
			bools = []string{"consent"}
		}
		flags, err := parseFlags(args, nil, bools...)
		if err != nil {
			return failWith(e, err)
		}
		uid := e.Proc.Getuid()
		home, err := sys.Home(uid)
		if err != nil {
			return fail(e.Stderr, term.ExitFailure, "the home of uid "+strconv.Itoa(uid)+": "+err.Error())
		}
		root := sys.Host.Root
		env := element.Env{
			Root: e.Proc.Geteuid() == 0, Home: home, UID: uid,
			Approvals: declare.FileApprovals{Path: func(id paths.TeamID) string { return root + paths.ApprovedFile(id) }, GID: -1},
			Names:     element.StatusNames{Path: func(id paths.TeamID) string { return root + paths.StatusFile(id) }},
			Stdout:    e.Stdout, Stderr: e.Stderr,
		}
		switch {
		case !applying:
			err = element.Check(env, e.Stdin)
		case flags["consent"] == flagTrue:
			if err = element.WriteConsent(env, sys.Host.Clock.Now()); err == nil {
				err = element.Apply(env, e.Stdin)
			}
		default:
			err = element.Apply(env, e.Stdin)
		}
		if errors.Is(err, element.ErrRefused) {
			return fail(e.Stderr, term.ExitUsage, err.Error())
		}
		if err != nil {
			return fail(e.Stderr, term.ExitFailure, err.Error())
		}
		return term.ExitOK
	}
}

// LookupHome is the home directory of uid's account in the account database, never $HOME
// (VB3).
func LookupHome(uid int) (string, error) {
	u, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		return "", err
	}
	return u.HomeDir, nil
}
