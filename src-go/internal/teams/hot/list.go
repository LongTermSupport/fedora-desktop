package hot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sessprobe"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// keeperLog is where a session's seat keeper writes, inside its container.
const keeperLog = "/tmp/pingbus-home/keeper.log"

// Session is one running session as `session list` and the picker's Running sessions view
// show it (section 5.1).
type Session struct {
	Name, Kind, Checkout, State string
	Teams                       []Cell
	// Controllable is a session whose membership can be changed while it runs (D5, H35): it has
	// a readable desired membership of a ccy session.
	Controllable bool
	// Unguarded is a session whose seat keeper has stopped (H10): repair starts it again.
	Unguarded bool
}

// Cell is one team of a session: its seat and the role the session was told ("-" before).
type Cell struct{ Team, Seat, Role string }

// OnlyTmuxLine is what the list and the picker's view say once about who can be changed (H35).
const OnlyTmuxLine = "Only sessions started through ccy's tmux (the ones ccy-sessions lists) can be changed here."

// listCache keeps what several sessions share: a checkout's seats and a team's homeserver.
type listCache struct {
	seats map[string][]agentbus.Seat
	up    map[string]bool
}

// Sessions is every running session ccy's tmux started, by name, read as `session list` reads
// them: its membership, its keeper, its seats and the roles it was told.
func (x *Hot) Sessions(ctx context.Context) ([]Session, error) {
	runtime := x.Host.Getenv("XDG_RUNTIME_DIR")
	if !filepath.IsAbs(runtime) {
		return nil, errors.New("XDG_RUNTIME_DIR is not an absolute path, so no session's membership can be found; run from your desktop login session")
	}
	out, err := sessprobe.LabelledSessions(ctx, x.Host.Runner)
	if err != nil {
		return nil, errors.New("podman could not list the sessions' containers: " + lastLine(err))
	}
	running := runningSessions(out)
	names := make([]string, 0, len(running))
	for name := range running {
		names = append(names, name)
	}
	slices.Sort(names)
	cache := listCache{seats: map[string][]agentbus.Seat{}, up: map[string]bool{}}
	rows := make([]Session, 0, len(names))
	for _, name := range names {
		rows = append(rows, x.session(ctx, runtime, running[name], name, cache))
	}
	return rows, nil
}

// list is `session list` (H17, H18, section 5.1): every running session ccy's tmux started.
func (x *Hot) list(ctx context.Context) term.ExitCode {
	rows, err := x.Sessions(ctx)
	if err != nil {
		x.Host.Term.PrintError(err.Error() + ".")
		return term.ExitFailure
	}
	if x.Host.IsTerminal(x.Host.Payload) {
		return x.table(rows)
	}
	for _, row := range rows {
		cells := make([]string, 0, len(row.Teams))
		for _, c := range row.Teams {
			cells = append(cells, c.Team+"/"+c.Seat+"/"+c.Role)
		}
		teams := strings.Join(cells, ",")
		if teams == "" {
			teams = "-"
		}
		x.Host.Print(x.Host.Payload, strings.Join([]string{"SESSION", row.Name, row.Kind, row.Checkout, teams, row.State}, "\t")+"\n")
	}
	return term.ExitOK
}

// table is the list for a person: aligned columns, and what cannot be listed said once.
func (x *Hot) table(rows []Session) term.ExitCode {
	heading, lines, err := SessionLines(rows, x.Host.Home)
	if err != nil {
		x.Host.Term.PrintError(err.Error())
		return term.ExitFailure
	}
	x.Host.Print(x.Host.Payload, strings.Join(append([]string{heading}, lines...), "\n")+"\n")
	x.say(OnlyTmuxLine)
	return term.ExitOK
}

