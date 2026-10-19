package picker

import (
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/coordinator"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
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
	// actDemote is the role action that makes a coordinator a worker: the member's own, or the
	// takeover that replaces the team's coordinator. It is destructive, so it is listed last.
	actDemote     = "demote"
	actLxc        = "lxc-add"
	actElementAll = "element-all"
	actShutdown   = "shutdown"
	actSettings   = "settings"
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

// actionsFor is everything that can be done to row, the likeliest first and every destructive
// action last (lastDestructive). Every row offers adding an LXC container's agent to its team,
// just after starting a ccy session in it, and a team's row and a running seat's then offer
// adding a running session (addActions): the add lines stay together.
func (r *run) actionsFor(row board.Row) []action {
	acts := r.kindActions(row)
	lxc := action{actLxc, "", "Add an LXC container's agent to " + row.Team, "add an LXC container to " + row.Team}
	adds := append([]action{lxc}, r.addActions(row)...)
	return lastDestructive(slices.Insert(acts, slices.IndexFunc(acts, func(a action) bool { return a.id == actNew })+1, adds...))
}

// destructive is whether choosing a parks, removes, takes down, shuts down or makes a
// coordinator a worker. Each still asks first, but none may be where the cursor starts.
func (a action) destructive() bool {
	return a.id == actRemove || a.id == actShutdown || a.id == actDemote
}

// lastDestructive is acts with every destructive action moved to the end, each group in its
// order: a menu never starts on one, and Park, Remove or Take down is just above Back.
func lastDestructive(acts []action) []action {
	ordered := slices.DeleteFunc(slices.Clone(acts), action.destructive)
	for _, a := range acts {
		if a.destructive() {
			ordered = append(ordered, a)
		}
	}
	return ordered
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
	}
	return r.memberActions(row, open, start, help)
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

// memberActions are an agent member's, whatever its kind (ccy, cc, LXC, host, docker, vm): a
// ccy seat's own first (attaching its session, restore), then its role, Element, a session of
// the team, help, and taking it away. Every kind gets the same role action in the same words
// (Plan 00203 Task 3.8), and one path changes it (seats.ToggleCoordinator). A member with no
// handle yet (a deferred LXC member, a seat ccy has not joined) has no role to change and
// nothing to park: only a ccy seat's own actions and its Remove seat, which go by its folder.
func (r *run) memberActions(row board.Row, open, start, help action) []action {
	handleless := row.Handle == "" || row.Handle == "-"
	seat := row.Kind == board.KindCcy && row.Dir != "-"
	acts := r.seatOwnActions(row)
	if !handleless {
		acts = append(acts, r.roleAction(row))
	}
	acts = append(acts, open, start, help)
	switch {
	case seat:
		return append(acts, action{actRemove, keyRemove, "Remove seat " + row.Label() + " from " + row.Team, "remove seat " + row.Label()})
	case handleless:
		return acts
	}
	return append(acts, action{actRemove, keyRemove, "Park " + row.Handle, "park it"})
}

// seatOwnActions are what only a ccy seat has, first in its menu: attaching its session, and
// whether it is started again after a reboot.
func (r *run) seatOwnActions(row board.Row) []action {
	if row.Kind != board.KindCcy {
		return nil
	}
	acts := []action{{actAttach, "", "Attach this terminal to the seat's session", "attach its session"}}
	current, recorded := r.snap.SeatRestore[row.Dir+"|"+row.Member+"@"+row.Team]
	switch {
	case !recorded:
	case current:
		acts = append(acts, action{actRestore, keyRestore, "Stop starting its session again after a reboot", "turn restore off"})
	default:
		acts = append(acts, action{actRestore, keyRestore, "Start its session again after a reboot", "turn restore on"})
	}
	return acts
}

// roleAction is making the member the team's coordinator, or, when it is, a worker: the role is
// read from the pass the list was drawn from. The two paths are worded apart (package
// coordinator): a team with no coordinator is offered one plainly; a team that has another is a
// takeover, which, like making the coordinator a worker, demotes someone and so is destructive.
func (r *run) roleAction(row board.Row) action {
	if r.snap.RoleOf(row.Team, row.Handle) == agentbus.RoleCoordinator {
		return action{actDemote, "", coordinator.WorkerChoice(row.Handle, row.Team), "make it a worker"}
	}
	holder := coordinator.Holding(r.snap.Members[row.Team], row.Handle, seats.SnapshotRunning(r.snap))
	if holder.Handle != "" {
		return action{actDemote, "", coordinator.TakeoverChoice(holder), "replace the coordinator"}
	}
	return action{actCoordinator, "", coordinator.MakeChoice(row.Handle, row.Team), "make it the coordinator"}
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
		Heading: rowHeading(row),
	}
}

// rowHeading is the heading of a row's menu: Teams › team, and the member for a member's row,
// with what the row is as its context.
func rowHeading(row board.Row) ui.Heading {
	if row.Kind == board.KindTeam {
		return flow.Here(row.Team).About("team " + row.Team + " · " + row.State)
	}
	return flow.Here(row.Team, row.Crumb()).About(string(row.Kind) + " " + row.Member + " in team " + row.Team + " · " + row.State)
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
