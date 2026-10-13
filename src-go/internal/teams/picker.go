package teams

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"

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

// pickerKeys are the keys besides Enter that choose a row.
var pickerKeys = []string{"ctrl-n", "ctrl-r", "ctrl-x", "ctrl-e", "ctrl-p", "ctrl-u", "?"}

// result says how a key's action ended. Actions that start or attach end the picker through an
// exitRequest instead.
type result int

const (
	// done is an action that did what it was asked, or that the person called off.
	done result = iota
	// refused is an action that could not be done: it counts toward maxNoOps.
	refused
)

// leaveError is a flow ending the picker with an exit code: not a failure, though 0 and a
// failure both end it.
type leaveError struct {
	code int
}

func (e *leaveError) Error() string { return fmt.Sprintf("leave the picker with exit %d", e.code) }

func leave(code int) error { return &leaveError{code: code} }

// exitCodeOf is the exit code err asks for, if it asks for one.
func exitCodeOf(err error) (int, bool) {
	req, ok := errors.AsType[*leaveError](err)
	if !ok {
		return 0, false
	}
	return req.code, true
}

// pickerRun is one run of the picker: the header, built once, and the pass being shown.
type pickerRun struct {
	a      *App
	header string
	snap   *Snapshot
	noOps  int
}

// picker is ccy-teams with no argument: the list of teams, seats and people, and a key for
// each thing that can be done to one.
func (a *App) picker(ctx context.Context) int {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	return a.runPicker(ctx)
}

func (a *App) runPicker(ctx context.Context) int {
	if !a.Term.IsTerminal(a.Stdin) || !a.Term.IsTerminal(a.Stdout) {
		a.print(a.Stderr, "ccy-teams needs a terminal: it attaches sessions to the one you are in. "+
			"ccy-teams --list prints the table instead.\n")
		return term.ExitFailure
	}
	// The session only ends by leaving; an error that is not a leave has already said why.
	if code, ok := exitCodeOf(a.pickerSession(ctx)); ok {
		return code
	}
	return term.ExitFailure
}

// interrupted is the person's Ctrl-C, as a request to leave with 130.
func (a *App) interrupted(ctx context.Context) error {
	if ctx.Err() == nil {
		return nil
	}
	return a.cancelled()
}

func (a *App) cancelled() error {
	a.tell("Cancelled; nothing was changed.")
	return leave(term.ExitCancelled)
}

// tell says one line to the person, on stderr.
func (a *App) tell(msg string) {
	a.print(a.Stderr, msg+"\n")
}

func (a *App) abbrev(path string) string { return term.AbbrevHome(path, a.Home) }

// here is the folder the command was run in.
func (a *App) here() string {
	if pwd := a.Getenv("PWD"); pwd != "" {
		return pwd
	}
	wd, err := os.Getwd()
	if err != nil {
		return "the current folder"
	}
	return wd
}

// pickerSession is the start-up offers, then the loop. It always ends in a leaveError.
func (a *App) pickerSession(ctx context.Context) error {
	if err := a.interrupted(ctx); err != nil {
		return err
	}
	first := newSnapshot()
	if err := a.collectTeams(ctx, first); err != nil {
		return leave(term.ExitFailure)
	}
	if len(first.Teams) == 0 {
		return a.offerCreate(ctx)
	}
	if err := a.offerPathLimits(ctx); err != nil {
		return err
	}
	if err := a.offerUpdate(ctx); err != nil {
		return err
	}
	header, err := a.pickerHeader(ctx)
	if err != nil {
		a.Term.PrintError(err.Error())
		return leave(term.ExitFailure)
	}
	return (&pickerRun{a: a, header: header}).loop(ctx)
}

// pickerHeader is what stays above the rows: the keys, whether Ctrl-N can start a session here,
// what RESTORE means, and the column headings. It is built once.
func (a *App) pickerHeader(ctx context.Context) (string, error) {
	restore := "off"
	wants := filepath.Join(a.Home, ".config/systemd/user/default.target.wants/ccy-sessions-restore.service")
	if _, err := os.Stat(wants); err == nil {
		restore = "on"
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("look for the restore service: %w", err)
	}
	project, err := a.Git.ProjectHere(ctx, "")
	if err != nil {
		return "", fmt.Errorf("ask git where this is: %w", err)
	}
	newLine := "Ctrl-N unavailable: " + a.abbrev(a.here()) + " is not a project folder (no git repository)"
	if project {
		top, err := a.Git.TopLevel(ctx, "")
		if err != nil {
			return "", fmt.Errorf("ask git where this checkout is: %w", err)
		}
		newLine = "Ctrl-N starts a new ccy session in the row's team, in " + a.abbrev(top)
	}
	heading := headingText()
	if a.Term.ColourOn(a.Stdout) {
		heading = cBold + heading + cReset
	}
	return ui.Header(
		"Enter attach a seat / password for a person   Ctrl-P password Ctrl-E Element   ? help   Esc or q quit",
		"Ctrl-N new session   Ctrl-U update out-of-date teams   Ctrl-R restore on/off   Ctrl-X remove/park/take down",
		newLine,
		"RESTORE: whether the seat's session is started again after a reboot; session restore on this machine is "+
			restore+" (ccy_restore_sessions)",
		heading,
	), nil
}

// loop shows the list, does what a key asks, and shows the list again, until a flow leaves.
func (r *pickerRun) loop(ctx context.Context) error {
	for first := true; ; first = false {
		if err := r.once(ctx, first); err != nil {
			return err
		}
	}
}

