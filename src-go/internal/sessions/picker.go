package sessions

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// pickerKeys are the keys besides Enter that choose a row.
var pickerKeys = []string{"ctrl-t", "ctrl-x", "ctrl-n"}

// step is how an action ended: the picker is done (with an exit code), or goes round again,
// having used a try or not.
type step struct {
	done   bool
	code   term.ExitCode
	counts bool
}

var (
	// redraw is an action that settled something: the list comes back without using a try.
	redraw = step{}
	// tried is an action that did not get the person anywhere: it uses one of the tries.
	tried = step{counts: true}
)

func leave(code term.ExitCode) step { return step{done: true, code: code} }

// pickState is what the picker knows for its whole run.
type pickState struct {
	here        string
	projectHere bool
	header      string
	// detachedFrom is the sessions a take-over was tried on without attaching here: it may have
	// detached their other terminal, so after the tries run out "Nothing changed" would not be true.
	detachedFrom []string
}

// terminalRequired is what a picker run without a terminal says.
const terminalRequired = "ccy-sessions needs a terminal: it attaches sessions to the one you are in. ccy-sessions --list prints the table instead.\n"

// picker is the interactive list. Only the picker needs a terminal: it attaches sessions to the
// one it runs in. The subcommands run headless: the restore unit has no terminal, and a reboot
// helper is as likely to be run over ssh as from a desktop.
func (a *App) picker(ctx context.Context) term.ExitCode {
	if !a.IsTerminal(a.Stdin) || !a.IsTerminal(a.Stdout) {
		a.sayf(terminalRequired)
		return term.ExitFailure
	}
	st := a.newPickState(ctx)
	a.Probe.KeepSamples = true
	attempt := 1
	for {
		rows, err := a.rows(ctx)
		if err != nil {
			return term.ExitFailure
		}
		if len(rows) == 0 {
			return a.noSessions(ctx, st)
		}
		picked, err := a.choose(ctx, st, rows)
		if errors.Is(err, ui.ErrLeave) {
			return term.ExitOK
		}
		if err != nil {
			a.failErr(err)
			return term.ExitFailure
		}
		s := a.act(ctx, st, picked, rows)
		if s.done {
			return s.code
		}
		if !s.counts {
			continue
		}
		if attempt++; attempt > maxTries {
			a.outOfTries(st)
			return term.ExitFailure
		}
	}
}

// newPickState works out where the picker was started and what its header says. A new session
// is only ever started the normal way, by running ccy itself, which does every check and prompt
// it always does, and only from a project folder, which for ccy means a git work tree. Anywhere
// else the key is shown as unavailable rather than hidden.
func (a *App) newPickState(ctx context.Context) *pickState {
	pwd := a.Getenv("PWD")
	if pwd == "" {
		if wd, err := os.Getwd(); err == nil {
			pwd = wd
		}
	}
	here := term.AbbrevHome(pwd, a.Getenv("HOME"))
	project, err := a.Git.ProjectHere(ctx, "")
	st := &pickState{here: here, projectHere: err == nil && project}
	newLine := "Ctrl-N starts a new ccy session in " + here
	if !st.projectHere {
		newLine = "Ctrl-N unavailable: " + here + " is not a project folder (no git repository)"
	}
	st.header = ui.Header("Enter attach   Ctrl-T take over   Ctrl-X end   Ctrl-N new   Esc or q quit",
		newLine,
		`"open elsewhere" is attached in another terminal: Ctrl-T detaches that terminal and attaches this one; the session keeps running`,
		`CPU: since the list last redrew ("-" until it has), 100% is one core`,
		`network, token, SSH key: what its container is on and was started with — "no container" and "-" mean nothing to ask`)
	return st
}

// choose shows the rows and returns the one picked. Each row carries its index as a hidden tag,
// so what is acted on is the row's data, never its text.
func (a *App) choose(ctx context.Context, st *pickState, rows []tableRow) (ui.Picked, error) {
	home := a.Getenv("HOME")
	texts := make([]string, len(rows))
	tags := make([]string, len(rows))
	for i, r := range rows {
		texts[i], tags[i] = r.text(home), strconv.Itoa(i)
	}
	picked, err := a.Picker.Pick(ctx, ui.PickSpec{
		Title: "CCY sessions", Header: st.header, Keys: pickerKeys, Rows: texts, Tags: tags,
	})
	return picked, err
}

// noSessions is what the picker does when nothing is running: offer to start ccy in a project
// folder, or say how.
func (a *App) noSessions(ctx context.Context, st *pickState) term.ExitCode {
	if !st.projectHere {
		a.sayf("No CCY or cc sessions are running. Start one with ccy or cc in a project directory.\n")
		return term.ExitOK
	}
	yes, err := a.Picker.Confirm(ctx, "CCY sessions", "No sessions are running. Start ccy in "+st.here+"?", "start ccy here", false)
	if err != nil {
		a.failErr(err)
		return term.ExitFailure
	}
	if yes {
		return a.startHere(st).code
	}
	return term.ExitOK
}

// outOfTries is the last word after the tries are used. A take-over may have detached a
// terminal, so then "Nothing changed" would not be true.
func (a *App) outOfTries(st *pickState) {
	if len(st.detachedFrom) > 0 {
		a.sayf("Nothing attached after %d tries. A take-over was tried on %s; what each one did is above.\n",
			maxTries, strings.Join(st.detachedFrom, " "))
		return
	}
	a.sayf("Nothing attached after %d tries. Nothing changed.\n", maxTries)
}