// SessionLines is the heading and one line per session, in aligned columns: the table of
// `session list` on a terminal, and the rows of the picker's Running sessions view.
func SessionLines(rows []Session, home string) (string, []string, error) {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	lines := make([]string, 0, 1+len(rows))
	lines = append(lines, "SESSION\tCHECKOUT\tTEAMS (seat, role)\tSTATE")
	for _, row := range rows {
		cells := make([]string, 0, len(row.Teams))
		for _, c := range row.Teams {
			cells = append(cells, c.Team+" ("+c.Seat+", "+c.Role+")")
		}
		teams := strings.Join(cells, " · ")
		if teams == "" {
			teams = "-"
		}
		checkout := row.Checkout
		if checkout != "-" {
			checkout = term.AbbrevHome(checkout, home)
		}
		lines = append(lines, row.Name+"\t"+checkout+"\t"+teams+"\t"+row.State)
	}
	_, err := w.Write([]byte(strings.Join(lines, "\n") + "\n"))
	if err = errors.Join(err, w.Flush()); err != nil {
		return "", nil, fmt.Errorf("lay out the sessions: %w", err)
	}
	laid := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
	return laid[0], laid[1:], nil
}

// session reads one running session: its membership, its keeper, its seats and roles.
func (x *Hot) session(ctx context.Context, runtime, container, name string, cache listCache) Session {
	row := Session{Name: name, Kind: kindCcy, Checkout: "-"}
	d, err := membership.Read(membership.Dir(runtime, name))
	if errors.Is(err, os.ErrNotExist) {
		row.State = "old or --no-bus: relaunch it to change it while it runs"
		return row
	}
	if err != nil {
		row.State = "membership unreadable: " + err.Error()
		return row
	}
	row.Kind, row.Checkout, row.Controllable = d.Kind, d.Checkout, d.Kind == kindCcy
	seats, seatsErr := x.checkoutSeats(ctx, d.Checkout, cache)
	rep, repErr := membership.ReadReport(d.Checkout, name)
	if repErr != nil {
		rep = membership.Report{}
	}
	var down, unheld []string
	for _, s := range d.Seats {
		cell := Cell{Team: s.Team, Seat: s.Seat, Role: "-"}
		held := false
		for _, have := range seats {
			if have.Team == s.Team && have.Seat == s.Seat {
				held = have.Held
				if told := rep.ToldRole(s.Team, have.Handle); told != "" {
					cell.Role = told
				}
			}
		}
		row.Teams = append(row.Teams, cell)
		if !x.teamIsUp(ctx, s.Team, cache) {
			down = append(down, s.Team)
		}
		if !held {
			unheld = append(unheld, s.Seat+"@"+s.Team)
		}
	}
	alive, aliveErr := x.keeperAlive(ctx, container)
	row.Unguarded = aliveErr == nil && !alive
	row.State = stateWord(name, alive, aliveErr, seatsErr, down, unheld)
	return row
}

// stateWord is a session's STATE: what is wrong with it first, else ok.
func stateWord(name string, alive bool, aliveErr, seatsErr error, down, unheld []string) string {
	switch {
	case aliveErr != nil:
		return "its processes cannot be read: " + lastLine(aliveErr)
	case !alive:
		return "unguarded: ccy-teams session repair " + name
	case seatsErr != nil:
		return "its seats cannot be read: " + lastLine(seatsErr)
	case len(down) > 0:
		return strings.Join(down, ",") + " down"
	case len(unheld) > 0:
		return strings.Join(unheld, ",") + " not held"
	}
	return "ok"
}

// checkoutSeats is agent-bus seat list in a checkout, read once per list.
func (x *Hot) checkoutSeats(ctx context.Context, checkout string, cache listCache) ([]agentbus.Seat, error) {
	if seats, ok := cache.seats[checkout]; ok {
		return seats, nil
	}
	seats, _, err := x.Host.Bus.SeatList(ctx, checkout)
	if err != nil {
		return nil, err
	}
	cache.seats[checkout] = seats
	return seats, nil
}

// teamIsUp is whether a team's homeserver is up, asked once per list.
func (x *Hot) teamIsUp(ctx context.Context, team string, cache listCache) bool {
	if up, ok := cache.up[team]; ok {
		return up
	}
	state, err := x.Host.Systemd.HomeserverState(ctx, team)
	cache.up[team] = err == nil && state == systemd.HomeserverUp
	return cache.up[team]
}

// runningSessions is the running sessions in sessprobe.LabelledSessions' lines: name -> container.
func runningSessions(out string) map[string]string {
	running := map[string]string{}
	for line := range strings.Lines(out) {
		fields := strings.Split(strings.TrimSpace(line), "\t")
		if len(fields) == 3 && fields[1] == stateRunning && membership.ValidSession(fields[2]) {
			running[fields[2]] = fields[0]
		}
	}
	return running
}

