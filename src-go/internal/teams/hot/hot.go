// Package hot is `ccy-teams session add` and `session remove`: a running ccy session put into a
// team, or taken out of one, while it runs (Plan 00203 SPEC-hot-control.md H17-H23, M3). The
// owner's ccy-teams on the host is the only writer of a session's desired membership: it writes
// the session record first (what a restore or restart replays, H5), then the desired file,
// atomically, into the directory the container has mounted read-only. The session's seat keeper
// follows it within 2 s; nothing is ever taken from the bus.
//
// Each command prints one line per step on stderr, marked ✓ (done), ✗ (failed) or - (already
// so), then the outcome. A step that fails after the record and the desired file were written
// restores both. Re-running a command whose result already holds prints every step as -.
package hot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sessprobe"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The exits a command uses beside term's (H17).
const (
	exitBusy   term.ExitCode = 75
	exitConfig term.ExitCode = 78
)

const (
	memberType = "podman"
	sessionTag = "ccy-session="
	markDone   = "✓"
	markFailed = "✗"
	markSkip   = "-"
	verbAdd    = "add"
	verbRemove = "remove"
)

// Waits bounds the waits of a change.
type Waits struct {
	// Poll is how often the seats, the report and the container are read again.
	Poll time.Duration
	// Apply bounds the wait for the keeper to apply a change (H22 step 8, H23 step 4).
	Apply time.Duration
	// Relaunch bounds the wait for a session's one relaunch on its first join.
	Relaunch time.Duration
}

// DefaultWaits are the production waits.
var DefaultWaits = Waits{Poll: 250 * time.Millisecond, Apply: 10 * time.Second, Relaunch: 180 * time.Second}

// Hot is the session command. Every dependency is a field, so a test stands in for each.
type Hot struct {
	Host  *host.Host
	Kit   *flow.Kit
	Now   func() time.Time
	Sleep func(time.Duration)
	Waits Waits
}

// New is the command over a host, on the wall clock.
func New(h *host.Host, kit *flow.Kit) *Hot {
	return &Hot{Host: h, Kit: kit, Now: time.Now, Sleep: time.Sleep, Waits: DefaultWaits}
}

const helpText = `Usage: ccy-teams session add <session> <team>[/<seat>] [--yes]
       ccy-teams session remove <session> <team> [--yes]

Only sessions started through ccy's tmux (listed by ccy-sessions), and cc sessions started with
--teams or --bus, can be changed while they run.

add puts the running session into the team, in the seat given or the lowest free one (a new
seat is created through agent-bus seat take, which asks for sudo), and waits for the session to
hold it. A session started on no team relaunches once, at its next idle moment, to accept team
messages; every later change is made while it runs. remove takes it out: the seat is released,
its sync position cleared (nothing posted while it is out is delivered on a later add), its
listening status expired and its goodbye posted. The seat's token stays valid: remove stops this
session's pingbus, it does not revoke. Each asks first; with no terminal, --yes is required.

Run one session command at a time per session: two at once on the same session can undo each
other (an add can be rolled back over another add that already said it was done).

Exit codes:
  0   done, or already so
  1   a step failed (the report says which)
  64  usage
  75  the seat is held by another session
  78  the session cannot be changed while it runs (relaunch it from ccy's tmux)
`

// Run is `ccy-teams session add|remove ...`.
func (x *Hot) Run(ctx context.Context, args []string) term.ExitCode {
	if len(args) == 0 {
		return x.usageError("'session' takes add or remove.")
	}
	switch args[0] {
	case "-h", "--help":
		x.Host.Print(x.Host.Payload, helpText)
		return term.ExitOK
	case verbAdd, verbRemove:
	default:
		return x.usageError("unknown session command '" + args[0] + "'.")
	}
	a, err := parse(args[0], args[1:])
	if err != nil {
		return x.usageError(err.Error())
	}
	if args[0] == verbAdd {
		return x.add(ctx, a)
	}
	return x.remove(ctx, a)
}

// request is a parsed command line.
type request struct {
	session, team, seat string
	yes                 bool
}

