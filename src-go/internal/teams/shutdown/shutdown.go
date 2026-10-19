// Package shutdown is `ccy-teams shutdown <team>` and the team row's "Shut down every session of
// <team>" (Plan 00201 SPEC-shutdown.md). Every live ccy or cc session that a session record
// names for a seat of the team is sent /exit through its tmux pane, as if typed, and stopped
// only when it has not ended in time; each seat it held is then read again until it is free.
// Members this host cannot reach (an LXC container's agent, a session on another machine, a
// held seat no record names) are listed as not shut down, never acted on.
//
// It is local only: it acts through tmux, the session registry and `agent-bus seat list` as
// the user. Nothing goes on the bus, and nothing from the bus can start it.
package shutdown

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

// Waits are the pauses and the bounds of a shutdown (SPEC-shutdown.md SD10, SD12, SD14).
type Waits struct {
	// Submit is the pause between typing /exit and pressing Enter, as ccy-sessions types.
	Submit time.Duration
	// Poll is how often the panes and the seats are read again.
	Poll time.Duration
	// Prompt bounds the wait for claude's prompt before /exit can be typed.
	Prompt time.Duration
	// Exit bounds the wait for a session to end after /exit.
	Exit time.Duration
	// Release bounds the wait for a seat to be free once its session has ended.
	Release time.Duration
}

// DefaultWaits are the production waits.
var DefaultWaits = Waits{
	Submit: 200 * time.Millisecond, Poll: time.Second,
	Prompt: 30 * time.Second, Exit: 60 * time.Second, Release: 30 * time.Second,
}

// Why a member is out of reach, in the words the preview and the result use.
const (
	whyHeld = "held by a session ccy-teams cannot name (a cc session started with --teams <team> alone, " +
		"or one outside ccy's tmux); attach it (ccy-sessions) and type /exit"
	whyLxc = "its session runs inside the LXC container, out of reach of this host's tmux; " +
		"in the container, type /exit in its session"
	whyElsewhere = "not a session on this host's ccy tmux (another machine, or a checkout ccy-teams " +
		"does not know); end it where it runs"
	wordParked = "parked"
	kindLxc    = board.Kind("lxc")
)

