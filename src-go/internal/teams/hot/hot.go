// Package hot is `ccy-teams session`: a running ccy session put into a team, taken out of one,
// moved between two, given a role, its seat keeper repaired, and the running sessions listed,
// all while it runs (Plan 00203 SPEC-hot-control.md H17-H30, M3 and M4). The owner's ccy-teams
// on the host is the only writer of a session's desired membership: it writes the session
// record first (what a restore or restart replays, H5), then the desired file, atomically, into
// the directory the container has mounted read-only. The session's seat keeper follows it
// within 2 s; nothing is ever taken from the bus.
//
// Each command prints one line per step on stderr, marked ✓ (done), ✗ (failed) or - (already
// so), then the outcome. A step that fails after the record and the desired file were written
// restores both. Re-running a command whose result already holds prints every step as -.
// One change runs at a time per session and one seat allocation at a time per checkout, each
// under a lock in $XDG_RUNTIME_DIR/ccy, out of every container's reach (H20, lock.go). The
// picker's Running sessions view (M5) runs these same commands. A cc session started with
// --teams or --bus is changed the same way (H31, cc.go).
package hot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sessprobe"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/discover"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/roles"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The exits a command uses beside term's (H17).
const (
	exitBusy   term.ExitCode = 75
	exitConfig term.ExitCode = 78
)

const (
	memberType = "podman"
	// kindCcy is the kind of a ccy session's membership (a cc session's is membership.KindCC).
	kindCcy    = "ccy"
	sessionTag = "ccy-session="
	// stateRunning is podman's word for a running container.
	stateRunning = "running"
	markDone     = "✓"
	markFailed   = "✗"
	markSkip     = "-"
	verbAdd      = "add"
	verbRemove   = "remove"
	verbMove     = "move"
	verbRole     = "role"
	verbRepair   = "repair"
	verbList     = "list"

	optYes      = "--yes"
	optRole     = "--role="
	optCoord    = "--coordinator="
	optDemote   = "--demote-current"
	optPark     = "--park"
	coordDemote = "demote"
	coordHandTo = "hand-to="
)

// Waits bounds the waits of a change.
type Waits struct {
	// Poll is how often the seats, the report and the container are read again.
	Poll time.Duration
	// Apply bounds the wait for the keeper to apply a change (H22 step 8, H23 step 4), for the
	// session to be told a role (H25) and for a repaired keeper to hold its seats (H10).
	Apply time.Duration
	// Relaunch bounds the wait for a session's one relaunch on its first join.
	Relaunch time.Duration
}

// DefaultWaits are the production waits.
var DefaultWaits = Waits{Poll: 250 * time.Millisecond, Apply: 10 * time.Second, Relaunch: 180 * time.Second}

// Hot is the session command. Every dependency is a field, so a test stands in for each.
type Hot struct {
	Host *host.Host
	Kit  *flow.Kit
	// Roles makes a role change once it is agreed, for a member of any kind.
	Roles *roles.Setter
	Now   func() time.Time
	Sleep func(time.Duration)
	Waits Waits
}

// New is the command over a host, on the wall clock; r changes roles.
func New(h *host.Host, kit *flow.Kit, r *roles.Setter) *Hot {
	return &Hot{Host: h, Kit: kit, Roles: r, Now: time.Now, Sleep: time.Sleep, Waits: DefaultWaits}
}