// once is one pass of the loop: collect, show, and do what the key chosen asks. nil goes round
// again; leaving, with any exit code, is a leaveError.
func (r *pickerRun) once(ctx context.Context, first bool) error {
	a := r.a
	if err := a.interrupted(ctx); err != nil {
		return err
	}
	s, err := a.collectWithSudo(ctx)
	if err != nil {
		return err
	}
	r.snap = s
	if len(s.Rows) == 0 {
		return a.offerCreate(ctx)
	}
	if first {
		if err = r.offerJoinHere(ctx); err != nil {
			return err
		}
	}
	picked, err := a.choose(ctx, ui.PickSpec{
		Title: "Agent teams", Header: r.header, Keys: pickerKeys, Rows: r.shownRows(), Tags: r.tags(),
	})
	if errors.Is(err, ui.ErrLeave) {
		if err = a.interrupted(ctx); err != nil {
			return err
		}
		return leave(term.ExitOK)
	}
	if err != nil {
		return err
	}
	row, ok := s.rowAt(picked.Tag)
	if !ok {
		a.tell("That row has changed; the list has been refreshed.")
		a.backToList(ctx)
		return nil
	}
	res, err := r.act(ctx, picked.Key, row)
	if err != nil {
		return err
	}
	return r.count(res)
}

// count tallies refusals in a row; any other outcome starts the tally again.
func (r *pickerRun) count(res result) error {
	if res != refused {
		r.noOps = 0
		return nil
	}
	r.noOps++
	if r.noOps < maxNoOps {
		return nil
	}
	r.a.tell("Nothing attached or started after " + strconv.Itoa(maxNoOps) + " tries.")
	return leave(term.ExitFailure)
}

// shownRows is the rows as fzf shows them: in colour where colour is on.
func (r *pickerRun) shownRows() []string {
	colour := r.a.Term.ColourOn(r.a.Stdout)
	rows := make([]string, len(r.snap.Rows))
	for i, row := range r.snap.Rows {
		rows[i] = row.Text()
		if colour {
			rows[i] = row.Painted()
		}
	}
	return rows
}

// tags number the rows, so two rows that read the same are still two rows to choose from.
func (r *pickerRun) tags() []string {
	tags := make([]string, len(r.snap.Rows))
	for i := range tags {
		tags[i] = strconv.Itoa(i)
	}
	return tags
}

// rowAt is the row a picked tag numbers.
func (s *Snapshot) rowAt(tag string) (Row, bool) {
	i, err := strconv.Atoi(tag)
	if err != nil || i < 0 || i >= len(s.Rows) {
		return Row{}, false
	}
	return s.Rows[i], true
}

// collectWithSudo is a pass over the machine; when the members can only be read as root it asks
// for sudo once, and reads again.
func (a *App) collectWithSudo(ctx context.Context) (*Snapshot, error) {
	s, err := a.collect(ctx)
	if err != nil {
		return nil, leave(term.ExitFailure)
	}
	if s.NeedSudo == "" {
		return s, nil
	}
	if !a.sudoReady(ctx, readWhy) {
		a.Term.PrintError("so the teams' members cannot be shown, and nothing was changed. " +
			"ccy-teams --list shows the teams without them.")
		return nil, leave(term.ExitFailure)
	}
	s, err = a.collect(ctx)
	if err != nil {
		return nil, leave(term.ExitFailure)
	}
	if s.NeedSudo != "" {
		a.Term.PrintError("sudo took your password but still refuses agent-bus list, so the members cannot be shown.")
		return nil, leave(term.ExitFailure)
	}
	return s, nil
}

// hasTool is whether name is on PATH.
func (a *App) hasTool(name string) bool {
	_, err := a.LookPath(name)
	return err == nil
}

// needFzf refuses without fzf, naming the play that installs it.
func (a *App) needFzf() error {
	if !a.hasTool("fzf") {
		a.Term.PrintError("fzf is not installed; playbooks/imports/play-claude-yolo.yml installs it.")
		return leave(term.ExitFailure)
	}
	return nil
}

// choose is one pick from the picker. Leaving it (Esc, q, the Exit row) is ui.ErrLeave.
func (a *App) choose(ctx context.Context, spec ui.PickSpec) (ui.Picked, error) {
	if err := a.needFzf(); err != nil {
		return ui.Picked{}, err
	}
	picked, err := a.Picker.Pick(ctx, spec)
	if err != nil && !errors.Is(err, ui.ErrLeave) {
		a.reportErr(err)
		return ui.Picked{}, leave(term.ExitFailure)
	}
	return picked, err
}

// confirm is a yes/no in the picker: no, Esc and q are all a no.
func (a *App) confirm(ctx context.Context, title, question, action string, defaultYes bool) (bool, error) {
	if err := a.needFzf(); err != nil {
		return false, err
	}
	yes, err := a.Picker.Confirm(ctx, title, question, action, defaultYes)
	if err != nil {
		a.reportErr(err)
		return false, leave(term.ExitFailure)
	}
	return yes, nil
}

// inPicker turns the exit code of a flow run from the picker into what the picker does next:
// success and failure both go back to the list, with the failure named; a cancel ends it.
func (a *App) inPicker(label string, rc int) error {
	switch rc {
	case term.ExitOK:
		return nil
	case term.ExitCancelled:
		return a.cancelled()
	}
	a.tell(fmt.Sprintf("(%s failed, exit %d: see above.)", label, rc))
	return nil
}