// Shutdown is the flow. Every dependency is a field so a test stands in for each.
type Shutdown struct {
	Kit      *flow.Kit
	Collect  func(context.Context) (*board.Snapshot, error)
	Tmux     *tmuxccy.Tmux
	Bus      *agentbus.Client
	Registry func() (*registry.Store, error)

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

// New is the Shutdown of a host, reading the machine through the table's collector.
func New(h *host.Host, kit *flow.Kit, b *board.Collector) *Shutdown {
	return &Shutdown{
		Kit: kit, Collect: b.Collect, Tmux: h.Tmux, Bus: h.Bus, Registry: h.Registry,
		Term: h.Term, Out: h.Out, Stderr: h.Stderr, Payload: h.Payload, Stdin: h.Stdin, IsTerminal: h.IsTerminal, Home: h.Home,
		Now: time.Now, Sleep: time.Sleep, Waits: DefaultWaits,
	}
}

const helpText = `Usage: ccy-teams shutdown <team> [--yes]

Sends /exit to every ccy session of the team, as if typed, and waits for each to end;
a session that has not ended within 60 s is stopped (its tmux session killed), and its
seat is released. The homeserver keeps running. Without --yes it asks first.

Exit codes:
  0   every session exited cleanly or stopped, or nothing was shut down on request
  1   a session failed, or a member is not reachable from here (an LXC member)
  64  usage error
`

// Run is `ccy-teams shutdown <team> [--yes]` (SD1).
func (s *Shutdown) Run(ctx context.Context, args []string) term.ExitCode {
	team, yes := "", false
	for _, a := range args {
		switch {
		case a == "-h" || a == "--help":
			s.Out.Print(s.Payload, helpText)
			return term.ExitOK
		case a == "--yes":
			yes = true
		case strings.HasPrefix(a, "-"):
			return s.usageError("unknown shutdown option '" + a + "'.")
		case team == "":
			team = a
		default:
			return s.usageError("unexpected argument '" + a + "'.")
		}
	}
	if team == "" {
		return s.usageError("'shutdown' needs a team.")
	}
	return s.forTeam(ctx, team, yes)
}

// Offer is the team row's menu line (SD2): the same flow with the picker's yes/no, then back to
// the list once what it said has been read.
func (s *Shutdown) Offer(ctx context.Context, team string) (flow.Result, error) {
	if err := s.Kit.InPicker("ccy-teams shutdown "+team, s.forTeam(ctx, team, false)); err != nil {
		return flow.Done, err
	}
	s.Kit.BackToList(ctx)
	return flow.Done, nil
}

// forTeam reads the machine, shows what will be shut down, asks, and shuts it down.
func (s *Shutdown) forTeam(ctx context.Context, team string, yes bool) term.ExitCode {
	snap, err := s.Collect(ctx)
	if err != nil {
		return term.ExitFailure
	}
	if !slices.Contains(snap.Teams, team) {
		s.Term.PrintError("there is no team " + team + " on this machine; ccy-teams --list shows the teams.")
		return term.ExitFailure
	}
	p := planFor(snap, team)
	if len(p.jobs) == 0 && len(p.unreached) == 0 {
		s.tell("Team " + team + " has no running session.")
		return term.ExitOK
	}
	s.preview(team, p)
	if len(p.jobs) == 0 {
		return s.report(team, p)
	}
	if code, proceed := s.confirm(ctx, team, len(p.jobs), yes); !proceed {
		return code
	}
	s.stopAll(ctx, p.jobs)
	s.checkSeats(ctx, p.jobs)
	return s.report(team, p)
}

// confirm is the question, default No (SD8), or --yes; with no terminal only --yes (SD9).
func (s *Shutdown) confirm(ctx context.Context, team string, n int, yes bool) (term.ExitCode, bool) {
	if yes {
		return term.ExitOK, true
	}
	if !s.IsTerminal(s.Stdin) || !s.IsTerminal(s.Term.Err) {
		return s.usageError("shutting sessions down ends them: confirm it with --yes when there is no terminal."), false
	}
	sure, err := s.Kit.Confirm(ctx, flow.Here(team, "Shut down").About("team "+team+" · "+sessions(n)+" running"), "Shut down "+sessions(n)+" of "+team+"?", "shut down "+sessions(n), false)
	if err != nil {
		if code, ok := flow.ExitCodeOf(err); ok {
			return code, false
		}
		return term.ExitFailure, false
	}
	if !sure {
		s.tell("Nothing was shut down.")
		return term.ExitOK, false
	}
	return term.ExitOK, true
}

// seatRef is one seat a session holds.
type seatRef struct{ dir, seat, team string }

func (r seatRef) String() string { return r.seat + "@" + r.team }

// unreached is a member that cannot be shut down from here, and why.
type unreached struct{ member, why string }

// plan is what a shutdown acts on and what it cannot reach.
type plan struct {
	jobs      []*job
	unreached []unreached
}

// planFor is the team's live sessions, one job each, and its members out of reach (SD4, SD5).
func planFor(snap *board.Snapshot, team string) plan {
	var p plan
	for _, row := range snap.Rows {
		if row.Team != team {
			continue
		}
		switch {
		case row.Kind == board.KindTeam, row.Kind == board.KindHuman, row.State == wordParked:
		case row.Kind == board.KindCcy && row.Dir != "-":
			p.addSeat(snap, team, row)
		case row.Kind == kindLxc:
			p.unreached = append(p.unreached, unreached{row.Handle, whyLxc})
		default:
			p.unreached = append(p.unreached, unreached{row.Handle, whyElsewhere})
		}
	}
	if why := snap.MembersUnknown[team]; why != "" {
		p.unreached = append(p.unreached, unreached{
			"the members of " + team,
			"the team's members could not be read (" + why + "), so members outside this host's checkouts are not listed",
		})
	}
	return p
}

// addSeat makes a job of the live session holding a seat row, once per session; a held seat no
// live session names is out of reach.
func (p *plan) addSeat(snap *board.Snapshot, team string, row board.Row) {
	attached, live := snap.Live[row.Record]
	switch {
	case row.Record != "" && live:
		if slices.ContainsFunc(p.jobs, func(j *job) bool { return j.name == row.Record }) {
			return
		}
		p.jobs = append(p.jobs, &job{
			name: row.Record, dir: row.Dir, seat: row.Member, handle: row.Handle, attached: attached,
			seats: seatsOf(snap, row.Record, team),
		})
	case row.Held:
		p.unreached = append(p.unreached, unreached{row.Member + "@" + team + " (" + row.Handle + ")", whyHeld})
	}
}

// seatsOf is every seat the session names (discover): the team's first, then the others by team.
func seatsOf(snap *board.Snapshot, name, team string) []seatRef {
	keys := snap.Seats.SeatsOf(name)
	refs := make([]seatRef, 0, len(keys))
	for _, k := range keys {
		refs = append(refs, seatRef{dir: k.Dir, seat: k.Seat, team: k.Team})
	}
	slices.SortFunc(refs, func(a, b seatRef) int {
		if (a.team == team) != (b.team == team) {
			if a.team == team {
				return -1
			}
			return 1
		}
		return strings.Compare(a.team+"@"+a.seat, b.team+"@"+b.seat)
	})
	return refs
}

// preview says what will be shut down and what cannot be (SD6).
func (s *Shutdown) preview(team string, p plan) {
	if len(p.jobs) > 0 {
		s.tell("Team " + team + ": " + sessions(len(p.jobs)) + " to shut down.")
	}
	for _, j := range p.jobs {
		line := "  " + j.name + "   seat " + j.seat + " (" + j.handle + ") in " + term.AbbrevHome(j.dir, s.Home)
		if j.attached > 0 {
			line += ", open on a terminal"
		}
		var others []string
		for _, r := range j.seats {
			if r.team != team {
				others = append(others, r.team+" (seat "+r.seat+")")
			}
		}
		if len(others) > 0 {
			line += ", also on " + strings.Join(others, ", ")
		}
		s.tell(line)
	}
	if len(p.unreached) > 0 {
		s.tell("Not reachable from here, so not shut down:")
		for _, u := range p.unreached {
			s.tell("  " + u.member + "   " + u.why)
		}
	}
	if len(p.jobs) > 0 {
		s.tell("Each session is sent /exit, as if typed; one that has not ended within " + seconds(s.Waits.Exit) +
			" is stopped (its tmux session killed). The homeserver keeps running.")
	}
}

// report is one line per session and per member out of reach, then the count, and the exit
// code: success only when every session ended, every seat is free and nothing was out of reach
// (SD15, SD16).
func (s *Shutdown) report(team string, p plan) term.ExitCode {
	var exitedN, stoppedN, failedN int
	for _, j := range p.jobs {
		line := j.name + ": "
		switch j.result {
		case exited:
			exitedN++
			line += "exited cleanly"
			if j.note != "" {
				line += "; " + j.note
			}
		case stopped:
			stoppedN++
			line += "stopped: " + j.note
		case pending, failed:
			failedN++
			line += "failed: " + j.note
		}
		if j.released && len(j.seats) > 0 {
			line += "; " + seatWords(j.seats) + " released"
		}
		s.tell(line)
	}
	for _, u := range p.unreached {
		s.tell(u.member + ": not shut down: " + u.why)
	}
	count := fmt.Sprintf("%s: %d exited cleanly, %d stopped, %d failed", team, exitedN, stoppedN, failedN)
	if n := len(p.unreached); n > 0 {
		count += "; " + strconv.Itoa(n) + " member" + plural(n) + " not reachable from here"
	}
	s.tell(count + ".")
	if failedN > 0 || len(p.unreached) > 0 {
		return term.ExitFailure
	}
	return term.ExitOK
}

func seatWords(refs []seatRef) string {
	names := make([]string, len(refs))
	for i, r := range refs {
		names[i] = r.String()
	}
	return "seat" + plural(len(refs)) + " " + strings.Join(names, ", ")
}

func sessions(n int) string { return strconv.Itoa(n) + " session" + plural(n) }

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func seconds(d time.Duration) string { return strconv.FormatFloat(d.Seconds(), 'f', -1, 64) + " s" }

func (s *Shutdown) tell(msg string) { s.Out.Print(s.Stderr, msg+"\n") }

func (s *Shutdown) usageError(msg string) term.ExitCode {
	s.Out.Print(s.Stderr, "ccy-teams: "+msg+" See ccy-teams --help.\n")
	return term.ExitUsage
}