func parse(verb string, args []string) (request, error) {
	var r request
	var words []string
	for _, a := range args {
		switch {
		case a == "--yes":
			r.yes = true
		case strings.HasPrefix(a, "-"):
			return r, fmt.Errorf("unknown session %s option '%s'", verb, a)
		default:
			words = append(words, a)
		}
	}
	if len(words) != 2 {
		return r, fmt.Errorf("'session %s' takes a session and a team", verb)
	}
	r.session = words[0]
	if !membership.ValidSession(r.session) {
		return r, fmt.Errorf("'%s' is not a session name", r.session)
	}
	r.team = words[1]
	if verb == verbAdd {
		if team, seat, given := strings.Cut(words[1], "/"); given {
			r.team, r.seat = team, seat
		}
	}
	probe := r.seat
	if probe == "" {
		probe = "1"
	}
	if _, err := membership.ParseSeats(probe + "@" + r.team); err != nil {
		return r, fmt.Errorf("'%s' is not a team, or a team/seat: %w", words[1], err)
	}
	return r, nil
}

// target is a resolved, running, hot-controllable session.
type target struct {
	session   string
	container string
	dir       string
	desired   membership.Desired
	store     *registry.Store
}

func (t *target) checkout() string { return t.desired.Checkout }

func (t *target) seatsRoot() string {
	return filepath.Join(t.checkout(), ".claude", "ccy", "pingbus", "seats")
}

// resolve finds the session's container and membership (H22 step 1, D5, H35).
func (x *Hot) resolve(ctx context.Context, session string) (*target, term.ExitCode, bool) {
	runtime := x.Host.Getenv("XDG_RUNTIME_DIR")
	if !filepath.IsAbs(runtime) {
		x.mark(markFailed, "session "+session+": XDG_RUNTIME_DIR is not an absolute path, so its membership cannot be found; run from your desktop login session")
		return nil, term.ExitFailure, false
	}
	id, err := x.container(ctx, session)
	if err != nil {
		x.mark(markFailed, "session "+session+": "+err.Error())
		return nil, term.ExitFailure, false
	}
	if id == "" {
		x.mark(markFailed, "session "+session+": no running container is labelled "+sessionTag+session+
			": it is not running, or it was started outside ccy's tmux, so it cannot be changed while it runs")
		return nil, exitConfig, false
	}
	store, err := x.Host.Registry()
	if err != nil {
		x.mark(markFailed, "session "+session+": "+err.Error())
		return nil, term.ExitFailure, false
	}
	if _, recErr := store.Get(session); recErr != nil {
		x.mark(markFailed, "session "+session+": "+recErr.Error()+"; only a session ccy-sessions knows can be changed while it runs")
		return nil, exitConfig, false
	}
	dir := membership.Dir(runtime, session)
	desired, err := membership.Read(dir)
	if err == nil && desired.Kind != "ccy" {
		err = fmt.Errorf("its membership is a %s session's", desired.Kind)
	}
	if err != nil {
		why := err.Error()
		if errors.Is(err, os.ErrNotExist) {
			why = "it was launched without the agent team bus kit (--no-bus, or a ccy older than 3.99)"
		}
		x.mark(markFailed, "session "+session+": "+why+"; relaunch it from ccy's tmux to change it while it runs")
		return nil, exitConfig, false
	}
	x.mark(markDone, "session "+session+": running, can be hot-controlled")
	return &target{session: session, container: id, dir: dir, desired: desired, store: store}, term.ExitOK, true
}

// container is the ID of the running container labelled with the session, or "" for none.
func (x *Hot) container(ctx context.Context, session string) (string, error) {
	out, err := sessprobe.LabelledContainers(ctx, x.Host.Runner, sessionTag+session)
	if err != nil {
		return "", fmt.Errorf("podman could not list its container: %w", err)
	}
	var running []string
	for line := range strings.Lines(out) {
		id, state, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if ok && id != "" && state == "running" {
			running = append(running, id)
		}
	}
	if len(running) > 1 {
		return "", fmt.Errorf("%d running containers carry the label %s%s", len(running), sessionTag, session)
	}
	if len(running) == 0 {
		return "", nil
	}
	return running[0], nil
}

