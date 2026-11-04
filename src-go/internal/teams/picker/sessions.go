package picker

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/coordinator"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/hot"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seats"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// SessionControl is hot control as the picker uses it (Plan 00203 SPEC-hot-control.md M5): the
// running sessions, and `ccy-teams session` run with the words a person would type, so a change
// made from the picker takes the same steps, questions, locks and rollback as on the command line.
type SessionControl interface {
	Sessions(ctx context.Context) ([]hot.Session, error)
	Run(ctx context.Context, args []string) term.ExitCode
}

// actSessions is the teams list's own row that shows the running sessions.
const actSessions = "sessions"

// The rows of the lists that are choices, not teams or sessions, each with its shortcut, which
// ui prints beside it.
var (
	sessionsAction     = ui.Action{ID: actSessions, Label: "Running sessions: change a session's teams while it runs", Key: "tab"}
	sessionsHelpAction = ui.Action{ID: actHelp, Label: "What this list shows", Key: keyHelp}
)

// view is which list the picker shows: the teams, or the running sessions.
type view int

const (
	viewTeams view = iota
	viewSessions
)

const (
	sessionsKeyLine = "Enter lists what can be done to the session   ? help   Esc back to the teams"
	questionKeyLine = "Enter choose   Esc or q back to the list"
	roleWorker      = agentbus.RoleWorker
	roleCoordinator = agentbus.RoleCoordinator
	// roleTakeover is the coordinator's role taken from the member that holds it.
	roleTakeover = "takeover"
)

// sessionsPass is one pass of the Running sessions view: list, show, do what the key asks.
func (r *run) sessionsPass(ctx context.Context) error {
	p := r.p
	sessions, err := p.Session.Sessions(ctx)
	var heading string
	var lines []string
	if err == nil {
		heading, lines, err = hot.SessionLines(sessions, p.Home)
	}
	if err != nil {
		r.view = viewTeams
		res, refuseErr := p.Kit.Refuse(ctx, "The running sessions cannot be listed: "+err.Error()+".")
		if refuseErr != nil {
			return refuseErr
		}
		return r.count(res)
	}
	tags := make([]string, 0, len(lines))
	for i := range lines {
		tags = append(tags, strconv.Itoa(i))
	}
	picked, err := p.Kit.Choose(ctx, flow.Here(runningCrumb).About(plural(len(sessions), "session")+" running"), ui.PickSpec{
		Title: "Running sessions", Header: ui.Header(sessionsKeyLine, hot.OnlyTmuxLine, heading),
		Rows: lines, Tags: tags, Actions: []ui.Action{sessionsHelpAction}, ExitLabel: "Back to the teams",
	})
	var notOffered *ui.NotOfferedError
	switch {
	case errors.Is(err, ui.ErrLeave):
		r.view = viewTeams
		return p.Kit.Interrupted(ctx)
	case errors.As(err, &notOffered):
		p.Kit.Tell(nothingOnThisRow)
		p.Kit.BackToList(ctx)
		return nil
	case err != nil:
		return err
	case picked.Action == actHelp:
		p.showHelp(ctx)
		return nil
	}
	i := slices.Index(tags, picked.Tag)
	if i < 0 {
		p.Kit.Tell("That row has changed; the list has been refreshed.")
		p.Kit.BackToList(ctx)
		return nil
	}
	res, err := r.sessionMenu(ctx, sessions[i])
	if err != nil {
		return err
	}
	return r.count(res)
}

// sessionChoice is one line of a session's menu: what it says, and what choosing it does.
type sessionChoice struct {
	line string
	do   func(context.Context) (flow.Result, error)
}

// sessionChoices is everything that can be done to s, in words, each naming the team it acts
// on; nothing is reached by a key alone.
func (r *run) sessionChoices(s hot.Session) []sessionChoice {
	var cs []sessionChoice
	if s.Controllable {
		cs = append(cs, sessionChoice{"Add to a team", func(ctx context.Context) (flow.Result, error) { return r.addToTeam(ctx, s) }})
		for _, c := range s.Teams {
			cs = append(cs, sessionChoice{"Remove from " + c.Team, func(ctx context.Context) (flow.Result, error) { return r.removeFromTeam(ctx, s, c) }})
		}
		for _, c := range s.Teams {
			cs = append(cs, sessionChoice{"Change role in " + c.Team + " (now " + c.Role + ")", func(ctx context.Context) (flow.Result, error) { return r.roleInTeam(ctx, s, c) }})
		}
		for _, c := range s.Teams {
			line := "Move to another team"
			if len(s.Teams) > 1 {
				line = "Move from " + c.Team + " to another team"
			}
			cs = append(cs, sessionChoice{line, func(ctx context.Context) (flow.Result, error) { return r.moveTeams(ctx, s, c) }})
		}
	}
	cs = append(cs, sessionChoice{"Attach", func(ctx context.Context) (flow.Result, error) { return r.p.Seats.AttachSession(ctx, r.snap, s.Name) }})
	if s.Unguarded {
		cs = append(cs, sessionChoice{"Repair (its seat keeper has stopped)", func(ctx context.Context) (flow.Result, error) {
			return r.session(ctx, []string{"repair", s.Name})
		}})
	}
	return cs
}

