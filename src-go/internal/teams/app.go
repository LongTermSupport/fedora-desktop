// Package teams is ccy-teams: the host command that creates, applies and removes agent teams.
// It parses the command line, checks that CCY is installed here and hands the subcommand to the
// package that owns it:
//
//	board   the table of teams, seats and people, and --list
//	picker  the interactive list (no argument), with the seats, login and create flows it calls
//	ops     apply, remove, backup and restore
//	create  the create wizard
//	find    for-checkout: the teams that work on this checkout's repository
//	choose  choose-seat and describe-seats: the launcher's seat and role chooser
//
// App only wires them over one host.Host and dispatches.
package teams

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/choose"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/config"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/create"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/find"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/login"
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
	Find   *find.Find
	Choose *choose.Choose
	Picker *picker.Picker
}

// NewApp is the production App: the real OS and the process's own streams.
func NewApp(cfg config.Config) *App { return Wire(host.New(cfg)) }

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
	return &App{
		Host: h, Board: b, Ops: o, Login: l, Seats: s, Create: c, Find: find.New(h), Choose: choose.New(h, kit, s),
		Picker: picker.New(h, kit, b, o, c, l, s),
	}
}

// Run dispatches args (without the program name) and returns the exit code. Output that could
// not be written turns a success into a failure.
func (a *App) Run(args []string) term.ExitCode {
	return a.Term.Finish(a.run(args), a.Out.Err())
}

func (a *App) run(args []string) term.ExitCode {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "create", "apply", "remove", "backup", "restore", "for-checkout", "record-seats",
		"choose-seat", "describe-seats":
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
		if err := a.NeedCheckout(); err != nil {
			return term.ExitFailure
		}
		return a.Create.Run(ctx, rest)
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

func (a *App) usageError(msg string) term.ExitCode {
	a.Print(a.Stderr, "ccy-teams: "+msg+" See ccy-teams --help.\n")
	return term.ExitUsage
}
