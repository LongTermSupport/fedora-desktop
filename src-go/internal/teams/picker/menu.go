package picker

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/login"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// The actions, by the key fzf reports for their shortcut. Attaching has none: it is what the
// menu of a seat does.
const (
	keyNew      = "ctrl-n"
	keyRestore  = "ctrl-r"
	keyRemove   = "ctrl-x"
	keyElement  = "ctrl-e"
	keyPassword = "ctrl-p"
	keyUpdate   = "ctrl-u"
	keyHelp     = "?"
	keyAttach   = "attach"
	// keyCoordinator, like keyAttach, has no shortcut: it is what the menu of a seat offers.
	keyCoordinator = "coordinator"
)

// backToList is the last line of every menu: leaving it does nothing and shows the list.
const backToList = "Back to the list"

// action is one thing that can be done to a row: its key, what it is called in the menu, and
// the few words the footer gives it.
type action struct {
	key string
	// label is the menu line; short is the footer's wording.
	label, short string
}

// shown is the shortcut as a person reads it ("Ctrl-E"); empty when there is none.
func (a action) shown() string {
	switch a.key {
	case keyAttach, keyCoordinator:
		return ""
	case keyHelp:
		return "?"
	}
	return "Ctrl-" + strings.ToUpper(strings.TrimPrefix(a.key, "ctrl-"))
}

// line is the menu line: the words, with the shortcut beside them.
func (a action) line() string {
	if a.shown() == "" {
		return a.label
	}
	return a.label + "   (" + a.shown() + ")"
}

// footerPart is the action as the footer says it: the shortcut and what it does.
func (a action) footerPart() string {
	if a.shown() == "" {
		return a.short + " via Enter"
	}
	return a.shown() + " " + a.short
}

// actionsFor is everything that can be done to row, the likeliest first.
func (r *run) actionsFor(row board.Row) []action {
	team := row.Team
	open := action{keyElement, "Open " + team + " in Element", "open " + team + " in Element"}
	start := action{keyNew, "Start a ccy session in " + team + ", in this folder", "start a session in " + team}
	help := action{keyHelp, "What the columns and keys mean", "help"}
	switch row.Kind {
	case board.KindTeam:
		return []action{
			open, start,
			{keyUpdate, "Update every team that is out of date or not built", "update out-of-date teams"},
			{keyRemove, "Take " + team + " down", "take " + team + " down"},
			help,
		}
	case board.KindHuman:
		return r.humanActions(row, open, start, help)
	case board.KindCcy:
		return r.seatActions(row, open, start, help)
	}
	return []action{{keyRemove, "Park " + row.Handle, "park it"}, open, start, help}
}

// humanActions are a person's: signing in when they never have, else Element first, and a new
// password named for the sessions it signs out.
func (r *run) humanActions(row board.Row, open, start, help action) []action {
	count, known := r.snap.SessionsOf(row.Team, row.Member)
	if known && count == 0 {
		signIn := action{keyPassword, "Sign in: set a password (shown once)", "set a password"}
		return []action{signIn, open, start, help}
	}
	password := action{keyPassword, "New password — " + login.SignOutPhrase(r.snap, row.Team, row.Member), "new password"}
	return []action{open, password, start, help}
}

// seatActions are a ccy seat's: attaching its session, then restore, a session of the team,
// Element, whether it is the team's coordinator, and taking the seat away.
func (r *run) seatActions(row board.Row, open, start, help action) []action {
	acts := []action{{keyAttach, "Attach this terminal to the seat's session", "attach its session"}}
	if current, recorded := r.snap.SeatRestore[row.Dir+"|"+row.Member+"@"+row.Team]; recorded {
		restore := action{keyRestore, "Start its session again after a reboot", "turn restore on"}
		if current {
			restore = action{keyRestore, "Stop starting its session again after a reboot", "turn restore off"}
		}
		acts = append(acts, restore)
	}
	acts = append(acts, start, open, r.coordinatorAction(row))
	if row.Dir == "-" {
		return append(acts, action{keyRemove, "Park " + row.Handle, "park it"}, help)
	}
	return append(acts, action{keyRemove, "Remove this seat", "remove this seat"}, help)
}

// coordinatorAction is making the seat's member the team's coordinator, or, when it is, stopping it
// being: the role is read from the pass the list was drawn from.
func (r *run) coordinatorAction(row board.Row) action {
	if r.snap.RoleOf(row.Team, row.Handle) == agentbus.RoleCoordinator {
		return action{keyCoordinator, "Stop being coordinator", "stop it being the coordinator"}
	}
	return action{keyCoordinator, "Make coordinator", "make it the coordinator"}
}

// menu is Enter on a row: everything that can be done to it, in words, each with its shortcut.
// Leaving the menu does nothing.
func (r *run) menu(ctx context.Context, row board.Row) (flow.Result, error) {
	p := r.p
	acts := r.actionsFor(row)
	lines := make([]string, len(acts))
	tags := make([]string, len(acts))
	for i, a := range acts {
		lines[i], tags[i] = a.line(), strconv.Itoa(i)
	}
	subject := row.Member + " in " + row.Team
	if row.Kind == board.KindTeam {
		subject = "team " + row.Team
	}
	picked, err := p.Kit.Choose(ctx, ui.PickSpec{
		Title: subject, Rows: lines, Tags: tags, ExitLabel: backToList,
		Header: ui.Header("Enter choose   Esc or q back to the list", "What to do with "+subject+":", aboutLine(row)),
	})
	if errors.Is(err, ui.ErrLeave) {
		return flow.Done, p.Kit.Interrupted(ctx)
	}
	if err != nil {
		return flow.Done, err
	}
	i, err := strconv.Atoi(picked.Tag)
	if err != nil || i < 0 || i >= len(acts) {
		p.Kit.Term.PrintError("the menu returned a choice that is not in it: " + picked.Tag)
		return flow.Done, flow.Leave(term.ExitFailure)
	}
	return r.do(ctx, acts[i].key, row)
}

// aboutLine is a team row's description, whole, for the header of its menu; "" for any other row.
func aboutLine(row board.Row) string {
	if row.Kind != board.KindTeam {
		return ""
	}
	return row.Member
}

// footerFor is the line of the header that names row's actions in words.
func (r *run) footerFor(row board.Row) string {
	acts := r.actionsFor(row)
	parts := make([]string, len(acts))
	for i, a := range acts {
		parts[i] = a.footerPart()
	}
	return "This row: " + strings.Join(parts, "   ")
}