// sessionMenu is Enter on a session: what can be done to it, in words. Leaving does nothing.
func (r *run) sessionMenu(ctx context.Context, s hot.Session) (flow.Result, error) {
	cs := r.sessionChoices(s)
	opts := make([]option, len(cs))
	for i, c := range cs {
		opts[i] = option{line: c.line}
	}
	question := s.Name + ": what to do (" + s.State + ")"
	if !s.Controllable {
		question += "\n" + s.Name + " cannot be changed while it runs. Relaunch it from ccy's tmux to change it."
	}
	o, res, ok, err := r.ask(ctx, sessionHeading(s).About(sessionContext(s)), question, opts)
	if !ok {
		return res, err
	}
	return cs[o.at].do(ctx)
}

// option is one answer to a question: its line, the value it gives, whether it names a seat
// that exists, and why it cannot be chosen ("" when it can). at is its place among the answers.
type option struct {
	line, value, refused string
	// handle is the member a seat answer names, "" for none.
	handle string
	exists bool
	at     int
}

// ask is one question in the picker. A refused answer is said and the question asked again,
// up to maxNoOps times (CLAUDE/InteractiveScripts.md). When ok is false, res and err are what the
// flow returns: leaving the question changes nothing, running out of tries is a refusal.
func (r *run) ask(ctx context.Context, h ui.Heading, question string, opts []option) (option, flow.Result, bool, error) {
	k := r.p.Kit
	lines, tags := make([]string, len(opts)), make([]string, len(opts))
	for i, o := range opts {
		lines[i], tags[i] = o.line, strconv.Itoa(i)
	}
	for try := 1; ; try++ {
		picked, err := k.Choose(ctx, h, ui.PickSpec{
			Title: h.Crumbs[len(h.Crumbs)-1], Rows: lines, Tags: tags, ExitLabel: backToList, Header: ui.Header(questionKeyLine, question),
		})
		if errors.Is(err, ui.ErrLeave) {
			return option{}, flow.Done, false, k.Interrupted(ctx)
		}
		if err != nil {
			return option{}, flow.Done, false, err
		}
		i, err := strconv.Atoi(picked.Tag)
		if err != nil || i < 0 || i >= len(opts) {
			k.Term.PrintError("the question returned a choice that is not in it: " + picked.Tag)
			return option{}, flow.Done, false, flow.Leave(term.ExitFailure)
		}
		if opts[i].refused == "" {
			chosen := opts[i]
			chosen.at = i
			return chosen, flow.Done, true, nil
		}
		k.Tell(opts[i].refused)
		if try >= maxNoOps {
			res, err := k.Refuse(ctx, "Nothing was changed after "+strconv.Itoa(maxNoOps)+" tries.")
			return option{}, res, false, err
		}
	}
}

// session runs `ccy-teams session <args>`: it asks its own question and prints each step; the
// list then waits until what it printed has been read.
func (r *run) session(ctx context.Context, args []string) (flow.Result, error) {
	k := r.p.Kit
	code := r.p.Session.Run(ctx, args)
	if err := k.InPicker("ccy-teams session "+strings.Join(args, " "), code); err != nil {
		return flow.Done, err
	}
	k.BackToList(ctx)
	return flow.Done, nil
}

// addToTeam is section 5.2: the team, the seat and the role, then `session add`.
func (r *run) addToTeam(ctx context.Context, s hot.Session) (flow.Result, error) {
	h := sessionHeading(s, "Add to a team")
	team, res, ok, err := r.ask(ctx, h.AtStep(1, 3, "Team").About("session "+s.Name), "Add "+s.Name+" to which team?", r.teamOptions(s, ""))
	if !ok {
		return res, err
	}
	seat, res, ok, err := r.askSeat(ctx, s, team.value, h.AtStep(2, 3, "Seat").About("session "+s.Name+" · team "+team.value))
	if !ok {
		return res, err
	}
	holder := coordinator.Holding(r.snap.Members[team.value], seat.handle, seats.SnapshotRunning(r.snap))
	current := ""
	if seat.handle != "" && r.snap.RoleOf(team.value, seat.handle) == roleCoordinator {
		current = roleCoordinator // the seat's handle holds the role already (Fable's N1)
	}
	seatWord := "the lowest free seat"
	if seat.value != "" {
		seatWord = "seat " + seat.value
	}
	role, res, ok, err := r.ask(ctx, h.AtStep(3, 3, "Role").About("session "+s.Name+" · team "+team.value+" · "+seatWord),
		"Role in "+team.value+":", roleOptions(team.value, current, holder))
	if !ok {
		return res, err
	}
	where := team.value
	if seat.value != "" {
		where += "/" + seat.value
	}
	args := []string{"add", s.Name, where}
	switch {
	case role.value == roleTakeover:
		args = append(args, "--role="+roleCoordinator, "--demote-current")
	case role.value == roleCoordinator:
		args = append(args, "--role="+roleCoordinator)
	case seat.exists:
		args = append(args, "--role="+roleWorker)
	}
	return r.session(ctx, args)
}