// commit writes the record, then the desired file with one more generation (H5). note, when
// given, says why on each line (a rollback's "restored").
func (x *Hot) commit(t *target, seats []membership.Seat, farewell []membership.Farewell, note string) error {
	if err := x.writeRecord(t, seats, note); err != nil {
		return err
	}
	return x.writeDesired(t, seats, farewell, note)
}

// writeRecord makes seats the session record's --teams.
func (x *Hot) writeRecord(t *target, seats []membership.Seat, note string) error {
	list := membership.Canonical(seats)
	if err := t.store.SetTeams(t.session, list); err != nil {
		x.mark(markFailed, "record: "+err.Error())
		return err
	}
	words := "--teams " + list
	if list == "" {
		words = "no --teams"
	}
	if note != "" {
		words = note + " (" + words + ")"
	}
	x.mark(markDone, "record: "+words)
	return nil
}

// writeDesired writes the desired file with one more generation.
func (x *Hot) writeDesired(t *target, seats []membership.Seat, farewell []membership.Farewell, note string) error {
	d := t.desired
	d.Seats, d.Farewell = slices.Clone(seats), slices.Clone(farewell)
	d.Generation++
	d.WrittenMS = x.Now().UnixMilli()
	if err := membership.Write(t.dir, d); err != nil {
		x.mark(markFailed, "membership: "+err.Error())
		return err
	}
	t.desired = d
	words := fmt.Sprintf("generation %d written", d.Generation)
	if note != "" {
		words += " (" + note + ")"
	}
	x.mark(markDone, "membership: "+words)
	return nil
}

// seatHeld is whether the checkout's seat lock reads held, and the seat's handle.
func (x *Hot) seatHeld(ctx context.Context, t *target, team, seat string) (bool, string, error) {
	seats, _, err := x.Host.Bus.SeatList(ctx, t.checkout())
	if err != nil {
		return false, "", fmt.Errorf("agent-bus seat list in %s: %w", t.checkout(), err)
	}
	for _, s := range seats {
		if s.Team == team && s.Seat == seat {
			return s.Held, s.Handle, nil
		}
	}
	return false, "", nil
}

// confirm is the one question, or --yes; with no terminal only --yes.
func (x *Hot) confirm(ctx context.Context, question string, yes bool) (term.ExitCode, bool) {
	if yes {
		return term.ExitOK, true
	}
	if !x.Host.IsTerminal(x.Host.Stdin) || !x.Host.IsTerminal(x.Host.Term.Err) {
		return x.usageError("a change to a running session asks first: confirm it with --yes when there is no terminal."), false
	}
	sure, err := x.Kit.AskYes(ctx, question, false)
	if err != nil {
		if code, ok := flow.ExitCodeOf(err); ok {
			return code, false
		}
		return term.ExitFailure, false
	}
	if !sure {
		x.say("Nothing was changed.")
		return term.ExitOK, false
	}
	return term.ExitOK, true
}

// elapsed is the time since start, in seconds.
func (x *Hot) elapsed(start time.Time) float64 { return x.Now().Sub(start).Seconds() }

func (x *Hot) mark(m, text string) { x.Host.Print(x.Host.Stderr, m+" "+text+"\n") }

func (x *Hot) say(text string) { x.Host.Print(x.Host.Stderr, text+"\n") }

func (x *Hot) usageError(msg string) term.ExitCode {
	x.say("ccy-teams: " + msg + " See ccy-teams session --help.")
	return term.ExitUsage
}

// lastLine is the last non-empty line a command printed on stderr, for a ✗ line.
func lastLine(err error) string {
	if exitErr, ok := errors.AsType[*execx.ExitError](err); ok {
		lines := strings.Split(strings.TrimSpace(exitErr.Stderr), "\n")
		if last := strings.TrimSpace(lines[len(lines)-1]); last != "" {
			return last
		}
	}
	if cmdErr, ok := errors.AsType[*agentbus.CommandError](err); ok {
		return cmdErr.Error()
	}
	return err.Error()
}
