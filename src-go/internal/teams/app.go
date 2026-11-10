// Package teams is ccy-teams: the host command that creates, applies and removes agent teams.
// It parses the command line, checks that CCY is installed here and hands the subcommand to the
// package that owns it:
//
//	board   the table of teams, seats and people, --list, and --graph (the same as a Mermaid flowchart)
//	picker  the interactive list (no argument), with the seats, login and create flows it calls
//	ops     apply, remove, backup and restore
//	create  the create wizard
//	lxcadd  lxc-add: an LXC container's agent joins a team
//	find    for-checkout: the teams that work on this checkout's repository
//	choose  choose-seat and describe-seats: the launcher's seat and role questions
//	chooser choose-teams: which team the launcher's session joins, then its seat through choose
//	shutdown shutdown: every session of a team sent /exit, and the team row's menu line
//	restart restart: a team's desktop sessions resume their conversations, and the menu line
//	winddown wind-down: a team's desktop sessions sent /goal clear and the wind-down, and the menu line
//	settings the team row's Edit settings: the team's entry edited, saved and applied
//	membership internal membership-init: the launcher writes a session's desired membership
//	containername internal claim-container-name: the launcher's container name, claimed atomically
//	gocache internal go-cache: the launcher's Go caches, on the host and bounded
//	discover the one lookup of which session names a seat (records and desired memberships)
//	hot     session list, add, remove, move, role and repair: a running session's teams and roles
//	system  system: which bus (agent-bus or teamchat) each team of a launch is live on
//	seatsweep seats sweep: the owner's teamchat seat sweep, and the user units that run it
//
// App only wires them over one host.Host and dispatches.
package teams

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/placeissued"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/choose"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/chooser"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/config"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/containername"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/create"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/find"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/gocache"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/hot"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/login"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/lxcadd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/ops"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/picker"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/restart"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seats"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seatsweep"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/settings"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/shutdown"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/system"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamchatlaunch"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/winddown"
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
	// Shutdown ends every session of a team.
	Shutdown *shutdown.Shutdown
	// Restart restarts a team's desktop sessions, each resuming its conversation.
	Restart *restart.Restart
	// WindDown tells every desktop session of a team to wind down, its goal cleared first.
	WindDown *winddown.WindDown
	// Membership is `internal membership-init`, the launcher's.
	Membership *membership.Init
	// ContainerName is `internal claim-container-name`, the launcher's.
	ContainerName *containername.Claim
	// GoCache is `internal go-cache`, the launcher's.
	GoCache *gocache.Cache
	// Session is `ccy-teams session ...`, hot control of a running session.
	Session *hot.Hot
	// System is `ccy-teams system`, each team's live system, the launcher's.
	System *system.Cmd
	// Launch is the launcher's `internal launch-args`, `seat-check`, `seat-take` and
	// `bus-disconnected`, over both systems.
	Launch *teamchatlaunch.Verbs
	// Sweep is `ccy-teams seats`, the owner's teamchat seat sweep and its user units.
	Sweep *seatsweep.Cmd
	// Interrupted makes the run's context: Ctrl-C cancels it instead of ending the process,
	// so the sudo credential is still dropped (review L-A).
	Interrupted func(context.Context) (context.Context, context.CancelFunc)
}

// NewApp is the production App: the real OS and the process's own streams.
func NewApp(cfg config.Config) *App { return Wire(host.New(cfg)) }

// NewAppFor is NewApp for the command line args: a command whose stdout is a value its caller
// captures (choose-teams) is given a host that prints everything else on stderr, so a play's
// output cannot reach the value.
func NewAppFor(cfg config.Config, args []string) *App {
	if len(args) > 0 && (args[0] == subChooseTeams || args[0] == subLaunchSummary || args[0] == subSystem || args[0] == subSeats) ||
		len(args) > 1 && args[0] == subInternal && (args[1] == verbLaunchArgs || args[1] == verbSeatCheck) {
		return Wire(host.NewDiverted(cfg))
	}
	return NewApp(cfg)
}

