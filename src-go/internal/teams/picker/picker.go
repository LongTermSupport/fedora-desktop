// Package picker is ccy-teams with no argument: the list of teams, seats and people, a key for
// each thing that can be done to one, and the offers made on the way in (create a team, lift a
// path limit, update what is out of date, join the team that covers this checkout). It decides
// what a key means and hands the work to the flows: seats, Element and passwords, create, apply
// and remove.
package picker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/hostcheckout"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/create"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/login"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/lxcadd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/ops"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seats"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/settings"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/shutdown"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// readWhy is what sudo is asked for when the members of a team can only be read as root.
const readWhy = "Reading who is in each team, and whether its people are signed in, is done as root: " +
	"the team's admin token is root's. Once play-agent-bus.yml has run, a sudoers rule lets ccy-teams " +
	"read this without a password; until then, sudo asks."

// maxNoOps is how many refused attempts in a row end the picker: a terminal that answers
// every key with nothing would otherwise loop for ever.
const maxNoOps = 3

// Picker is the interactive list. Every dependency is a field so a test stands in for each.
type Picker struct {
	Kit    *flow.Kit
	Board  *board.Collector
	Ops    *ops.Ops
	Create *create.Wizard
	Login  *login.Login
	Seats  *seats.Seats
	Lxc    *lxcadd.Adder
	// Shutdown is the team row's "Shut down every session"; teams.Wire sets it.
	Shutdown *shutdown.Shutdown
	// Settings is the team row's "Edit settings"; teams.Wire sets it.
	Settings *settings.Settings
	// Session is the Running sessions view's hot control (Tab); teams.Wire sets it.
	Session SessionControl

	Declare *declare.Client
	Git     *gitx.Git
	// Checkouts says whether the folder the picker runs in is a checkout a session may start in.
	Checkouts *hostcheckout.Finder

	Stdin  io.Reader
	Stdout io.Writer
	// Here is the folder the command was run in; Columns is how wide its terminal is (0 unknown).
	Here     func() string
	Columns  func() int
	Home     string
	StateDir string
}

// New is the Picker over a host and the flows it hands work to.
func New(h *host.Host, kit *flow.Kit, b *board.Collector, o *ops.Ops, c *create.Wizard, l *login.Login, s *seats.Seats, x *lxcadd.Adder) *Picker {
	return &Picker{
		Kit: kit, Board: b, Ops: o, Create: c, Login: l, Seats: s, Lxc: x, Declare: h.Declare, Git: h.Git,
		Checkouts: h.Checkouts,
		Stdin:     h.Stdin, Stdout: h.Stdout, Here: h.Here, Home: h.Home, StateDir: h.StateDir,
		Columns: func() int { return h.Columns(h.Stdout) },
	}
}

// run is one run of the picker: the header lines built once, the pass being shown, its rows
// laid out, and which list shows it.
type run struct {
	p     *Picker
	frame frame
	snap  *board.Snapshot
	table board.Table
	noOps int
	view  view
}

// Run is ccy-teams with no argument: the list of teams, seats and people, and a key for
// each thing that can be done to one.
func (p *Picker) Run(ctx context.Context) term.ExitCode {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	return p.run(ctx)
}

func (p *Picker) run(ctx context.Context) term.ExitCode {
	if !p.Kit.Term.IsTerminal(p.Stdin) || !p.Kit.Term.IsTerminal(p.Stdout) {
		p.Kit.Out.Print(p.Kit.Stderr, "ccy-teams needs a terminal: it attaches sessions to the one you are in. "+
			"ccy-teams --list prints the table instead.\n")
		return term.ExitFailure
	}
	// The session only ends by leaving; an error that is not a leave has already said why.
	if code, ok := flow.ExitCodeOf(p.pickerSession(ctx)); ok {
		return code
	}
	return term.ExitFailure
}

// pickerSession is the start-up offers, then the loop. It always ends in a leaveError.
func (p *Picker) pickerSession(ctx context.Context) error {
	if err := p.Kit.Interrupted(ctx); err != nil {
		return err
	}
	first, err := p.Board.Refresh(ctx)
	if err != nil {
		return flow.Leave(term.ExitFailure)
	}
	if len(first.Teams) == 0 {
		return p.offerCreate(ctx)
	}
	if err = p.offerPathLimits(ctx); err != nil {
		return err
	}
	if err = p.offerDescriptions(ctx); err != nil {
		return err
	}
	if err = p.offerUpdate(ctx); err != nil {
		return err
	}
	above, err := p.pickerFrame(ctx)
	if err != nil {
		p.Kit.Term.PrintError(err.Error())
		return flow.Leave(term.ExitFailure)
	}
	return (&run{p: p, frame: above}).loop(ctx)
}

