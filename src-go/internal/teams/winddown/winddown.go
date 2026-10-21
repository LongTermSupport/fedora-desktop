// Package winddown is `ccy-teams wind-down <team>` and the team row's "Wind down the team's
// sessions" (Plan 00203 SPEC-team-wind-down.md W1-W13). Every running desktop session that a
// session record names for a seat of the team, the coordinator's included, gets `/goal clear`
// typed into its tmux pane at an idle moment, then the wind-down instruction: we are about to
// shut down, the work is not done, so save and push what is in progress, stop sub-agents, write
// down where unfinished work stands, and stop. Both are typed, as `shutdown` types /exit, so the
// instruction is the owner's own turn in the session, never a bus message (D1).
//
// An LXC member in a running container is reached the same way, in its own session: the tmux
// server play-agent-bus.yml's service unit starts in the container, through `sudo lxc-attach` as
// the member's user (lxcfreeze.Lister.InContainer). Members this host cannot type into (a
// stopped or held-back container, a session on another machine, a held seat no record names)
// are named as not reached and fail the run, with the text to give them by hand. Nothing is
// ended, killed or changed: it is local only.
package winddown

import (
	"context"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/lxcfreeze"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sudo"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/lxcrecord"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

// Waits are the pauses and the bounds of a wind-down (W6).
type Waits struct {
	// Submit is the pause between typing a line and pressing Enter, as ccy-sessions types.
	Submit time.Duration
	// Poll is how often the panes are read again.
	Poll time.Duration
	// Idle bounds the wait for a busy session to become idle before a line is typed.
	Idle time.Duration
	// Settle bounds the wait for a typed line to leave the input box.
	Settle time.Duration
}

// DefaultWaits are the production waits.
var DefaultWaits = Waits{Submit: 200 * time.Millisecond, Poll: time.Second, Idle: 300 * time.Second, Settle: 30 * time.Second}

// Why a member is not reached or not running, in the words the preview and the report use.
const (
	whyHeld = "held by a session ccy-teams cannot name (a cc session started with --teams <team> alone, " +
		"or one outside ccy's tmux); attach it and type /goal clear, then the text below"
	whyLxcUnknown = "no LXC member record of this host names it (a container on another machine, or one " +
		"joined by hand): type /goal clear in its session where it runs, then the text below"
	whyLxcDeferred = "the last apply deferred it (its container was not running): start the container, " +
		"run ccy-teams apply, then wind-down again"
	whyNoSudo    = "lxc-attach needs sudo, which is not ready"
	whyElsewhere = "not a session on this host's ccy tmux (another machine, or a checkout ccy-teams " +
		"does not know); type /goal clear in its session where it runs, then the text below"
	whyFree    = "no session holds it"
	wordParked = "parked"
	kindLxc    = board.Kind("lxc")
	// lxcTmux is the socket and the session of an LXC member's service unit (members.py
	// SERVICE_TMUX); prefixDirect is restore.Judge's word for claude run with no launcher.
	lxcTmux      = "agent-bus-claude"
	prefixDirect = "cc"
)

// Containers is the host's LXC, as lxcfreeze.Lister reads it: where root keeps its containers,
// which run, and a runner for tmux inside one as its user.
type Containers interface {
	LxcPath(ctx context.Context) (string, error)
	Containers(ctx context.Context) (running, stopped []string, err error)
	InContainer(lxcPath, container, user string) execx.Runner
}

// WindDown is the flow. Every dependency is a field so a test stands in for each.
type WindDown struct {
	Kit     *flow.Kit
	Collect func(context.Context) (*board.Snapshot, error)
	Tmux    *tmuxccy.Tmux
	// Lxc reaches the LXC members' sessions; Sudo is made ready for it; AppliedDir holds the
	// play's LXC member record.
	Lxc        Containers
	Sudo       *sudo.Sudo
	AppliedDir string

	Term   *term.Term
	Out    *term.Printer
	Stderr io.Writer
	// Payload is where --help prints.
	Payload    io.Writer
	Stdin      io.Reader
	IsTerminal func(any) bool
	Home       string

	Now   func() time.Time
	Sleep func(time.Duration)
	Waits Waits
}

// New is the WindDown of a host, reading the machine through the table's collector.
func New(h *host.Host, kit *flow.Kit, b *board.Collector) *WindDown {
	return &WindDown{
		Kit: kit, Collect: b.Collect, Tmux: h.Tmux,
		Lxc: &lxcfreeze.Lister{Sudo: h.Sudo, LookPath: h.LookPath}, Sudo: h.Sudo, AppliedDir: h.AppliedDir,
		Term: h.Term, Out: h.Out, Stderr: h.Stderr, Payload: h.Payload, Stdin: h.Stdin, IsTerminal: h.IsTerminal, Home: h.Home,
		Now: time.Now, Sleep: time.Sleep, Waits: DefaultWaits,
	}
}

const helpText = `Usage: ccy-teams wind-down <team> [--yes]

Tells every running session of the team on this desktop, the coordinator included, that we are
about to shut down: the work is not done, so commit what is in progress, stop sub-agents, push
what passes its QA gate, write down where unfinished work stands, and stop. Into each session's
tmux pane, at an idle moment (a busy one is waited for, up to 300 s, never interrupted), it
types /goal clear and then that instruction, as if you typed them: it is your turn in the
session, not a team message. Nothing is ended; ccy-teams shutdown <team> does that afterwards.
An LXC member in a running container is reached in its session there (the agent-bus-claude
tmux session of its service unit), through sudo lxc-attach as its user. A member whose container
is stopped or was held back, or one on another machine, is named, with the text to give it by
hand. Without --yes it asks first.

Exit codes:
  0   every running session was wound down, and no member was out of reach
  1   a session failed, or a member is not reachable from here (a stopped container, another machine)
  64  usage error
`

// Run is `ccy-teams wind-down <team> [--yes]` (W1).
func (w *WindDown) Run(ctx context.Context, args []string) term.ExitCode {
	team, yes := "", false
	for _, a := range args {
		switch {
		case a == "-h" || a == "--help":
			w.Out.Print(w.Payload, helpText)
			return term.ExitOK
		case a == "--yes":
			yes = true
		case strings.HasPrefix(a, "-"):
			return w.usageError("unknown wind-down option '" + a + "'.")
		case team == "":
			team = a
		default:
			return w.usageError("unexpected argument '" + a + "'.")
		}
	}
	if team == "" {
		return w.usageError("'wind-down' needs a team.")
	}
	return w.forTeam(ctx, team, yes)
}

// Offer is the team row's menu line (W12): the same flow with the picker's yes/no, then back to
// the list once what it said has been read.
func (w *WindDown) Offer(ctx context.Context, team string) (flow.Result, error) {
	if err := w.Kit.InPicker("ccy-teams wind-down "+team, w.forTeam(ctx, team, false)); err != nil {
		return flow.Done, err
	}
	w.Kit.BackToList(ctx)
	return flow.Done, nil
}

// forTeam reads the machine, shows what will be done, asks, does it and reports.
func (w *WindDown) forTeam(ctx context.Context, team string, yes bool) term.ExitCode {
	snap, err := w.Collect(ctx)
	if err != nil {
		return term.ExitFailure
	}
	if !slices.Contains(snap.Teams, team) {
		w.Term.PrintError("there is no team " + team + " on this machine; ccy-teams --list shows the teams.")
		return term.ExitFailure
	}
	p := planFor(snap, team)
	for _, j := range p.jobs {
		j.tmux = w.Tmux
	}
	w.reachLxc(ctx, team, snap, &p)
	if len(p.jobs) == 0 && len(p.unreached) == 0 && len(p.idle) == 0 {
		w.tell("Team " + team + " has no member to wind down.")
		return term.ExitOK
	}
	w.preview(team, p)
	if len(p.jobs) > 0 {
		if code, proceed := w.confirm(ctx, team, len(p.jobs), yes); !proceed {
			return code
		}
		w.windAll(ctx, p.jobs)
	}
	return w.report(team, p)
}

// confirm is the question, default No, or --yes; with no terminal only --yes (W4).
func (w *WindDown) confirm(ctx context.Context, team string, n int, yes bool) (term.ExitCode, bool) {
	if yes {
		return term.ExitOK, true
	}
	if !w.IsTerminal(w.Stdin) || !w.IsTerminal(w.Term.Err) {
		return w.usageError("winding sessions down tells their agents to stop: confirm it with --yes when there is no terminal."), false
	}
	here := flow.Here(team, "Wind down").About("team " + team + " · " + sessions(n) + " running")
	sure, err := w.Kit.Confirm(ctx, here, "Wind down "+sessions(n)+" of "+team+"?", "wind down "+sessions(n), false)
	if err != nil {
		if code, ok := flow.ExitCodeOf(err); ok {
			return code, false
		}
		return term.ExitFailure, false
	}
	if !sure {
		w.tell("Nothing was wound down.")
		return term.ExitOK, false
	}
	return term.ExitOK, true
}

// seatRef is one seat a session names.
type seatRef struct{ seat, team string }

// member is a member that is not a job, and why: not running (nothing to tell) or not reached.
type member struct{ name, why string }

// plan is what a wind-down acts on, what has nothing to tell and what it cannot reach; lxc is
// the LXC member rows still to be resolved (reachLxc).
type plan struct {
	jobs      []*job
	idle      []member
	unreached []member
	lxc       []board.Row
}

// planFor is the team's running sessions, one job each, its members with no running session,
// and its members out of reach (W2, D4).
func planFor(snap *board.Snapshot, team string) plan {
	var p plan
	for _, row := range snap.Rows {
		if row.Team != team {
			continue
		}
		switch {
		case row.Kind == board.KindTeam, row.Kind == board.KindHuman:
		case row.State == wordParked:
			p.idle = append(p.idle, member{nameOf(row), wordParked})
		case row.Kind == board.KindCcy && row.Dir != "-":
			p.addSeat(snap, team, row)
		case row.Kind == kindLxc && row.State == lxcrecord.StatusDeferred:
			p.unreached = append(p.unreached, member{row.Member, whyLxcDeferred})
		case row.Kind == kindLxc:
			p.lxc = append(p.lxc, row)
		default:
			p.unreached = append(p.unreached, member{row.Handle, whyElsewhere})
		}
	}
	if why := snap.MembersUnknown[team]; why != "" {
		p.unreached = append(p.unreached, member{
			"the members of " + team,
			"the team's members could not be read (" + why + "), so members outside this host's checkouts are not listed",
		})
	}
	return p
}

// nameOf is how a member is named: its handle, else its seat.
func nameOf(row board.Row) string {
	if row.Handle != "" && row.Handle != "-" {
		return row.Handle
	}
	return row.Member + "@" + row.Team
}

// addSeat makes a job of the live session holding a seat row, once per session; a held seat no
// live session names is out of reach, and a seat with no live session has nothing to tell.
func (p *plan) addSeat(snap *board.Snapshot, team string, row board.Row) {
	_, live := snap.Live[row.Record]
	switch {
	case row.Record != "" && live:
		if slices.ContainsFunc(p.jobs, func(j *job) bool { return j.name == row.Record }) {
			return
		}
		p.jobs = append(p.jobs, &job{
			name: row.Record, dir: row.Dir, seat: row.Member, handle: row.Handle, team: team,
			coordinator: snap.RoleOf(team, row.Handle) == agentbus.RoleCoordinator,
			others:      othersOf(snap, row.Record, team),
			pane:        row.Record, prefix: launcherOf(row.Record), missing: "the session ended before anything was typed",
		})
	case row.Held:
		p.unreached = append(p.unreached, member{row.Member + "@" + team + " (" + row.Handle + ")", whyHeld})
	case row.Record != "":
		p.idle = append(p.idle, member{row.Member + "@" + team, "its session " + row.Record + " is stopped"})
	default:
		p.idle = append(p.idle, member{row.Member + "@" + team, whyFree})
	}
}

// launcherOf is the launcher a ccy tmux session's name starts with (ccy, cc).
func launcherOf(name string) string {
	prefix, _, _ := strings.Cut(name, "-")
	return prefix
}

// reachLxc makes a job of each LXC member whose container runs on this host, in its session
// there; the rest are not reached (D4). sudo is asked for only when there is one to reach.
func (w *WindDown) reachLxc(ctx context.Context, team string, snap *board.Snapshot, p *plan) {
	rows := p.lxc
	p.lxc = nil
	if len(rows) == 0 {
		return
	}
	notReached := func(why string, rows ...board.Row) {
		for _, row := range rows {
			p.unreached = append(p.unreached, member{row.Handle, why})
		}
	}
	rec, _, err := lxcrecord.Read(w.AppliedDir, team)
	if err != nil {
		notReached(err.Error(), rows...)
		return
	}
	var items []lxcrecord.Item
	var reachable []board.Row
	for _, row := range rows {
		i := slices.IndexFunc(rec.Members, func(it lxcrecord.Item) bool { return it.Handle == row.Handle })
		switch {
		case i < 0:
			notReached(whyLxcUnknown, row)
		case rec.Members[i].Status != lxcrecord.StatusJoined:
			notReached("the last apply held it back ("+rec.Members[i].Words()+")", row)
		default:
			items, reachable = append(items, rec.Members[i]), append(reachable, row)
		}
	}
	if len(reachable) == 0 {
		return
	}
	if err = w.Sudo.Ready(ctx, "Typing into the LXC members' sessions runs lxc-attach as root."); err != nil {
		notReached(whyNoSudo, reachable...)
		return
	}
	lxcPath, err := w.Lxc.LxcPath(ctx)
	if err != nil {
		notReached(err.Error(), reachable...)
		return
	}
	running, _, err := w.Lxc.Containers(ctx)
	if err != nil {
		notReached(err.Error(), reachable...)
		return
	}
	for i, row := range reachable {
		it := items[i]
		if !slices.Contains(running, it.Container) {
			notReached("its container "+it.Container+" is not running; start it, then run wind-down again", row)
			continue
		}
		p.jobs = append(p.jobs, &job{
			name: it.Label(), handle: row.Handle, team: team,
			coordinator: snap.RoleOf(team, row.Handle) == agentbus.RoleCoordinator,
			tmux:        &tmuxccy.Tmux{Runner: w.Lxc.InContainer(lxcPath, it.Container, it.User), Term: w.Term, Socket: lxcTmux},
			pane:        lxcTmux, prefix: prefixDirect,
			where: "LXC member " + row.Handle + ", its session " + lxcTmux + " in container " + it.Container,
			missing: "no " + lxcTmux + " tmux session runs in container " + it.Container + " for " + it.User +
				" (a member started by hand, not with service: true, or its session has stopped); nothing was typed",
		})
	}
}

// othersOf is every seat the session names on teams other than team, by team.
func othersOf(snap *board.Snapshot, name, team string) []seatRef {
	var refs []seatRef
	for _, k := range snap.Seats.SeatsOf(name) {
		if k.Team != team {
			refs = append(refs, seatRef{seat: k.Seat, team: k.Team})
		}
	}
	slices.SortFunc(refs, func(a, b seatRef) int { return strings.Compare(a.team+"@"+a.seat, b.team+"@"+b.seat) })
	return refs
}

// preview says what will be wound down, what has nothing to tell and what cannot be reached (W3).
func (w *WindDown) preview(team string, p plan) {
	if len(p.jobs) > 0 {
		w.tell("Team " + team + ": " + sessions(len(p.jobs)) + " to wind down.")
	}
	for _, j := range p.jobs {
		line := "  " + j.name + "   seat " + j.seat + " (" + j.handle + ") in " + term.AbbrevHome(j.dir, w.Home)
		if j.where != "" {
			line = "  " + j.name + "   " + j.where
		}
		if j.coordinator {
			line += ", the coordinator"
		}
		if len(j.others) > 0 {
			names := make([]string, len(j.others))
			for i, r := range j.others {
				names[i] = r.team + " (seat " + r.seat + ")"
			}
			line += ", also on " + strings.Join(names, ", ")
		}
		w.tell(line)
	}
	w.list("Not running, nothing to wind down:", p.idle)
	w.list("Not reachable from here:", p.unreached)
	if len(p.jobs) > 0 {
		w.tell("Each session is sent /goal clear, then the wind-down instruction, typed into its pane at an idle moment " +
			"(a busy one is waited for, up to " + seconds(w.Waits.Idle) + ", never interrupted). " +
			"Nothing is ended: ccy-teams shutdown " + team + " does that afterwards.")
	}
}

func (w *WindDown) list(heading string, members []member) {
	if len(members) == 0 {
		return
	}
	w.tell(heading)
	for _, m := range members {
		w.tell("  " + m.name + "   " + m.why)
	}
}

// report is one line per member, then the count, and the exit code: success only when every
// running session was wound down and nothing was out of reach (W9, W10).
func (w *WindDown) report(team string, p plan) term.ExitCode {
	var doneN, failedN int
	for _, j := range p.jobs {
		if j.result == resultDone {
			doneN++
			w.tell("✓ " + j.name + ": /goal clear and the wind-down typed")
			continue
		}
		failedN++
		w.tell("✗ " + j.name + ": " + j.note)
	}
	for _, m := range p.idle {
		w.tell("- " + m.name + ": not running: " + m.why)
	}
	for _, m := range p.unreached {
		w.tell("✗ " + m.name + ": not reached: " + m.why)
	}
	count := team + ": " + strconv.Itoa(doneN) + " wound down, " + strconv.Itoa(failedN) + " failed, " +
		strconv.Itoa(len(p.idle)) + " not running"
	if n := len(p.unreached); n > 0 {
		count += "; " + strconv.Itoa(n) + " member" + plural(n) + " not reachable from here"
	}
	w.tell(count + ".")
	if len(p.unreached) > 0 {
		w.tell("To wind them down by hand, type /goal clear in each one's session, then:")
		w.tell("  " + Instruction(team, false))
	}
	if failedN > 0 || len(p.unreached) > 0 {
		return term.ExitFailure
	}
	return term.ExitOK
}

// Instruction is the wind-down text a session is given (W8): one line, since a newline typed
// into the box would submit it early.
func Instruction(team string, coordinator bool) string {
	text := "[ccy-teams wind-down] This is the owner's wind-down of team " + team + ", typed from ccy-teams on the host. " +
		"We are about to shut down. This does not mean the work is done. Wind down now: " +
		"commit your work in progress (WIP commits are fine); " +
		"stop every sub-agent and background task you started, and wait for each to end; " +
		"push each branch whose QA gate passes, and say which you did not push and why; " +
		"write down where each unfinished piece of work stands and what comes next, in the plan or handoff it belongs to, " +
		"so it can be resumed; start and dispatch nothing new. Any /goal was cleared for this: set no new one. "
	if coordinator {
		text += "You are the team's coordinator: your workers are being told the same, so dispatch nothing " +
			"and record what the team still owes. "
	}
	return text + "Then stop; if a hook asks you to carry on, stop anyway with " +
		"STOPPING BECAUSE: the owner ordered a team wind-down."
}

func sessions(n int) string { return strconv.Itoa(n) + " session" + plural(n) }

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func seconds(d time.Duration) string { return strconv.FormatFloat(d.Seconds(), 'f', -1, 64) + " s" }

func (w *WindDown) tell(msg string) { w.Out.Print(w.Stderr, msg+"\n") }

func (w *WindDown) usageError(msg string) term.ExitCode {
	w.Out.Print(w.Stderr, "ccy-teams: "+msg+" See ccy-teams --help.\n")
	return term.ExitUsage
}