// teamOptions is every team, each refused when s cannot join it now; leaving is left out.
func (r *run) teamOptions(s hot.Session, leaving string) []option {
	var opts []option
	for _, team := range r.snap.Teams {
		if team == leaving {
			continue
		}
		o := option{line: r.teamLine(team), value: team}
		if seat, in := cellOf(s, team); in {
			o.refused = s.Name + " is already in " + team + " as seat " + seat.Seat + ": one seat per team per session."
		} else if !r.snap.Loaded[team] {
			o.refused = team + " is not installed: ccy-teams apply " + team + " builds it."
		} else if systemd.StateWord(r.snap.Active[team]) != systemd.HomeserverUp {
			o.refused = team + "'s homeserver is down, so nothing can join it: systemctl status agent-bus-hs@" + team
		}
		opts = append(opts, o)
	}
	return opts
}

// teamLine is a team as the add question shows it: what it is for, its homeserver and size.
func (r *run) teamLine(team string) string {
	about := board.NoDescription
	if d := r.snap.Description[team]; d != "" {
		about = strconv.Quote(d)
	}
	size := "members unknown"
	if why := r.snap.MembersUnknown[team]; why != "" {
		size = "members: " + why
	} else if roster, read := r.snap.Members[team]; read {
		size = strconv.Itoa(len(roster.Members)) + " agents"
	}
	return fmt.Sprintf("%-16s %s   %s   %s", team, about, systemd.StateWord(r.snap.Active[team]), size)
}

// askSeat is the seat of team in s's checkout: the lowest free one (ccy-teams chooses, the
// default), or one agent-bus offers that no process holds.
func (r *run) askSeat(ctx context.Context, s hot.Session, team string, h ui.Heading) (option, flow.Result, bool, error) {
	offered, err := r.p.Seats.Bus.SeatChooseIn(ctx, s.Checkout, team, "podman")
	if err != nil {
		res, refuseErr := r.p.Kit.Refuse(ctx, "The seats of "+team+" in "+r.p.Kit.Abbrev(s.Checkout)+" cannot be read: "+err.Error())
		return option{}, res, false, refuseErr
	}
	opts := []option{{line: "The lowest free seat (ccy-teams chooses it)"}}
	for _, c := range offered.Rows {
		if c.State == "held" {
			continue
		}
		opts = append(opts, option{line: "seat " + c.Seat + " (" + c.State + ", " + c.Handle + ")", value: c.Seat, handle: c.Handle, exists: c.State != "new"})
	}
	return r.ask(ctx, h, "Seat in "+team+" ("+r.p.Kit.Abbrev(s.Checkout)+"):", opts)
}

// roleOptions is worker, the ordinary choice, first; then the coordinator's role in one of the
// two paths (package coordinator): held now, offered plainly on a team with none, or, when
// holder holds it, the takeover in its own words, which passes --demote-current and whose
// question, asked by the command, defaults to no. current marks the role held now.
func roleOptions(team, current string, holder coordinator.Holder) []option {
	worker := option{line: roleWorker, value: roleWorker}
	if current == roleWorker {
		worker.line += " (now)"
	}
	switch {
	case current == roleCoordinator:
		return []option{worker, {line: roleCoordinator + " (now)", value: roleCoordinator}}
	case holder.Handle != "":
		return []option{worker, {line: coordinator.TakeoverChoice(holder), value: roleTakeover}}
	}
	return []option{worker, {line: coordinator.NoneChoice(team), value: roleCoordinator}}
}

// removeFromTeam is section 5.3: what becomes of a coordinator's role, then `session remove`
// for the team the menu line named.
func (r *run) removeFromTeam(ctx context.Context, s hot.Session, cell hot.Cell) (flow.Result, error) {
	h := sessionHeading(s, "Remove from "+cell.Team).About("session " + s.Name + " · seat " + cell.Seat + " in team " + cell.Team)
	coordinator, res, ok, err := r.askCoordinator(ctx, s, cell, h)
	if !ok {
		return res, err
	}
	return r.session(ctx, append([]string{"remove", s.Name, cell.Team}, coordinator...))
}

