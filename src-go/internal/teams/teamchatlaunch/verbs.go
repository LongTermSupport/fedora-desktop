package teamchatlaunch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/system"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// AgentBusCommand is the v1 seat command the launcher ran before these verbs, which
// play-agent-bus.yml installs on every desktop.
const AgentBusCommand = "agent-bus"

// restartStatus is the container's exit status for a relaunch the supervisor asked for
// (ccy's CCY_RESTART_EXIT_STATUS, SPEC-n3 RS2).
const restartStatus = "75"

// The v1 membership mount (lib/agent-bus-membership.bash's, now printed from here).
const (
	v1MembershipMount = "/run/ccy/membership"
	v1SeatsRoot       = "/.claude/ccy/pingbus/seats"
)

// Holder is the part of the seat store the launcher's exit uses (seat.Store).
type Holder interface {
	Holdings() ([]seat.Holding, error)
	Release(ctx context.Context, r seat.Release) error
	Announce(ctx context.Context, s seat.Seat, session paths.SessionName) error
	Restarted(ctx context.Context, s seat.Seat, session paths.SessionName) error
	Close() error
}

// Verbs is the launcher's `ccy-teams internal` commands over both systems (LAUNCH-ccy.md):
// launch-args, seat-check, seat-take and bus-disconnected. For a launch on agent-bus each is what
// the bash did before; for teamchat each is the v2 launch's.
type Verbs struct {
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string
	Getwd  func() (string, error)
	// Resolve is each team's live system for a checkout (system.Cmd.Lines there).
	Resolve func(ctx context.Context, checkout string, teams []string) ([]system.Line, error)
	// Membership writes a v1 session's membership and is its directory (membership.Init.Prepare).
	Membership func(ctx context.Context, args []string) (string, term.ExitCode)
	Status     func(paths.TeamID) (status.Record, bool, error)
	NowMS      func() int64
	Exec       execx.Execer
	// Parts is the seat store's clock, credential, session records, announcer and reach; the
	// verbs fill in where the store is and whose checkout it allocates.
	Parts seat.Config
	// Taker takes a v2 launch's seats from the store of cfg.
	Taker func(cfg seat.Config) Taker
	// Holder opens the store of cfg for the exit.
	Holder func(cfg seat.Config) (Holder, error)
	// Forget removes a session's record (seam 54), before its seats are released.
	Forget func(session string) error
}

// errUsage is a call the launcher never makes: the reason is said, exit 64.
var errUsage = errors.New("usage")

const verbsUsage = "internal launch-args <session> <checkout> <seats> <Z or empty> -- <mounts...>, " +
	"internal seat-check <list> [--resolved], internal seat-take <list> [--no-prompt], " +
	"or internal bus-disconnected <status> <pingbus> seat disconnected --root <dir> --session <name> --since <ms>"

// Run dispatches one of the four verbs.
func (v *Verbs) Run(ctx context.Context, args []string) term.ExitCode {
	if len(args) == 0 {
		return v.fail("internal", fmt.Errorf("%w: takes %s", errUsage, verbsUsage))
	}
	verb, rest := args[0], args[1:]
	var err error
	switch verb {
	case "launch-args":
		var code term.ExitCode
		if code, err = v.launchArgs(ctx, rest); err == nil {
			return code
		}
	case "seat-check", "seat-take":
		err = v.seat(ctx, verb, rest)
	case "bus-disconnected":
		err = v.busDisconnected(ctx, rest)
	default:
		err = fmt.Errorf("%w: %q is not one of %s", errUsage, verb, verbsUsage)
	}
	if err != nil {
		return v.fail(verb, err)
	}
	return term.ExitOK
}

// fail says err and is its exit code.
func (v *Verbs) fail(verb string, err error) term.ExitCode {
	if _, werr := fmt.Fprintf(v.Stderr, "ccy-teams: internal %s: %v.\n", verb, err); werr != nil {
		return term.ExitFailure
	}
	return codeOf(err)
}

// codeOf is the launcher's exit code for a refusal: 64 a list or call it cannot use, 75 a seat
// held or the store busy, 78 what stops the seats being had here, 1 anything else.
func codeOf(err error) term.ExitCode {
	switch {
	case errors.Is(err, errUsage), errors.Is(err, system.ErrRefused), errors.Is(err, ErrSeat):
		return term.ExitUsage
	case errors.Is(err, seat.ErrHeld), errors.Is(err, seat.ErrBusy):
		return term.ExitBusy
	case errors.Is(err, system.ErrUndecided), errors.Is(err, ErrMount), errors.Is(err, seat.ErrReturn),
		errors.Is(err, seat.ErrRetired), errors.Is(err, seat.ErrIncomplete), errors.Is(err, seat.ErrLeftover),
		execx.IsNotInstalled(err):
		return term.ExitConfig
	}
	return term.ExitFailure
}