const helpText = `Usage: ccy-teams session list
       ccy-teams session add <session> <team>[/<seat>] [--role=worker|coordinator] [--demote-current] [--yes]
       ccy-teams session remove <session> <team> [--coordinator=demote|hand-to=<seat>] [--park] [--yes]
       ccy-teams session move <session> <from-team> <to-team>[/<seat>] [--coordinator=demote|hand-to=<seat>] [--yes]
       ccy-teams session role <session>|<handle> <team> worker|coordinator [--demote-current] [--yes]
       ccy-teams session repair <session> [--yes]

Only sessions started through ccy's tmux (listed by ccy-sessions), and cc sessions started with
--teams or --bus, can be changed while they run. A cc session runs as you, not in a container, so
it could change its own membership file: for cc this is guidance, not a wall, as its Element
consent says.

list shows every running session ccy's tmux started: its checkout, its teams (seat and the role
it was told) and its state: ok, a team whose homeserver is down, a seat not held, unguarded (its
seat keeper stopped: repair it), or old or --no-bus (relaunch it to change it while it runs). On
a terminal it is a table; otherwise SESSION lines (name, kind, checkout, team/seat/role,...,
state), tab-separated.

add puts the running session into the team, in the seat given or the lowest free one (a new
seat is created through agent-bus seat take, which asks for sudo), and waits for the session to
hold it. --role sets the seat's role on the way. A team has one coordinator: --role=coordinator
on a team with none is offered plainly; on a team that has one it is a takeover, which names the
current coordinator and whether it runs, asks with no as the default, and with no terminal (or
with --yes) needs --demote-current, which makes that seat a worker first. A session started on no team
relaunches once, at its next idle moment, to accept team messages; every later change is made
while it runs. A cc session started with --bus cannot relaunch itself: its first add keeps the
seat and exits 1, saying to end cc and start it again from ccy-sessions.

remove takes it out: the seat is released, its sync position cleared (nothing posted while it is
out is delivered on a later add), its listening status expired and its goodbye posted. If the
seat is the team's coordinator, --coordinator says what becomes of the role: demote (no one has
it) or hand-to=<seat> (another member of the team, by its seat or its full handle). --park also
parks the seat's handle and removes the seat from the checkout. The seat's token stays valid:
remove stops this session's pingbus, it does not revoke.

move adds the session to the new team, then removes it from the old one. If the add fails the
old team is untouched; if the remove fails, the session is in both, and the same move again
finishes it. A move within one team is refused: remove, then add.

role makes the session's seat in the team a worker or the coordinator (sudo agent-bus
set-role), and waits for the session to be told. Making it the coordinator of a team that has
one is the same takeover as add's, with the same --demote-current. Given a member's handle
(<repo>.<seat>+<host>.<type>) in place of a session, it changes the role of a member of any
kind, an LXC container's agent, a host, docker or vm member as much as a session, by the same
question and the same path as the member's row in ccy-teams: a member whose role is declared
(an agent_bus_lxc_members entry) has its entry changed too, then its team applied. Its running
session is told by pingbus; there is no session record to wait on.

repair starts the session's seat keeper again when it has stopped (list says unguarded), and
waits for it to take back every seat. A cc session's keeper ends with its claude, so a cc session
is not repaired: end it and start it again from ccy-sessions.

Each change asks first; with no terminal, --yes is required. One change runs at a time per
session, and one seat allocation at a time per checkout: a second one is refused (exit 75),
naming what holds it.

Exit codes:
  0   done, or already so
  1   a step failed (the report says which)
  64  usage
  75  the seat is held or named by another session, or another change is in progress
  78  the session cannot be changed while it runs (relaunch it from ccy's tmux)
`

// Run is `ccy-teams session ...`.
func (x *Hot) Run(ctx context.Context, args []string) term.ExitCode {
	if len(args) == 0 {
		return x.usageError("'session' takes list, add, remove, move, role or repair.")
	}
	switch args[0] {
	case "-h", "--help":
		x.Host.Print(x.Host.Payload, helpText)
		return term.ExitOK
	case verbList:
		if len(args) > 1 {
			return x.usageError("'session list' takes no further arguments.")
		}
		return x.list(ctx)
	case verbAdd, verbRemove, verbMove, verbRole, verbRepair:
	default:
		return x.usageError("unknown session command '" + args[0] + "'.")
	}
	r, err := parse(args[0], args[1:])
	if err != nil {
		return x.usageError(err.Error())
	}
	if r.verb == verbRole && roles.IsHandle(r.session) {
		return x.memberRole(ctx, r)
	}
	t, code, ok := x.resolve(ctx, r)
	if !ok {
		return code
	}
	code = x.change(ctx, t, r)
	if err := t.lock.release(); err != nil {
		x.mark(markFailed, "session "+t.session+": "+err.Error())
		if code == term.ExitOK {
			code = term.ExitFailure
		}
	}
	return code
}

