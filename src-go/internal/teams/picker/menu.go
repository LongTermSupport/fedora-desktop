package picker

import (
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/coordinator"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/login"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seats"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// The actions on a row, by ID. Every one is a line of the row's menu (Enter); some also have a
// shortcut key, which ui prints beside the line and accepts on the row from the list.
const (
	actNew         = "new"
	actRestore     = "restore"
	actRemove      = "remove"
	actElement     = "element"
	actPassword    = "password"
	actUpdate      = "update"
	actHelp        = "help"
	actAttach      = "attach"
	actCoordinator = "coordinator"
	actLxc         = "lxc-add"
	actElementAll  = "element-all"
	actShutdown    = "shutdown"
	actSettings    = "settings"
	// actAskDescription and actAskLimit undo a never answer to an offer: menu lines of a team
	// marked never, with no shortcut.
	actAskDescription = "ask-description"
	actAskLimit       = "ask-limit"
)

// The shortcut keys, as fzf names them. Attaching, the coordinator role, adding an LXC
// container's agent, opening every team and shutting a team's sessions down have none: they are
// menu lines only, and a key that ends sessions is too easy to press by mistake.
const (
	keyNew      = "ctrl-n"
	keyRestore  = "ctrl-r"
	keyRemove   = "ctrl-x"
	keyElement  = "ctrl-e"
	keyPassword = "ctrl-p"
	keyUpdate   = "ctrl-u"
	keyHelp     = "?"
)

// backToList is the last line of every question: leaving it does nothing and shows the list.
const backToList = "Back to the list"

// action is one thing that can be done to a row: its ID, its shortcut ("" for none), what it is
// called in the menu, and the few words the footer gives it.
type action struct {
	id, key string
	// label is the menu line; short is the footer's wording.
	label, short string
}

// ui is the action as the picker lists it.
func (a action) ui() ui.Action { return ui.Action{ID: a.id, Label: a.label, Key: a.key} }

// footerPart is the action as the footer says it: the shortcut and what it does.
func (a action) footerPart() string {
	if a.key == "" {
		return a.short + " via Enter"
	}
	return ui.KeyName(a.key) + " " + a.short
}

// actionsFor is everything that can be done to row, the likeliest first. Every row offers
// adding an LXC container's agent to its team, just after starting a ccy session in it.
func (r *run) actionsFor(row board.Row) []action {
	acts := r.kindActions(row)
	lxc := action{actLxc, "", "Add an LXC container's agent to " + row.Team, "add an LXC container to " + row.Team}
	return slices.Insert(acts, slices.IndexFunc(acts, func(a action) bool { return a.id == actNew })+1, lxc)
}

// kindActions is what can be done to row because of what it is.
func (r *run) kindActions(row board.Row) []action {
	team := row.Team
	open := action{actElement, keyElement, "Open " + team + " in Element", "open " + team + " in Element"}
	start := action{actNew, keyNew, "Start a ccy session in " + team + ", in this folder", "start a session in " + team}
	help := action{actHelp, keyHelp, "What the columns and keys mean", "help"}
	switch row.Kind {
	case board.KindTeam:
		return append(append([]action{
			open,
			{actElementAll, "", "Open every team in Element, a window each", "open every team in Element"},
			start,
			{actSettings, "", "Edit settings", "edit " + team + "'s settings"},
			{actUpdate, keyUpdate, "Update every team that is out of date or not built", "update out-of-date teams"},
			{actShutdown, "", "Shut down every session of " + team, "shut down " + team + "'s sessions"},
			{actRemove, keyRemove, "Take " + team + " down", "take " + team + " down"},
		}, r.askAgainActions(team)...), help)
	case board.KindHuman:
		return r.humanActions(row, open, start, help)
	case board.KindCcy:
		return r.seatActions(row, open, start, help)
	}
	return []action{{actRemove, keyRemove, "Park " + row.Handle, "park it"}, open, start, help}
}

// askAgainActions undo a never answer to the description or limit offer of team.
func (r *run) askAgainActions(team string) []action {
	var acts []action
	if r.snap.Description[team] == "" && saidNever(r.p.descriptionMarker(team)) {
		acts = append(acts, action{actAskDescription, "", askDescriptionLabel(team), "be asked to describe " + team + " again"})
	}
	if saidNever(r.p.pathLimitMarker(team)) {
		acts = append(acts, action{actAskLimit, "", askLimitLabel(team), "be asked about " + team + "'s limit again"})
	}
	return acts
}

// humanActions are a person's: signing in when they never have, else Element first, and a new
// password named for the sessions it signs out.
func (r *run) humanActions(row board.Row, open, start, help action) []action {
	count, known := r.snap.SessionsOf(row.Team, row.Member)
	if known && count == 0 {
		signIn := action{actPassword, keyPassword, "Sign in: set a password (shown once)", "set a password"}
		return []action{signIn, open, start, help}
	}
	password := action{actPassword, keyPassword, "New password — " + login.SignOutPhrase(r.snap, row.Team, row.Member), "new password"}
	return []action{open, password, start, help}
}

// seatActions are a ccy seat's: attaching its session, then restore, a session of the team,
// Element, whether it is the team's coordinator, and taking the seat away.
func (r *run) seatActions(row board.Row, open, start, help action) []action {
	acts := []action{{actAttach, "", "Attach this terminal to the seat's session", "attach its session"}}
	if current, recorded := r.snap.SeatRestore[row.Dir+"|"+row.Member+"@"+row.Team]; recorded {
		restore := action{actRestore, keyRestore, "Start its session again after a reboot", "turn restore on"}
		if current {
			restore = action{actRestore, keyRestore, "Stop starting its session again after a reboot", "turn restore off"}
		}
		acts = append(acts, restore)
	}
	role, takeover := r.coordinatorAction(row)
	if !takeover {
		acts = append(acts, start, open, role)
	} else {
		acts = append(acts, start, open)
	}
	remove := action{actRemove, keyRemove, "Remove seat " + row.Label() + " from " + row.Team, "remove seat " + row.Label()}
	if row.Dir == "-" {
		remove = action{actRemove, keyRemove, "Park " + row.Handle, "park it"}
	}
	acts = append(acts, remove)
	if takeover {
		acts = append(acts, role)
	}
	return append(acts, help)
}

// coordinatorAction is making the seat's member the team's coordinator, or, when it is, stopping it
// being: the role is read from the pass the list was drawn from. The two paths are worded apart
// (package coordinator); takeover is a team that has another coordinator, a rarer choice that
// the menu lists after the ordinary ones.
func (r *run) coordinatorAction(row board.Row) (action, bool) {
	if r.snap.RoleOf(row.Team, row.Handle) == agentbus.RoleCoordinator {
		return action{actCoordinator, "", "Stop being coordinator", "stop it being the coordinator"}, false
	}
	holder := coordinator.Holding(r.snap.Members[row.Team], row.Handle, seats.SnapshotRunning(r.snap))
	if holder.Handle != "" {
		return action{actCoordinator, "", coordinator.TakeoverChoice(holder), "replace the coordinator"}, true
	}
	return action{actCoordinator, "", coordinator.NoneChoice(row.Team), "make it the coordinator"}, false
}

// menuFor is Enter on a row: everything that can be done to it, in words, each with its shortcut.
// Leaving the menu does nothing.
func (r *run) menuFor(row board.Row) ui.Menu {
	acts := r.actionsFor(row)
	items := make([]ui.Action, len(acts))
	for i, a := range acts {
		items[i] = a.ui()
	}
	subject := row.Label() + " in " + row.Team
	if row.Kind == board.KindTeam {
		subject = "team " + row.Team
	}
	return ui.Menu{
		Title:   subject,
		Header:  ui.Header("Enter choose   Esc or q back to the list", "What to do with "+subject+":", aboutLine(row)),
		Actions: items,
	}
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