// SessionsIn is every running session whose desired membership names team (H29, for
// ccy-teams remove), by name.
func (x *Hot) SessionsIn(ctx context.Context, team string) ([]string, error) {
	runtime := x.Host.Getenv("XDG_RUNTIME_DIR")
	if !filepath.IsAbs(runtime) {
		return nil, errors.New("XDG_RUNTIME_DIR is not an absolute path")
	}
	out, err := sessprobe.LabelledSessions(ctx, x.Host.Runner)
	if err != nil {
		return nil, fmt.Errorf("podman could not list the sessions' containers: %s", lastLine(err))
	}
	var in []string
	for name := range runningSessions(out) {
		d, readErr := membership.Read(membership.Dir(runtime, name))
		if readErr == nil && slices.ContainsFunc(d.Seats, func(s membership.Seat) bool { return s.Team == team }) {
			in = append(in, name)
		}
	}
	slices.Sort(in)
	return in, nil
}

// TakeOut is `session remove <session> <team>` for a team being taken down (H29): the
// coordinator role goes to no one, nothing is asked; whether the session is out.
func (x *Hot) TakeOut(ctx context.Context, session, team string) bool {
	return x.Run(ctx, []string{verbRemove, session, team, optCoord + coordDemote, optYes}) == term.ExitOK
}

// keeperAlive is whether a seat keeper (pingbus seat keep) runs in the container (H10).
func (x *Hot) keeperAlive(ctx context.Context, container string) (bool, error) {
	out, err := sessprobe.ContainerProcesses(ctx, x.Host.Runner, container)
	if err != nil {
		return false, err
	}
	for line := range strings.Lines(out) {
		if strings.Contains(line, "pingbus") && strings.Contains(line, " seat keep") {
			return true, nil
		}
	}
	return false, nil
}

// repair is `session repair` (H10): a session whose seat keeper stopped gets one again, with no
// parent in the container, and it takes back every seat the session is in.
func (x *Hot) repair(ctx context.Context, t *target, r request) term.ExitCode {
	alive, err := x.keeperAlive(ctx, t.container)
	if err != nil {
		x.mark(markFailed, "keeper: podman could not list the session's processes: "+lastLine(err))
		return term.ExitFailure
	}
	if alive {
		x.mark(markSkip, "keeper: running")
		x.say("Nothing to do: " + t.session + "'s seat keeper is running.")
		return term.ExitOK
	}
	if code, proceed := x.confirm(ctx, "Start "+t.session+"'s seat keeper again? It takes back every seat the session is in.", r.yes); !proceed {
		return code
	}
	if err = sessprobe.ExecDetached(ctx, x.Host.Runner, t.container, "pingbus", "seat", "keep"); err != nil {
		x.mark(markFailed, "keeper: podman exec: "+lastLine(err))
		return term.ExitFailure
	}
	start := x.Now()
	for {
		alive, err = x.keeperAlive(ctx, t.container)
		unheld := x.unheld(ctx, t)
		if err == nil && alive && len(unheld) == 0 {
			n := len(t.desired.Seats)
			x.mark(markDone, fmt.Sprintf("keeper: running again, %s held (%.1f s)", plural(n, "seat"), x.elapsed(start)))
			x.say("Repaired. " + t.session + "'s seat keeper is running again.")
			return term.ExitOK
		}
		if x.Now().Sub(start) >= x.Waits.Apply {
			if !alive {
				x.mark(markFailed, "keeper: not running after "+seconds(x.Waits.Apply)+": see "+keeperLog+" in the session")
			} else {
				x.mark(markFailed, "keeper: running, but not holding "+strings.Join(unheld, ", ")+" after "+seconds(x.Waits.Apply)+
					" (ccy-teams session list says why)")
			}
			return term.ExitFailure
		}
		x.Sleep(x.Waits.Poll)
	}
}

// unheld is every desired seat of the session whose lock does not read held.
func (x *Hot) unheld(ctx context.Context, t *target) []string {
	var not []string
	for _, s := range t.desired.Seats {
		if held, err := x.isHeld(ctx, t, s.Team, s.Seat); err != nil || !held {
			not = append(not, s.Seat+"@"+s.Team)
		}
	}
	return not
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}