// teamsOf is the team of each item of a --teams list (spaces dropped, as agent-bus drops them),
// and false when an item is not one a v2 team could be: agent-bus then says what is wrong.
func teamsOf(list string) ([]string, bool) {
	if list == "" {
		return nil, false
	}
	var teams []string
	for item := range strings.SplitSeq(list, ",") {
		item = strings.Join(strings.Fields(item), "")
		_, team, found := strings.Cut(item, "@")
		if !found {
			team = item
		}
		if _, err := paths.ParseTeamID(team); err != nil {
			return nil, false
		}
		teams = append(teams, team)
	}
	return teams, true
}

// systemOf is the one system of a list's teams in checkout: agent-bus for a list a v2 team
// cannot be in, or the system Resolve decides (which refuses a list on both).
func (v *Verbs) systemOf(ctx context.Context, checkout, list string) (string, error) {
	teams, ok := teamsOf(list)
	if !ok {
		return system.AgentBus, nil
	}
	lines, err := v.Resolve(ctx, checkout, teams)
	if err != nil {
		return "", err
	}
	for _, l := range lines {
		if l.System == system.Teamchat {
			return system.Teamchat, nil
		}
	}
	return system.AgentBus, nil
}

// launchArgs is `internal launch-args <session> <checkout> <seats> <Z or empty> -- <mounts...>`:
// the root the exit is given, then each argument podman run is given for the bus, one a line.
func (v *Verbs) launchArgs(ctx context.Context, args []string) (term.ExitCode, error) {
	if len(args) < 5 || args[4] != "--" || (args[3] != "" && args[3] != "Z") {
		return 0, fmt.Errorf("%w: takes <session> <checkout> <seats> <Z or empty> -- <mounts...>", errUsage)
	}
	session, checkout, list, relabel, mounts := args[0], args[1], args[2], args[3] == "Z", args[5:]
	sys, err := v.systemOf(ctx, checkout, list)
	if err != nil {
		return 0, err
	}
	if sys == system.AgentBus {
		dir, code := v.Membership(ctx, []string{session, checkout, list})
		if code != term.ExitOK {
			return code, nil
		}
		z := ""
		if relabel {
			z = ",Z"
		}
		return term.ExitOK, v.print(checkout+v1SeatsRoot, "-v", dir+":"+v1MembershipMount+":ro"+z, "-e", "PINGBUS_MEMBERSHIP="+v1MembershipMount)
	}
	name, err := paths.ParseSessionName(session)
	if err != nil {
		return 0, fmt.Errorf("%w: the session %q: %w", errUsage, session, err)
	}
	store, key, state, err := system.Here(v.Getenv, func() (string, error) { return checkout, nil })
	if err != nil {
		return 0, err
	}
	launch := &Launch{Store: store, Checkout: key, Taker: v.Taker(v.config(state, key)), Status: v.Status, NowMS: v.NowMS}
	run, err := launch.Run(ctx, name, list, mounts, relabel)
	if err != nil {
		return 0, err
	}
	return term.ExitOK, v.print(append([]string{store.Control(name)}, run...)...)
}

// config is the seat store of the state home for checkout key.
func (v *Verbs) config(state string, key paths.CheckoutKey) seat.Config {
	cfg := v.Parts
	cfg.StateHome, cfg.RuntimeDir, cfg.Checkout = state, v.Getenv("XDG_RUNTIME_DIR"), key
	return cfg
}

func (v *Verbs) print(lines ...string) error {
	_, err := io.WriteString(v.Stdout, strings.Join(lines, "\n")+"\n")
	return err
}

