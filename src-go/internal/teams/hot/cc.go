package hot

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sessprobe"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/discover"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The cc half of hot control (Plan 00203 SPEC-hot-control.md H31, H32, M6). A cc session started
// with --teams or --bus runs claude on the host, in a tmux session cc started on ccy's server. It
// has no container: it is found by its desired membership under $XDG_RUNTIME_DIR/pingbus/membership/
// and its tmux pane; it has ended when that pane is gone or another runs in its place; its seat
// keeper is a process in the pane's tree; its route to a homeserver is the host's own; its seats
// are host seats. It runs as the owner, so it could write its own membership: cc's weaker
// authority (Decision 3), which the usage text says.

const (
	ccMemberType = "host"
	// ccUnguarded is a cc session's STATE when its seat keeper has stopped: repair cannot help.
	ccUnguarded = "unguarded: end it and start it again from ccy-sessions"
)

// isCC is whether the session has a cc session's membership directory.
func isCC(runtime, session string) bool {
	info, err := os.Lstat(membership.CCDir(runtime, session))
	return err == nil && info.IsDir()
}

// locateCC finds a cc session's tmux pane on ccy's server (H35: only a session ccy-sessions knows).
func (x *Hot) locateCC(ctx context.Context, t *target) (term.ExitCode, bool) {
	pid, err := x.panePID(ctx, t.session)
	if err != nil {
		x.mark(markFailed, "session "+t.session+": "+err.Error())
		return term.ExitFailure, false
	}
	if pid == 0 {
		x.mark(markFailed, "session "+t.session+": a cc session, but its tmux session is not running on ccy's server, "+
			"so it cannot be changed while it runs")
		return exitConfig, false
	}
	t.kind, t.pane = membership.KindCC, pid
	return term.ExitOK, true
}

// panePID is the pid of the session's first pane on ccy's tmux server, or 0 when it has none.
func (x *Hot) panePID(ctx context.Context, session string) (int, error) {
	panes, err := x.Host.Tmux.Panes(ctx)
	if err != nil {
		return 0, err
	}
	for _, p := range panes {
		if p.Session == session {
			return p.PID, nil
		}
	}
	return 0, nil
}

// ccEnded is whether a cc session's pane is gone, or another runs in its place.
func (x *Hot) ccEnded(ctx context.Context, t *target) bool {
	pid, err := x.panePID(ctx, t.session)
	return err == nil && pid != t.pane
}

// sessionKeeper is whether a listed session's seat keeper runs: in its container, or (it has
// none) in its cc pane.
func (x *Hot) sessionKeeper(ctx context.Context, container string, pane int) (bool, error) {
	if container == "" {
		return x.ccKeeperAlive(ctx, pane)
	}
	return x.keeperAlive(ctx, container)
}

// rowState is a listed session's STATE: a cc session with no keeper cannot be repaired.
func rowState(name, kind string, alive bool, aliveErr, seatsErr error, down, unheld []string) string {
	if kind == membership.KindCC && aliveErr == nil && !alive {
		return ccUnguarded
	}
	return stateWord(name, alive, aliveErr, seatsErr, down, unheld)
}

// ccKeeperAlive is whether a seat keeper runs in the process tree under the cc session's pane.
func (x *Hot) ccKeeperAlive(ctx context.Context, pane int) (bool, error) {
	table, err := sessprobe.ProcessTable(ctx, x.Host.Runner)
	if err != nil {
		return false, fmt.Errorf("ps could not list the processes: %w", err)
	}
	procs, err := sessprobe.ParseProcs(table)
	if err != nil {
		return false, err
	}
	args := make(map[int]string, len(procs))
	for _, p := range procs {
		args[p.PID] = p.Args
	}
	for _, owned := range sessprobe.SessionPIDs([]sessprobe.Root{{Session: "cc", PID: pane}}, procs) {
		for _, pid := range owned.PIDs {
			if keeperLine(args[pid]) {
				return true, nil
			}
		}
	}
	return false, nil
}

// keeperLine is whether a command line is a seat keeper's (pingbus seat keep).
func keeperLine(line string) bool {
	return strings.Contains(line, "pingbus") && strings.Contains(line, " seat keep")
}

// ccReach is H22 step 5 for a cc session: the session's route is the host's, so the host's
// pingbus probes the team's homeserver from the checkout's seats.
func (x *Hot) ccReach(ctx context.Context, t *target, team string) (term.ExitCode, bool) {
	if !x.pingbus(ctx, "route", "this host reaches "+team+"'s homeserver", "reach", "--team", team, "--root", t.seatsRoot()) {
		return term.ExitFailure, false
	}
	return term.ExitOK, true
}

// ccNoInbound is the end of an add to a cc session launched on no team (Decision 7): Claude Code
// read its settings once, refusing team messages, and cc has no supervisor to relaunch it, so the
// seat and the record stay and the owner is told to start cc again, which starts it in the seat.
func (x *Hot) ccNoInbound(t *target, team, seat string) term.ExitCode {
	x.mark(markFailed, "session: "+t.session+" is in "+team+" as seat "+seat+", but it started on no team (cc --bus), "+
		"so team messages cannot reach it until cc is started again, and cc has nothing that can restart it: "+
		"end it (/exit) and start it again from ccy-sessions, which starts it in seat "+seat)
	return term.ExitFailure
}

// ccRefusing is whether a cc session's keeper reports that it was launched refusing team
// messages (cc --bus, Decision 7).
func ccRefusing(t *target, rep membership.Report) bool {
	return t.cc() && rep.Inbound != nil && !*rep.Inbound
}

// ccRepair is repair for a cc session: its keeper ends with its claude (it watches its parent),
// and one the host started would outlive the session holding its seats, so it is refused.
func (x *Hot) ccRepair(t *target) term.ExitCode {
	x.mark(markFailed, "keeper: "+t.session+" is a cc session: its seat keeper ends with its claude, so it cannot be "+
		"started again from outside; end the session (/exit) and start it again from ccy-sessions, which takes its seats")
	return exitConfig
}

// runningCC is every running cc session with a membership, name -> pane, and a line for each cc
// membership that could not be read.
func (x *Hot) runningCC(ctx context.Context, runtime string) (map[string]int, []string, error) {
	panes, err := x.Host.Tmux.Panes(ctx)
	if err != nil {
		return nil, nil, err
	}
	pane := map[string]int{}
	for _, p := range panes {
		if _, seen := pane[p.Session]; !seen {
			pane[p.Session] = p.PID
		}
	}
	desired, unreadable, err := discover.RunningCC(runtime, func(name string) bool { return pane[name] != 0 })
	if err != nil {
		return nil, nil, err
	}
	running := make(map[string]int, len(desired))
	for _, d := range desired {
		running[d.Session] = pane[d.Session]
	}
	return running, unreadable, nil
}
