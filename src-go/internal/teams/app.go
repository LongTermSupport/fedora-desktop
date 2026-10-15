// Package teams is ccy-teams: the host command that creates, applies and removes agent teams.
// It parses the command line, checks that CCY is installed here and hands the subcommand to the
// package that owns it:
//
//	board   the table of teams, seats and people, and --list
//	picker  the interactive list (no argument), with the seats, login and create flows it calls
//	ops     apply, remove, backup and restore
//	create  the create wizard
//	lxcadd  lxc-add: an LXC container's agent joins a team
//	find    for-checkout: the teams that work on this checkout's repository
//	choose  choose-seat and describe-seats: the launcher's seat and role questions
//	chooser choose-teams: which team the launcher's session joins, then its seat through choose
//
// App only wires them over one host.Host and dispatches.
package teams

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/choose"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/chooser"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/config"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/create"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/find"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/login"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/lxcadd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/ops"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/picker"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seats"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

//go:embed usage.txt
var usage string

// App is one run of ccy-teams: the host and the flows built over it.
type App struct {
	*host.Host

	Board  *board.Collector
	Ops    *ops.Ops
	Login  *login.Login
	Seats  *seats.Seats
	Create *create.Wizard
	Lxc    *lxcadd.Adder
	Find   *find.Find
	Choose *choose.Choose
	// Chooser is choose-teams.
	Chooser *chooser.Chooser
	Picker  *picker.Picker
}

// NewApp is the production App: the real OS and the process's own streams.
func NewApp(cfg config.Config) *App { return Wire(host.New(cfg)) }

// NewAppFor is NewApp for the command line args: a command whose stdout is a value its caller
// captures (choose-teams) is given a host that prints everything else on stderr, so a play's
// output cannot reach the value.
func NewAppFor(cfg config.Config, args []string) *App {
	if len(args) > 0 && args[0] == subChooseTeams {
		return Wire(host.NewDiverted(cfg))
	}
	return NewApp(cfg)
}

// subChooseTeams is the subcommand whose stdout is its payload.
const subChooseTeams = "choose-teams"

// Wire builds the flows over h, after h has built every adapter it was not given.
func Wire(h *host.Host) *App {
	h.Wire()
	kit := flow.New(h)
	b := board.New(h)
	o := ops.New(h, b.Statuses, func() { b.ForgetDevices("") })
	l := login.New(h, kit, b, o)
	o.SignInGap = l.SignInGap
	s := seats.New(h, kit, o)
	c := create.New(h, o, l)
	x := lxcadd.New(h, kit, o)
	pick := choose.New(h, kit, s)
	return &App{
		Host: h, Board: b, Ops: o, Login: l, Seats: s, Create: c, Lxc: x, Find: find.New(h), Choose: pick,
		Chooser: chooser.New(h, pick.PickSeat, c.RunForChooser, o.ApplyTeams),
		Picker:  picker.New(h, kit, b, o, c, l, s, x),
	}
}

// Run dispatches args (without the program name) and returns the exit code. Output that could
// not be written turns a success into a failure.
//
// A run that made sudo ready drops the credential last (security audit H1): a launcher may
// start an agent's session on this terminal next, and it must not inherit the owner's sudo.
// A launcher started by exec takes its seats through `agent-bus seat take`, which drops it too.
func (a *App) Run(args []string) term.ExitCode {
	code := a.run(args)
	if err := a.Sudo.Forget(context.Background()); err != nil {
		a.Term.PrintError("the sudo credential could not be dropped (" + err.Error() +
			"); run sudo -k before starting an agent on this terminal.")
		if code == term.ExitOK {
			code = term.ExitFailure
		}
	}
	return a.Term.Finish(code, a.Out.Err())
}

func (a *App) run(args []string) term.ExitCode {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "create", "lxc-add", "apply", "remove", "backup", "restore", "for-checkout", "record-seats",
		"choose-seat", "describe-seats", subChooseTeams:
	case "-l", "--list":
		if len(args) > 1 {
			return a.usageError(fmt.Sprintf("'%s' takes no further arguments.", sub))
		}
	case "-h", "--help":
		a.Print(a.Stdout, usage)
		return term.ExitOK
	default:
		return a.usageError(fmt.Sprintf("unknown option '%s'.", sub))
	}
	if !a.CcyInstalled() {
		return term.ExitFailure
	}
	return a.dispatch(context.Background(), sub, args[min(1, len(args)):])
}

// dispatch hands the validated subcommand to the package that implements it.
func (a *App) dispatch(ctx context.Context, sub string, rest []string) term.ExitCode {
	switch sub {
	case "create":
		return a.inCheckout(func() term.ExitCode { return a.Create.Run(ctx, rest) })
	case "lxc-add":
		return a.inCheckout(func() term.ExitCode { return a.Lxc.Run(ctx, rest) })
	case "apply":
		return a.Ops.Apply(ctx, rest)
	case "remove":
		return a.Ops.Remove(ctx, rest)
	case "backup":
		return a.Ops.Backup(ctx, rest)
	case "restore":
		return a.Ops.Restore(ctx, rest)
	case "for-checkout":
		return a.Find.Run(ctx, rest)
	case "record-seats":
		return a.Find.RecordSeats(ctx, rest)
	case subChooseTeams:
		return a.Chooser.Run(ctx, rest)
	case "choose-seat":
		return a.Choose.Seat(ctx, rest)
	case "describe-seats":
		return a.Choose.Describe(ctx, rest)
	case "-l", "--list":
		return a.Board.List(ctx)
	default:
		return a.Picker.Run(ctx)
	}
}

// inCheckout runs a flow that needs to be in a checkout, or fails (NeedCheckout has said why).
func (a *App) inCheckout(run func() term.ExitCode) term.ExitCode {
	if err := a.NeedCheckout(); err != nil {
		return term.ExitFailure
	}
	return run()
}

func (a *App) usageError(msg string) term.ExitCode {
	a.Print(a.Stderr, "ccy-teams: "+msg+" See ccy-teams --help.\n")
	return term.ExitUsage
}