// seat is `internal seat-check <list> [--resolved]` and `internal seat-take <list> [--no-prompt]`:
// agent-bus's own `seat check` or `seat take` for a v1 list; for a v2 list, check prints the
// canonical list and take does nothing, since launch-args took the seats.
func (v *Verbs) seat(ctx context.Context, verb string, args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("%w: takes <list> and at most one option", errUsage)
	}
	wd, err := v.Getwd()
	if err != nil {
		return fmt.Errorf("the working directory: %w", err)
	}
	sys, err := v.systemOf(ctx, wd, args[0])
	if err != nil {
		return err
	}
	if sys == system.AgentBus {
		return v.Exec.Exec(execx.Cmd{Name: AgentBusCommand, Args: append([]string{"seat", strings.TrimPrefix(verb, "seat-")}, args...)})
	}
	if verb == "seat-take" {
		return nil
	}
	seats, err := membership.ParseSeats(strings.Join(strings.Fields(args[0]), ""))
	if err != nil {
		return fmt.Errorf("%w: a teamchat team is named with its seat, <seat>@<team> (%w)", ErrSeat, err)
	}
	items := make([]string, 0, len(seats))
	for _, s := range seats {
		items = append(items, s.Seat+"@"+s.Team)
	}
	return v.print(strings.Join(items, ","))
}

// busDisconnected is `internal bus-disconnected <status> <pingbus> seat disconnected --root <dir>
// --session <name> --since <ms>`, the launcher's exit (seams 54, 27). A v1 root runs pingbus with
// those arguments, as before. A v2 session's root is its control directory; then status, the
// container's exit status (empty while it still ran: the launcher left on a signal), decides:
// 75, a relaunch, records each seat's ended run (SPEC-n3 RS3) and keeps it; another status ends
// the session, its record removed before its seats are released; none announces each seat and
// keeps it, as for a crash.
func (v *Verbs) busDisconnected(ctx context.Context, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("%w: takes <status> <pingbus> and pingbus's arguments", errUsage)
	}
	exitStatus, pingbus, rest := args[0], args[1], args[2:]
	root, session, err := rootAndSession(rest)
	if err != nil {
		return err
	}
	wd, err := v.Getwd()
	if err != nil {
		return fmt.Errorf("the working directory: %w", err)
	}
	store, key, state, err := system.Here(v.Getenv, func() (string, error) { return wd, nil })
	if err != nil {
		return err
	}
	if filepath.Clean(root) != store.Control(session) {
		return v.Exec.Exec(execx.Cmd{Name: pingbus, Args: rest})
	}
	holder, err := v.Holder(v.config(state, key))
	if err != nil {
		return err
	}
	return errors.Join(v.end(ctx, holder, session, exitStatus), holder.Close())
}

// rootAndSession reads --root and --session from pingbus's arguments.
func rootAndSession(args []string) (string, paths.SessionName, error) {
	var root, session string
	for i := 0; i+1 < len(args); i++ {
		switch args[i] {
		case "--root":
			root = args[i+1]
		case "--session":
			session = args[i+1]
		}
	}
	if root == "" || session == "" {
		return "", paths.SessionName{}, fmt.Errorf("%w: pingbus's arguments name no --root and --session", errUsage)
	}
	name, err := paths.ParseSessionName(session)
	if err != nil {
		return "", paths.SessionName{}, fmt.Errorf("%w: the session %q: %w", errUsage, session, err)
	}
	return root, name, nil
}

// end acts on every complete holding of the session; each failure is said, and the others go on.
func (v *Verbs) end(ctx context.Context, holder Holder, session paths.SessionName, exitStatus string) error {
	holdings, err := holder.Holdings()
	if err != nil {
		return fmt.Errorf("the seats of session %s: %w", session, err)
	}
	_, numeric := strconv.Atoi(exitStatus)
	over := numeric == nil && exitStatus != restartStatus
	if over {
		if err := v.Forget(session.String()); err != nil {
			return fmt.Errorf("remove the record of session %s, so its seats are kept: %w", session, err)
		}
	}
	var errs []error
	for _, h := range holdings {
		if !h.Complete || h.Record.Session != session {
			continue
		}
		var err error
		switch {
		case over:
			err = holder.Release(ctx, seat.Release{Seat: h.Seat, Session: session})
		case exitStatus == restartStatus:
			err = holder.Restarted(ctx, h.Seat, session)
		default:
			err = holder.Announce(ctx, h.Seat, session)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("seat %s of team %s: %w", h.Seat.Name, h.Seat.Team, err))
		}
	}
	for _, err := range errs {
		if _, werr := fmt.Fprintf(v.Stderr, "ccy-teams: internal bus-disconnected: %v.\n", err); werr != nil {
			return werr
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%d seat(s) of session %s were not settled (each reason is above); the seat sweep tries again", len(errs), session)
	}
	return nil
}