// change runs one verb on a resolved session, under its change lock.
func (x *Hot) change(ctx context.Context, t *target, r request) term.ExitCode {
	switch r.verb {
	case verbAdd:
		return x.addTo(ctx, t, r, r.team, r.seat)
	case verbRemove:
		return x.removeFrom(ctx, t, r, r.team)
	case verbMove:
		return x.move(ctx, t, r)
	case verbRole:
		return x.setRole(ctx, t, r)
	default:
		return x.repair(ctx, t, r)
	}
}

// request is a parsed command line.
type request struct {
	verb, session, team, seat string
	// to and toSeat are a move's new team and its seat, if given.
	to, toSeat string
	// role is add's --role, or role's worker|coordinator.
	role string
	// coordinator is remove's and move's --coordinator: "", demote, or hand-to=<seat>.
	coordinator              string
	demoteCurrent, park, yes bool
}

// optionsOf is which options each verb takes, and how many words.
var optionsOf = map[string]struct {
	words   int
	options []string
}{
	verbAdd:    {2, []string{optYes, optRole, optDemote}},
	verbRemove: {2, []string{optYes, optCoord, optPark}},
	verbMove:   {3, []string{optYes, optCoord}},
	verbRole:   {3, []string{optYes, optDemote}},
	verbRepair: {1, []string{optYes}},
}

// handToValue is what hand-to may name: a seat, or a member's full handle.
var handToValue = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)

func parse(verb string, args []string) (request, error) {
	r := request{verb: verb}
	spec := optionsOf[verb]
	var words []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			words = append(words, a)
			continue
		}
		name := a
		if i := strings.Index(a, "="); i >= 0 {
			name = a[:i+1]
		}
		if !slices.Contains(spec.options, name) {
			return r, fmt.Errorf("'session %s' takes no option '%s'", verb, a)
		}
		if err := r.option(name, strings.TrimPrefix(a, name)); err != nil {
			return r, err
		}
	}
	if len(words) != spec.words {
		return r, fmt.Errorf("'session %s' takes %s", verb, map[string]string{
			verbAdd: "a session and a team", verbRemove: "a session and a team", verbMove: "a session, the team it leaves and the team it joins",
			verbRole: "a session, a team and worker or coordinator", verbRepair: "a session",
		}[verb])
	}
	r.session = words[0]
	if !membership.ValidSession(r.session) {
		return r, fmt.Errorf("'%s' is not a session name", r.session)
	}
	return r, r.names(words)
}

// option takes one option's value.
func (r *request) option(name, value string) error {
	switch name {
	case optYes:
		r.yes = true
	case optDemote:
		r.demoteCurrent = true
	case optPark:
		r.park = true
	case optRole:
		if value != agentbus.RoleWorker && value != agentbus.RoleCoordinator {
			return fmt.Errorf("--role is worker or coordinator, not '%s'", value)
		}
		r.role = value
	case optCoord:
		seat, hand := strings.CutPrefix(value, coordHandTo)
		if value != coordDemote && (!hand || !handToValue.MatchString(seat)) {
			return fmt.Errorf("--coordinator is demote or hand-to=<seat>, not '%s'", value)
		}
		r.coordinator = value
	}
	return nil
}

// names reads the team words of the verb.
func (r *request) names(words []string) error {
	var err error
	switch r.verb {
	case verbAdd:
		r.team, r.seat, err = teamAndSeat(words[1], true)
	case verbRemove:
		r.team, _, err = teamAndSeat(words[1], false)
	case verbMove:
		if r.team, _, err = teamAndSeat(words[1], false); err == nil {
			r.to, r.toSeat, err = teamAndSeat(words[2], true)
		}
		if err == nil && r.to == r.team {
			err = errors.New("a move within one team is refused: remove, then add")
		}
	case verbRole:
		r.team, _, err = teamAndSeat(words[1], false)
		if err == nil && words[2] != agentbus.RoleWorker && words[2] != agentbus.RoleCoordinator {
			err = fmt.Errorf("a role is worker or coordinator, not '%s'", words[2])
		}
		r.role = words[2]
	}
	return err
}