// frame is what stays above the rows whichever row the cursor is on: where Ctrl-N would start a
// session and what RESTORE means. It is built once; the column headings are each pass's.
type frame struct {
	newLine, restoreLine string
}

// header is the picker's header with footer, the line naming the row's actions, and rest, the
// row's columns a narrow table moved off it, among the rest; both are empty for the Exit row.
func (r *run) header(footer, rest string) string {
	heading := r.table.Heading()
	if r.p.Kit.Term.ColourOn(r.p.Stdout) {
		heading = term.Bold + heading + term.Reset
	}
	if rest != "" {
		rest = "This row: " + rest
	}
	return ui.Header(
		"Enter lists what can be done to the row   Tab running sessions (the last row)   ? help   Esc or q quit",
		footer, rest, r.frame.newLine, r.frame.restoreLine, heading,
	)
}

func (p *Picker) pickerFrame(ctx context.Context) (frame, error) {
	restore := "off"
	wants := filepath.Join(p.Home, ".config/systemd/user/default.target.wants/ccy-sessions-restore.service")
	if _, err := os.Stat(wants); err == nil {
		restore = "on"
	} else if !errors.Is(err, fs.ErrNotExist) {
		return frame{}, fmt.Errorf("look for the restore service: %w", err)
	}
	newLine := "Ctrl-N starts a new ccy session in the row's team, in "
	switch here, err := p.Checkouts.Find(ctx, ""); {
	case err != nil:
		newLine = "Ctrl-N unavailable: " + err.Error()
	case here.Top == "":
		newLine = "Ctrl-N unavailable: " + p.Kit.Abbrev(p.Here()) + " is not a project folder (no git repository)"
	case !here.Host():
		newLine = "Ctrl-N unavailable: " + here.Refusal(p.Kit.Abbrev(here.Top))
	default:
		newLine += p.Kit.Abbrev(here.Top)
	}
	return frame{
		newLine: newLine,
		restoreLine: "RESTORE: whether the seat's session is started again after a reboot; session restore on this machine is " +
			restore + " (ccy_restore_sessions)",
	}, nil
}

// rowHeaders is the header of each row, with that row's actions in words, and last the
// Running sessions row's.
func (r *run) rowHeaders() []string {
	headers := make([]string, 0, len(r.snap.Rows)+1)
	for _, row := range r.snap.Rows {
		headers = append(headers, r.header(r.footerFor(row), r.table.Rest(row)))
	}
	return append(headers, r.header("This row: Enter or Tab shows the running sessions, and what can be done to each", ""))
}

// listHeading is the teams list's heading: how many teams there are, and which are not up.
func (r *run) listHeading() ui.Heading {
	context := plural(len(r.snap.Teams), "team")
	var down []string
	for _, team := range r.snap.Teams {
		if r.snap.StateOf(team) != systemd.HomeserverUp {
			down = append(down, team)
		}
	}
	if len(down) > 0 {
		context += " · not up: " + strings.Join(down, ", ")
	}
	return flow.Here().About(context)
}

// loop shows the list, does what a key asks, and shows the list again, until a flow leaves.
func (r *run) loop(ctx context.Context) error {
	for first := true; ; first = false {
		if err := r.once(ctx, first); err != nil {
			return err
		}
	}
}

// once is one pass of the loop: collect, show, and do what the key chosen asks. nil goes round
// again; leaving, with any exit code, is a leaveError.
func (r *run) once(ctx context.Context, first bool) error {
	p := r.p
	if err := p.Kit.Interrupted(ctx); err != nil {
		return err
	}
	s, err := p.collectWithSudo(ctx)
	if err != nil {
		return err
	}
	r.snap = s
	if len(s.Rows) == 0 {
		return p.offerCreate(ctx)
	}
	if r.view == viewSessions {
		return r.sessionsPass(ctx)
	}
	if first {
		if err = r.offerJoinHere(ctx); err != nil {
			return err
		}
	}
	return r.listPass(ctx)
}