// subChooseTeams is the subcommand whose stdout is its payload.
const subChooseTeams = "choose-teams"

// subLaunchSummary is the launch summary, whose stdout is its payload too.
const subLaunchSummary = "launch-summary"

// subSystem is `ccy-teams system`, whose stdout is its payload too.
const subSystem = "system"

// subSeats is `ccy-teams seats`: the sweep, and the units text the play captures from stdout.
const subSeats = "seats"

// The launcher's verbs whose stdout is their payload: the run arguments and the canonical list.
const (
	verbLaunchArgs = "launch-args"
	verbSeatCheck  = "seat-check"
)

// subChooseSeat is the launcher's seat and role question.
const subChooseSeat = "choose-seat"

// subDescribeSeats names a launch's seats with their roles.
const subDescribeSeats = "describe-seats"

// Wire builds the flows over h, after h has built every adapter it was not given.
func Wire(h *host.Host) *App {
	h.Wire()
	kit := flow.New(h)
	b := board.Wired(h)
	o := ops.New(h, b.Statuses, b.Changed)
	l := login.New(h, kit, b, o)
	o.SignInGap = l.SignInGap
	s := seats.New(h, kit, o)
	c := create.New(h, o, l)
	x := lxcadd.New(h, kit, o)
	pick := choose.New(h, kit, s)
	down := shutdown.New(h, kit, b)
	list := picker.New(h, kit, b, o, c, l, s, x)
	list.Shutdown = down
	again := restart.New(h, kit, b)
	list.Restart = again
	calm := winddown.New(h, kit, b)
	list.WindDown = calm
	list.Settings = settings.New(h, kit, o.ApplyTeams, o.Locked, s.ChangeRole)
	session := hot.New(h, kit, s.Roles)
	o.SessionsIn, o.TakeOut = session.SessionsIn, session.TakeOut
	list.Session = session
	chosen := chooser.New(h, pick.PickSeat, c.RunForChooser, o.ApplyTeams, o.Locked)
	chosen.Probe = pick.Probe
	membershipInit := membership.New(h)
	sys := newSystem(h)
	pick.Teamchat = func(ctx context.Context, team string) (bool, error) {
		lines, err := sys.Lines(ctx, []string{team})
		if err != nil {
			return false, err
		}
		return len(lines) == 1 && lines[0].System == system.Teamchat, nil
	}
	return &App{
		Host: h, Board: b, Ops: o, Login: l, Seats: s, Create: c, Lxc: x, Find: find.New(h), Choose: pick,
		Chooser: chosen,
		Picker:  list, Shutdown: down, Restart: again, WindDown: calm, Membership: membershipInit, ContainerName: containername.New(h), GoCache: gocache.New(h),
		Session: session,
		System:  sys,
		Launch:  newLaunch(h, membershipInit, sys),
		Sweep:   newSweep(h),
		Interrupted: func(ctx context.Context) (context.Context, context.CancelFunc) {
			return signal.NotifyContext(ctx, os.Interrupt)
		},
	}
}

// Run dispatches args (without the program name) and returns the exit code. Output that could
// not be written turns a success into a failure.
//
// A run that made sudo ready drops the credential last (security audit H1): a launcher may
// start an agent's session on this terminal next, and it must not inherit the owner's sudo.
// A launcher started by exec takes its seats through `agent-bus seat take`, which drops it too.
//
// The terminal's echo is held off from the first screen (Host.Echo) and its mode put back
// before the credential is dropped, a panic included.
func (a *App) Run(args []string) term.ExitCode {
	ctx, stop := a.Interrupted(context.Background())
	var code term.ExitCode
	err := a.Echo.Held(func() { code = a.run(ctx, args) })
	stop()
	if err != nil {
		a.Term.PrintError(err.Error() + "; run reset to see what you type.")
		if code == term.ExitOK {
			code = term.ExitFailure
		}
	}
	if err := a.Sudo.Forget(context.Background()); err != nil {
		a.Term.PrintError("the sudo credential could not be dropped (" + err.Error() +
			"); run sudo -k before starting an agent on this terminal.")
		if code == term.ExitOK {
			code = term.ExitFailure
		}
	}
	if why := term.Explain(code); why != "" {
		a.Print(a.Stderr, "ccy-teams: "+why+".\n")
	}
	return a.Term.Finish(code, a.Out.Err())
}