// teamAndSeat is a team, or a team/seat where a seat may be given.
func teamAndSeat(word string, seatAllowed bool) (string, string, error) {
	team, seat := word, ""
	if seatAllowed {
		if t, s, given := strings.Cut(word, "/"); given {
			team, seat = t, s
		}
	}
	probe := seat
	if probe == "" {
		probe = "1"
	}
	if _, err := membership.ParseSeats(probe + "@" + team); err != nil {
		return "", "", fmt.Errorf("'%s' is not a team, or a team/seat: %w", word, err)
	}
	return team, seat, nil
}

// holderOf is what a run writes into a lock it holds.
func holderOf(r request) string {
	what := "ccy-teams session " + r.verb + " " + r.session
	if r.team != "" {
		what += " " + r.team
	}
	if r.to != "" {
		what += " " + r.to
	}
	return what + " (pid " + strconv.Itoa(os.Getpid()) + ")"
}

// target is a resolved, running, hot-controllable session, under its change lock: a ccy session
// in its container, or a cc session in its tmux pane (kind cc, no container).
type target struct {
	session   string
	kind      string
	container string
	pane      int
	dir       string
	desired   membership.Desired
	store     *registry.Store
	lock      *lock
	runtime   string
}

func (t *target) cc() bool { return t.kind == membership.KindCC }

// memberType is the member type of the session's seats.
func (t *target) memberType() string {
	if t.cc() {
		return ccMemberType
	}
	return memberType
}

func (t *target) checkout() string { return t.desired.Checkout }

func (t *target) seatsRoot() string {
	return filepath.Join(t.checkout(), ".claude", "ccy", "pingbus", "seats")
}

// resolve finds the session's container and membership (H22 step 1, D5, H35), and takes its
// change lock (H20).
func (x *Hot) resolve(ctx context.Context, r request) (*target, term.ExitCode, bool) {
	session := r.session
	runtime := x.Host.Getenv("XDG_RUNTIME_DIR")
	if !filepath.IsAbs(runtime) {
		x.mark(markFailed, "session "+session+": XDG_RUNTIME_DIR is not an absolute path, so its membership cannot be found; run from your desktop login session")
		return nil, term.ExitFailure, false
	}
	t := &target{session: session, kind: membership.KindCcy, runtime: runtime}
	if code, found := x.locate(ctx, t); !found {
		return nil, code, false
	}
	store, err := x.Host.Registry()
	if err != nil {
		x.mark(markFailed, "session "+session+": "+err.Error())
		return nil, term.ExitFailure, false
	}
	rec, recErr := store.Get(session)
	if recErr != nil {
		x.mark(markFailed, "session "+session+": "+recErr.Error()+"; only a session ccy-sessions knows can be changed while it runs")
		return nil, exitConfig, false
	}
	if !discover.LaunchedWithBus(rec) {
		x.notOnBus(t)
		return nil, exitConfig, false
	}
	dir := membership.DirOf(runtime, t.kind, session)
	t.dir, t.store = dir, store
	if info, statErr := os.Lstat(dir); statErr != nil || !info.IsDir() {
		x.notKit(session, os.ErrNotExist)
		return nil, exitConfig, false
	}
	lockPath, err := changePath(runtime, session)
	if err == nil {
		t.lock, err = takeLock(lockPath, holderOf(r))
	}
	if busy, ok := errors.AsType[*busyError](err); ok {
		x.mark(markFailed, "session "+session+": another change to it is in progress: "+busy.holder)
		return nil, exitBusy, false
	}
	if err != nil {
		x.mark(markFailed, "session "+session+": "+err.Error())
		return nil, term.ExitFailure, false
	}
	t.desired, err = membership.Read(dir)
	if err == nil && t.desired.Kind != t.kind {
		err = fmt.Errorf("its membership is a %s session's", t.desired.Kind)
	}
	if err != nil {
		x.notKit(session, err)
		x.letGo(t.lock)
		return nil, exitConfig, false
	}
	x.mark(markDone, "session "+session+": running, can be hot-controlled")
	return t, term.ExitOK, true
}