// listPass shows the teams list once and does what was chosen: a row's action, from its menu
// or by the shortcut shown beside it there, or the list's own Running sessions row.
func (r *run) listPass(ctx context.Context) error {
	p := r.p
	r.table = board.Layout(r.snap.Rows, p.rowWidth())
	menus := make([]ui.Menu, len(r.snap.Rows))
	for i, row := range r.snap.Rows {
		menus[i] = r.menuFor(row)
	}
	picked, err := p.Kit.Choose(ctx, r.listHeading(), ui.PickSpec{
		Title: "Agent teams", Header: r.header("", ""), RowHeaders: r.rowHeaders(),
		Rows: r.shownRows(), Tags: r.tags(), Actions: []ui.Action{sessionsAction}, Menus: menus,
	})
	var notOffered *ui.NotOfferedError
	switch {
	case errors.Is(err, ui.ErrLeave):
		if err = p.Kit.Interrupted(ctx); err != nil {
			return err
		}
		return flow.Leave(term.ExitOK)
	case errors.Is(err, ui.ErrBack):
		if err = p.Kit.Interrupted(ctx); err != nil {
			return err
		}
		return r.count(flow.Done)
	case errors.As(err, &notOffered):
		p.Kit.Tell(nothingOnThisRow)
		p.Kit.BackToList(ctx)
		return r.count(flow.Done)
	case err != nil:
		return err
	case picked.Action == actSessions:
		r.view = viewSessions
		return nil
	}
	row, ok := r.snap.RowAt(picked.Tag)
	if !ok {
		p.Kit.Tell("That row has changed; the list has been refreshed.")
		p.Kit.BackToList(ctx)
		return nil
	}
	res, err := r.do(ctx, picked.Action, row)
	if err != nil {
		return err
	}
	return r.count(res)
}

// count tallies refusals in a row; any other outcome starts the tally again.
func (r *run) count(res flow.Result) error {
	if res != flow.Refused {
		r.noOps = 0
		return nil
	}
	r.noOps++
	if r.noOps < maxNoOps {
		return nil
	}
	r.p.Kit.Tell("Nothing attached or started after " + strconv.Itoa(maxNoOps) + " tries.")
	return flow.Leave(term.ExitFailure)
}

// shownRows is the rows as fzf shows them, in colour where colour is on.
func (r *run) shownRows() []string {
	colour := r.p.Kit.Term.ColourOn(r.p.Stdout)
	rows := make([]string, 0, len(r.snap.Rows))
	for _, row := range r.snap.Rows {
		if colour {
			rows = append(rows, r.table.Painted(row))
		} else {
			rows = append(rows, r.table.Text(row))
		}
	}
	return rows
}

// rowWidth is how wide a row of the list can be: the terminal less the frame around it; 0 when
// the terminal does not say, which lays the rows out with no limit.
func (p *Picker) rowWidth() int {
	if columns := p.Columns(); columns > ui.RowChrome {
		return columns - ui.RowChrome
	}
	return 0
}

// tags number the rows, so two rows that read the same are still two rows to choose from.
func (r *run) tags() []string {
	tags := make([]string, 0, len(r.snap.Rows))
	for i := range r.snap.Rows {
		tags = append(tags, strconv.Itoa(i))
	}
	return tags
}

// collectWithSudo is a pass over the machine; when the members can only be read as root it asks
// for sudo once, and reads again.
func (p *Picker) collectWithSudo(ctx context.Context) (*board.Snapshot, error) {
	s, err := p.Board.Collect(ctx)
	if err != nil {
		return nil, flow.Leave(term.ExitFailure)
	}
	if s.NeedSudo == "" {
		return s, nil
	}
	if !p.Kit.SudoReady(ctx, readWhy) {
		p.Kit.Term.PrintError("so the teams' members cannot be shown, and nothing was changed. " +
			"ccy-teams --list shows the teams without them.")
		return nil, flow.Leave(term.ExitFailure)
	}
	s, err = p.Board.Collect(ctx)
	if err != nil {
		return nil, flow.Leave(term.ExitFailure)
	}
	if s.NeedSudo != "" {
		p.Kit.Term.PrintError("sudo took your password but still refuses agent-bus list, so the members cannot be shown.")
		return nil, flow.Leave(term.ExitFailure)
	}
	return s, nil
}