func (a *App) run(ctx context.Context, args []string) term.ExitCode {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "create", "lxc-add", "apply", "remove", "backup", "restore", "shutdown", subRestart, subWindDown, "for-checkout", "record-seats",
		subChooseSeat, subDescribeSeats, subChooseTeams, subLaunchSummary, subInternal, subSession, subSystem, subSeats:
	case "-l", "--list", subGraph:
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
	switch sub {
	case subInternal:
		return a.internal(ctx, args[1:])
	case subSession:
		return a.Session.Run(ctx, args[1:])
	case subSystem:
		return a.System.Run(ctx, args[1:])
	case subSeats:
		return a.Sweep.Run(ctx, args[1:])
	}
	return a.dispatch(ctx, sub, args[min(1, len(args)):])
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
	case "shutdown", subRestart, subWindDown:
		return a.teamSessions(ctx, sub, rest)
	case "for-checkout":
		return a.Find.Run(ctx, rest)
	case "record-seats":
		return a.Find.RecordSeats(ctx, rest)
	case subChooseTeams, subLaunchSummary, subChooseSeat, subDescribeSeats:
		return a.launchQuestions(ctx, sub, rest)
	case "-l", "--list", subGraph:
		return a.printBoard(ctx, sub)
	default:
		return a.Picker.Run(ctx)
	}
}

// launchQuestions is the commands the launcher asks of a person through: the summary, the team
// chooser, and the seat chooser and describer.
func (a *App) launchQuestions(ctx context.Context, sub string, rest []string) term.ExitCode {
	switch sub {
	case subLaunchSummary:
		return a.Chooser.Summary(ctx, rest)
	case subChooseSeat:
		return a.Choose.Seat(ctx, rest)
	case subDescribeSeats:
		return a.Choose.Describe(ctx, rest)
	}
	return a.Chooser.Run(ctx, rest)
}

// subGraph is `ccy-teams --graph`: the --list pass drawn as a Mermaid flowchart.
const subGraph = "--graph"

// printBoard is the two printouts of the board: the table (--list) or the flowchart (--graph).
func (a *App) printBoard(ctx context.Context, sub string) term.ExitCode {
	if sub == subGraph {
		return a.Board.Graph(ctx)
	}
	return a.Board.List(ctx)
}

// subRestart is `ccy-teams restart`, subWindDown `ccy-teams wind-down`.
const (
	subRestart  = "restart"
	subWindDown = "wind-down"
)

// teamSessions is the three commands that act on every desktop session of a team.
func (a *App) teamSessions(ctx context.Context, sub string, rest []string) term.ExitCode {
	switch sub {
	case subRestart:
		return a.Restart.Run(ctx, rest)
	case subWindDown:
		return a.WindDown.Run(ctx, rest)
	}
	return a.Shutdown.Run(ctx, rest)
}

// subInternal groups the commands only ccy's launcher runs; subSession is hot control.
const (
	subInternal = "internal"
	subSession  = "session"
)