// locate finds the running session: its labelled container, or else a cc session's tmux pane.
func (x *Hot) locate(ctx context.Context, t *target) (term.ExitCode, bool) {
	id, err := x.container(ctx, t.session)
	if err != nil {
		x.mark(markFailed, "session "+t.session+": "+err.Error())
		return term.ExitFailure, false
	}
	if id != "" {
		t.container = id
		return term.ExitOK, true
	}
	if isCC(t.runtime, t.session) {
		return x.locateCC(ctx, t)
	}
	x.mark(markFailed, "session "+t.session+": no running container is labelled "+sessionTag+t.session+
		": it is not running, or it was started outside ccy's tmux, so it cannot be changed while it runs")
	return exitConfig, false
}

// notOnBus says why a session whose own launch left the bus out cannot be changed, though a
// membership is under its name: an earlier session's, left behind (Fable's M6 review B1).
func (x *Hot) notOnBus(t *target) {
	started, again := "with --no-bus", "relaunch it from ccy's tmux without --no-bus"
	if t.cc() {
		started, again = "without --teams or --bus", "start it again with cc --bus or cc --teams"
	}
	x.mark(markFailed, "session "+t.session+": it was started "+started+", so it is not on the agent team bus "+
		"(the membership under its name is an earlier session's); "+again+" to change it while it runs")
}

// notKit says why a session with no readable membership cannot be changed while it runs.
func (x *Hot) notKit(session string, err error) {
	why := err.Error()
	if errors.Is(err, os.ErrNotExist) {
		why = "it was launched without the agent team bus kit (--no-bus, or a ccy older than 3.99)"
	}
	x.mark(markFailed, "session "+session+": "+why+"; relaunch it from ccy's tmux to change it while it runs")
}

// letGo releases a lock on a path that is failing anyway, saying so if it could not.
func (x *Hot) letGo(l *lock) {
	if err := l.release(); err != nil {
		x.mark(markFailed, err.Error())
	}
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
		if ok && id != "" && state == stateRunning {
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

// ended is whether the session's container (a cc session's pane) has stopped or been replaced
// since it was resolved.
func (x *Hot) ended(ctx context.Context, t *target) bool {
	if t.cc() {
		return x.ccEnded(ctx, t)
	}
	id, err := x.container(ctx, t.session)
	return err == nil && id != t.container
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
	set := t.store.SetTeams
	if t.cc() {
		set = t.store.SetCCTeams
	}
	if err := set(t.session, list); err != nil {
		x.mark(markFailed, "record: "+err.Error())
		return err
	}
	words := "--teams " + list
	switch {
	case list == "" && t.cc():
		words = "no --teams (cc keeps --bus)"
	case list == "":
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

// dropDesired removes the desired file of a session that has ended (H28).
func (x *Hot) dropDesired(t *target) error {
	if err := membership.RemoveDesired(t.dir); err != nil {
		x.mark(markFailed, "membership: "+err.Error())
		return err
	}
	x.mark(markDone, "membership: removed (the session is not running; its next launch writes it)")
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

// interactive is whether a question can be asked.
func (x *Hot) interactive() bool {
	return x.Host.IsTerminal(x.Host.Stdin) && x.Host.IsTerminal(x.Host.Term.Err)
}

// confirm is the one question, or --yes; with no terminal only --yes.
func (x *Hot) confirm(ctx context.Context, question string, yes bool) (term.ExitCode, bool) {
	if yes {
		return term.ExitOK, true
	}
	if !x.interactive() {
		return x.usageError("a change to a running session asks first: confirm it with --yes when there is no terminal."), false
	}
	return x.ask(ctx, question, false)
}

// ask is a yes/no question on the terminal; no is "Nothing was changed." and exit 0.
func (x *Hot) ask(ctx context.Context, question string, defaultYes bool) (term.ExitCode, bool) {
	sure, err := x.Kit.AskYes(ctx, question, defaultYes)
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