// moveTeams is `session move` out of the team from: the team it joins, and a coordinator's role.
func (r *run) moveTeams(ctx context.Context, s hot.Session, from hot.Cell) (flow.Result, error) {
	h := sessionHeading(s, "Move from "+from.Team)
	to, res, ok, err := r.ask(ctx, h.About("session "+s.Name+" · seat "+from.Seat+" in team "+from.Team),
		"Move "+s.Name+" from "+from.Team+" to which team?", r.teamOptions(s, from.Team))
	if !ok {
		return res, err
	}
	coordinator, res, ok, err := r.askCoordinator(ctx, s, from, h.About("session "+s.Name+" · from team "+from.Team+" to team "+to.value))
	if !ok {
		return res, err
	}
	return r.session(ctx, append([]string{"move", s.Name, from.Team, to.value}, coordinator...))
}

// askCoordinator is the --coordinator option for a seat leaving its team: none when the seat
// is not the team's coordinator, as far as the board's roster says; otherwise the role goes to
// no one, or to another active member. With the roster unread, `session remove` asks itself.
func (r *run) askCoordinator(ctx context.Context, s hot.Session, cell hot.Cell, h ui.Heading) ([]string, flow.Result, bool, error) {
	handle := r.seatHandle(s.Checkout, cell)
	if handle == "" || r.snap.RoleOf(cell.Team, handle) != roleCoordinator {
		return nil, flow.Done, true, nil
	}
	opts := []option{{line: "The role goes to no one (seat " + cell.Seat + " becomes a worker)", value: "demote"}}
	for _, m := range r.snap.Members[cell.Team].Members {
		if m.Handle != handle && !m.Parked {
			opts = append(opts, option{line: "Hand it to " + m.Handle, value: "hand-to=" + m.Handle})
		}
	}
	o, res, ok, err := r.ask(ctx, h.Push("Coordinator role"), "Its seat "+cell.Seat+" is "+cell.Team+"'s coordinator. After it leaves:", opts)
	if !ok {
		return nil, res, false, err
	}
	return []string{"--coordinator=" + o.value}, flow.Done, true, nil
}

// roleInTeam is `session role` in the team the menu line named: the role.
func (r *run) roleInTeam(ctx context.Context, s hot.Session, cell hot.Cell) (flow.Result, error) {
	h := sessionHeading(s, "Role in "+cell.Team).About("session " + s.Name + " · seat " + cell.Seat + " in team " + cell.Team + " · now " + cell.Role)
	holder := coordinator.Holding(r.snap.Members[cell.Team], r.seatHandle(s.Checkout, cell), seats.SnapshotRunning(r.snap))
	role, res, ok, err := r.ask(ctx, h, "Role of seat "+cell.Seat+" in "+cell.Team+":", roleOptions(cell.Team, cell.Role, holder))
	if !ok {
		return res, err
	}
	args := []string{"role", s.Name, cell.Team, role.value}
	if role.value == roleTakeover {
		args = []string{"role", s.Name, cell.Team, roleCoordinator, "--demote-current"}
	}
	return r.session(ctx, args)
}

// seatHandle is the handle of a seat of checkout, as the board's seat rows have it.
func (r *run) seatHandle(checkout string, cell hot.Cell) string {
	for _, row := range r.snap.Rows {
		if row.Kind == board.KindCcy && row.Dir == checkout && row.Team == cell.Team && row.Member == cell.Seat {
			return row.Handle
		}
	}
	return ""
}

// runningCrumb is the crumb of the Running sessions view.
const runningCrumb = "Running sessions"

// sessionHeading is the heading of a screen about the running session s: Teams › Running
// sessions › its name, then crumbs.
func sessionHeading(s hot.Session, crumbs ...string) ui.Heading {
	return flow.Here(append([]string{runningCrumb, s.Name}, crumbs...)...)
}

// sessionContext is a running session in one line: its state and the teams it is in.
func sessionContext(s hot.Session) string {
	teams := make([]string, len(s.Teams))
	for i, c := range s.Teams {
		teams[i] = c.Team + " (" + c.Role + ")"
	}
	in := "in no team"
	if len(teams) > 0 {
		in = "in " + strings.Join(teams, ", ")
	}
	return "session " + s.Name + " · " + s.State + " · " + in
}

// plural is n and word, with an s unless n is one.
func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// cellOf is s's seat in team, if it is in it.
func cellOf(s hot.Session, team string) (hot.Cell, bool) {
	for _, c := range s.Teams {
		if c.Team == team {
			return c, true
		}
	}
	return hot.Cell{}, false
}
