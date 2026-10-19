// Package restart is `ccy-teams restart <team>` and the team row's "Restart the team's desktop
// sessions" (Plan 00203 SPEC-team-add-and-restart B1-B11, Decisions 8 to 10). Each session a
// session record names for a seat of the team is one job, once per session:
//
//   - a running ccy session restarts in place at its next idle moment, through its own
//     supervisor: relaunch.json with why "restart" in its membership directory, which the
//     lifecycle plugin answers, resuming its exact conversation in a fresh container (D8);
//   - a running cc session is left running and named: cc has no supervisor (D9);
//   - a stopped one is started as `ccy-sessions restore` starts a record, resuming its recorded
//     conversation by id, never `--continue`; with none recorded it is not started (D10).
//
// LXC members, members run elsewhere and seats no record names are left out and named. Each
// session is failed alone and the others go on; any failure is exit 1. Nothing here applies a
// play, so there is no play lock to wait for (B10). It is local only: tmux, podman, the session
// registry, the membership directory and `agent-bus seat list`, as the user (B11).
package restart

import (
	"context"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

// Waits are the bounds of a restart (B7).
type Waits struct {
	// Poll is how often the containers and the seats are read again.
	Poll time.Duration
	// InPlace bounds a running session's restart: a busy one waits for its idle moment.
	InPlace time.Duration
	// Start bounds a stopped session's start, until its seats are held.
	Start time.Duration
}

// DefaultWaits are the production waits.
var DefaultWaits = Waits{Poll: time.Second, InPlace: 300 * time.Second, Start: 180 * time.Second}

// Why a member is left out, in the words the preview and the result use (B3).
const (
	whyLxc       = "an LXC container's agent: restart it inside its container"
	whyElsewhere = "not a session on this desktop's ccy tmux (another machine, or a checkout ccy-teams does not know); restart it where it runs"
	wordParked   = "parked"
	kindLxc      = board.Kind("lxc")
)

// Restart is the flow. Every dependency is a field so a test stands in for each.
type Restart struct {
	Kit      *flow.Kit
	Collect  func(context.Context) (*board.Snapshot, error)
	Tmux     *tmuxccy.Tmux
	Bus      *agentbus.Client
	Runner   execx.Runner
	Registry func() (*registry.Store, error)
	Getenv   func(string) string

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

// New is the Restart of a host, reading the machine through the table's collector.
func New(h *host.Host, kit *flow.Kit, b *board.Collector) *Restart {
	return &Restart{
		Kit: kit, Collect: b.Collect, Tmux: h.Tmux, Bus: h.Bus, Runner: h.Runner, Registry: h.Registry, Getenv: h.Getenv,
		Term: h.Term, Out: h.Out, Stderr: h.Stderr, Payload: h.Payload, Stdin: h.Stdin, IsTerminal: h.IsTerminal, Home: h.Home,
		Now: time.Now, Sleep: time.Sleep, Waits: DefaultWaits,
	}
}

const helpText = `Usage: ccy-teams restart <team> [--yes]

Restarts the team's sessions on this desktop, each resuming its own conversation by id
(claude --resume <id>, never --continue):
  a running ccy session restarts in place at its next idle moment, through its supervisor,
    in a fresh container (up to 300 s);
  a running cc session is left running: cc has no supervisor; /exit it and run this again;
  a stopped session is started again as ccy-sessions restore starts it (up to 180 s); one
    whose conversation was never recorded is named and not started.
LXC members are left out: restart them inside their container. Without --yes it asks first.

Exit codes:
  0   every session restarted, started or left as it is on purpose
  1   a session failed
  64  usage error
`

// Run is `ccy-teams restart <team> [--yes]` (B1).
func (r *Restart) Run(ctx context.Context, args []string) term.ExitCode {
	team, yes := "", false
	for _, a := range args {
		switch {
		case a == "-h" || a == "--help":
			r.Out.Print(r.Payload, helpText)
			return term.ExitOK
		case a == "--yes":
			yes = true
		case strings.HasPrefix(a, "-"):
			return r.usageError("unknown restart option '" + a + "'.")
		case team == "":
			team = a
		default:
			return r.usageError("unexpected argument '" + a + "'.")
		}
	}
	if team == "" {
		return r.usageError("'restart' needs a team.")
	}
	return r.forTeam(ctx, team, yes)
}

// Offer is the team row's menu line: the same flow with the picker's yes/no, then back to the
// list once what it said has been read.
func (r *Restart) Offer(ctx context.Context, team string) (flow.Result, error) {
	if err := r.Kit.InPicker("ccy-teams restart "+team, r.forTeam(ctx, team, false)); err != nil {
		return flow.Done, err
	}
	r.Kit.BackToList(ctx)
	return flow.Done, nil
}

// forTeam reads the machine, shows what will be done, asks, does it and reports.
func (r *Restart) forTeam(ctx context.Context, team string, yes bool) term.ExitCode {
	snap, err := r.Collect(ctx)
	if err != nil {
		return term.ExitFailure
	}
	if !slices.Contains(snap.Teams, team) {
		r.Term.PrintError("there is no team " + team + " on this machine; ccy-teams --list shows the teams.")
		return term.ExitFailure
	}
	p := r.planFor(snap, team)
	if len(p.jobs) == 0 && len(p.leftOut) == 0 {
		r.tell("Team " + team + " has no member on this desktop to restart.")
		return term.ExitOK
	}
	r.preview(team, p)
	acting := p.acting()
	if acting == 0 {
		return r.report(team, p)
	}
	if code, proceed := r.confirm(ctx, team, acting, yes); !proceed {
		return code
	}
	r.actAll(ctx, p.jobs)
	r.waitAll(ctx, p.jobs)
	return r.report(team, p)
}

// confirm is the question, default no, or --yes; with no terminal only --yes (B8).
func (r *Restart) confirm(ctx context.Context, team string, n int, yes bool) (term.ExitCode, bool) {
	if yes {
		return term.ExitOK, true
	}
	if !r.IsTerminal(r.Stdin) || !r.IsTerminal(r.Term.Err) {
		return r.usageError("restarting sessions ends and starts them: confirm it with --yes when there is no terminal."), false
	}
	here := flow.Here(team, "Restart").About("team " + team + " · " + sessions(n) + " to restart")
	sure, err := r.Kit.Confirm(ctx, here, "Restart "+sessions(n)+" of "+team+"?", "restart "+sessions(n), false)
	if err != nil {
		if code, ok := flow.ExitCodeOf(err); ok {
			return code, false
		}
		return term.ExitFailure, false
	}
	if !sure {
		r.tell("Nothing was restarted.")
		return term.ExitOK, false
	}
	return term.ExitOK, true
}

// preview says what will be done to each session and who is left out (B8).
func (r *Restart) preview(team string, p plan) {
	if n := p.acting(); n > 0 {
		r.tell("Team " + team + ": " + sessions(n) + " to restart.")
	}
	for _, j := range p.jobs {
		r.tell("  " + j.name + "   seat " + j.seat + " (" + j.handle + ") in " + term.AbbrevHome(j.dir, r.Home) + ": " + j.plan())
	}
	if len(p.leftOut) > 0 {
		r.tell("Left out:")
		for _, l := range p.leftOut {
			r.tell("  " + l.member + "   " + l.why)
		}
	}
}

// report is one line per session and per member left out, then the count; exit 1 when any
// session failed (B9). A member left out is not a failure: the owner asked for it.
func (r *Restart) report(team string, p plan) term.ExitCode {
	var done, failedN, left int
	for _, j := range p.jobs {
		switch j.result {
		case resultDone:
			done++
			r.tell("✓ " + j.name + ": " + j.note)
		case resultLeft:
			left++
			r.tell("- " + j.name + ": " + j.note)
		case resultPending, resultFailed:
			failedN++
			r.tell("✗ " + j.name + ": " + j.note)
		}
	}
	for _, l := range p.leftOut {
		r.tell("- " + l.member + ": left out: " + l.why)
	}
	count := team + ": " + strconv.Itoa(done) + " restarted, " + strconv.Itoa(failedN) + " failed, " + strconv.Itoa(left)
	if left == 1 {
		count += " left as it is"
	} else {
		count += " left as they are"
	}
	if n := len(p.leftOut); n > 0 {
		count += "; " + strconv.Itoa(n) + " member" + plural(n) + " left out"
	}
	r.tell(count + ".")
	if failedN > 0 {
		return term.ExitFailure
	}
	return term.ExitOK
}

func sessions(n int) string { return strconv.Itoa(n) + " session" + plural(n) }

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func seconds(d time.Duration) string { return strconv.FormatFloat(d.Seconds(), 'f', -1, 64) + " s" }

func (r *Restart) tell(msg string) { r.Out.Print(r.Stderr, msg+"\n") }

func (r *Restart) usageError(msg string) term.ExitCode {
	r.Out.Print(r.Stderr, "ccy-teams: "+msg+" See ccy-teams --help.\n")
	return term.ExitUsage
}