// internal dispatches `ccy-teams internal <command>`.
func (a *App) internal(ctx context.Context, rest []string) term.ExitCode {
	if len(rest) > 0 {
		switch rest[0] {
		case "membership-init":
			return a.Membership.Run(ctx, rest[1:])
		case "restart-args":
			return a.Find.RestartArgs(ctx, rest[1:], a.Stdin, a.Payload)
		case "claim-container-name":
			return a.ContainerName.Run(ctx, rest[1:])
		case "go-cache":
			return a.GoCache.Run(ctx, rest[1:])
		case verbLaunchArgs, verbSeatCheck, "seat-take", "bus-disconnected":
			return a.Launch.Run(ctx, rest)
		}
	}
	return a.usageError("'internal' takes a command: internal membership-init [--kind=cc] <session> <checkout> <seats>, " +
		"internal restart-args <session>, internal claim-container-name <project> <suffix> <owner-pid>, " +
		"internal go-cache <project> [z], internal launch-args <session> <checkout> <seats> <Z or empty> -- <mounts...>, " +
		"internal seat-check <list> [--resolved], internal seat-take <list> [--no-prompt], " +
		"or internal bus-disconnected <status> <pingbus> <pingbus arguments>.")
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

// newSystem is `ccy-teams system` over h: the declared v1 teams from the host_vars, the status
// records under the status root, the working directory as the checkout.
func newSystem(h *host.Host) *system.Cmd {
	return &system.Cmd{
		Getenv: h.Getenv, Getwd: os.Getwd, Stdout: h.Payload, Stderr: h.Stderr,
		Declared: func(ctx context.Context) ([]string, error) {
			teams, err := h.Declare.Declared(ctx)
			if err != nil {
				return nil, err
			}
			names := make([]string, 0, len(teams))
			for _, t := range teams {
				names = append(names, t.Name)
			}
			return names, nil
		},
		Status: system.StatusRecord,
	}
}

// newLaunch is the launcher's verbs over h: membership-init for agent-bus, and for teamchat the
// owner's seat store on the real machine (placeissued.OS's clock, credential and reach), ccy's
// session registry as its session records and area B's disconnected verb run in process.
func newLaunch(h *host.Host, membershipInit *membership.Init, sys *system.Cmd) *teamchatlaunch.Verbs {
	return &teamchatlaunch.Verbs{
		Stdout: h.Payload, Stderr: h.Stderr, Getenv: h.Getenv, Getwd: os.Getwd,
		Resolve: func(ctx context.Context, checkout string, teams []string) ([]system.Line, error) {
			in := *sys
			in.Getwd = func() (string, error) { return checkout, nil }
			return in.Lines(ctx, teams)
		},
		Membership: membershipInit.Prepare, Status: system.StatusRecord, Exec: h.Echo.Execer(h.Execer),
		NowMS: func() int64 { return time.Now().UnixMilli() },
		Parts: storeParts(h),
		Taker: func(cfg seat.Config) teamchatlaunch.Taker {
			return teamchatlaunch.StoreTaker{Open: func() (*seat.Store, error) { return seat.Open(cfg) }}
		},
		Holder: func(cfg seat.Config) (teamchatlaunch.Holder, error) {
			store, err := seat.Open(cfg)
			if err != nil {
				return nil, err
			}
			return store, nil
		},
		Forget: func(session string) error {
			store, err := h.Registry()
			if err != nil {
				return err
			}
			return store.Remove(session)
		},
	}
}

// storeParts is the owner's seat store on the real machine, as the launcher and the sweep use
// it: placeissued.OS's clock, credential and reach, ccy's session registry as its session
// records and area B's disconnected verb run in process.
func storeParts(h *host.Host) seat.Config {
	machine := placeissued.OS("", "", paths.CheckoutKey{})
	return seat.Config{
		Clock: machine.Clock, Credential: machine.Credential, Reach: machine.Reach,
		Sessions: teamchatlaunch.RegistrySessions{Open: h.Registry}, Announcer: teamchatlaunch.Announcer{},
	}
}

// newSweep is `ccy-teams seats` over h: the sweep of every checkout of the owner's seat store.
func newSweep(h *host.Host) *seatsweep.Cmd {
	return &seatsweep.Cmd{Stdout: h.Payload, Stderr: h.Stderr, Getenv: h.Getenv, Parts: storeParts(h), Open: seatsweep.OpenStore}
}