// act does what the key asked of the row.
func (a *App) act(ctx context.Context, st *pickState, picked ui.Picked, rows []tableRow) step {
	i, err := strconv.Atoi(picked.Tag)
	if err != nil || i < 0 || i >= len(rows) {
		a.fail("the picker returned a row that is not in the list: " + picked.Row)
		return leave(term.ExitFailure)
	}
	row := rows[i]
	switch picked.Key {
	case "ctrl-n":
		return a.newHere(st)
	case "ctrl-x":
		return a.confirmEnd(ctx, row.Name)
	case "ctrl-t":
		return a.confirmTakeOver(ctx, st, row.Name)
	}
	if row.elsewhere() {
		a.sayf("'%s' is open in another terminal. Ctrl-T takes it over here; Ctrl-X ends it.\n", row.Name)
		return tried
	}
	return a.attachHere(ctx, row.Name)
}

// newHere starts ccy in this folder, when it is a project.
func (a *App) newHere(st *pickState) step {
	if st.projectHere {
		return a.startHere(st)
	}
	a.sayf("%s is not a project folder (no git repository), so ccy cannot be started here.\n", st.here)
	return tried
}

// startHere replaces this process with ccy, which then does its own checks and prompts. It
// returns only when ccy could not be started.
func (a *App) startHere(st *pickState) step {
	a.sayf("Starting ccy in %s.\n", st.here)
	if err := a.Execer.Exec(execx.Cmd{Name: launcher}); err != nil {
		a.failErr(err)
		return leave(term.ExitFailure)
	}
	return leave(term.ExitOK)
}

// confirmEnd ends a session and everything in it, after the picker's yes/no (no is the default
// row). An ended session is not a session to restore: its record goes with it, since a
// kill-session never lets the pane's trampoline remove it. A session that ended since the list
// was drawn is just as ended.
func (a *App) confirmEnd(ctx context.Context, name string) step {
	yes, err := a.Picker.Confirm(ctx, "End "+name+"?", "Everything running in it will be killed.", "end '"+name+"'", false)
	if err != nil {
		a.failErr(err)
		return leave(term.ExitFailure)
	}
	if !yes {
		a.sayf("Kept '%s'.\n", name)
		return redraw
	}
	gone := false
	if killErr := a.Tmux.KillSession(ctx, name); errors.Is(killErr, tmuxccy.ErrSessionGone) {
		gone = true
	} else if killErr != nil {
		a.failErr(killErr)
		return leave(term.ExitFailure)
	}
	store, err := registry.Locate(a.Getenv)
	if err == nil {
		err = store.Remove(name)
	}
	if err != nil {
		a.failErr(err)
		return leave(term.ExitFailure)
	}
	if gone {
		a.sayf("'%s' had already ended.\n", name)
	} else {
		a.sayf("Ended '%s'.\n", name)
	}
	return redraw
}

// attachHere attaches a detached session and ends the picker once the user detaches again. It
// returns only when the session was taken or ended between the list and the key, or another
// terminal was on it afterwards, which counts as a try.
func (a *App) attachHere(ctx context.Context, name string) step {
	outcome, err := a.Tmux.Attach(ctx, name)
	switch {
	case err == nil && outcome != tmuxccy.Kicked:
		return leave(term.ExitOK)
	case err == nil:
		// The library said why this terminal was not left attached.
		return tried
	case errors.Is(err, tmuxccy.ErrOpenElsewhere):
		a.failErr(err)
		a.sayf("'%s' was taken or ended just now — the list has been refreshed.\n", name)
	default:
		a.failErr(err)
		a.sayf("The list has been refreshed.\n")
	}
	return tried
}

// confirmTakeOver takes a session over from the terminal it is open on, after the picker's
// yes/no naming that terminal and how long it has been idle (no is the default row). It asks
// list-clients, the hook's own view, rather than trusting the row's state: a client tmux leaves
// out of the row's count can still hold the session. With nobody holding it, it attaches as
// Enter does. Declined, the list comes back without using a try.
func (a *App) confirmTakeOver(ctx context.Context, st *pickState, name string) step {
	others, err := a.Tmux.OtherTerminals(ctx, name)
	if errors.Is(err, tmuxccy.ErrSessionGone) {
		a.sayf("'%s' has ended — the list has been refreshed.\n", name)
		return tried
	}
	if err != nil {
		a.failErr(err)
		return leave(term.ExitFailure)
	}
	if others == "" {
		if s := a.attachHere(ctx, name); s.done {
			return s
		}
		return tried
	}
	yes, err := a.Picker.Confirm(ctx, "Take over "+name+"?",
		"It is open on "+others+". That terminal is detached, not closed, and the session keeps running.",
		"take over '"+name+"'", false)
	if err != nil {
		a.failErr(err)
		return leave(term.ExitFailure)
	}
	if !yes {
		a.sayf("Left '%s' where it is.\n", name)
		return redraw
	}
	return a.takeOver(ctx, st, name)
}

// takeOver detaches the other terminals and attaches this one. A session that ended, or was
// taken again before the attach, refreshes the list; anything else ends the picker.
func (a *App) takeOver(ctx context.Context, st *pickState, name string) step {
	outcome, err := a.Tmux.TakeOver(ctx, name, a.cfg.TakeOverWaitTenths)
	switch {
	case err == nil && outcome != tmuxccy.Kicked:
		return leave(term.ExitOK)
	case err == nil:
		return leave(term.ExitFailure)
	case errors.Is(err, tmuxccy.ErrSessionGone), errors.Is(err, tmuxccy.ErrOpenElsewhere):
		a.failErr(err)
		st.detachedFrom = append(st.detachedFrom, name)
		a.sayf("The list has been refreshed.\n")
		return tried
	}
	a.failErr(err)
	return leave(term.ExitFailure)
}
