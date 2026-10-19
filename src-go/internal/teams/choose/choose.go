// Package choose is what the ccy launcher asks of ccy-teams for `ccy --teams` when it has a
// person to ask: choose-seat, which chooses the seat a session takes in a team and then offers
// the seat's role (keep it, make it the coordinator of a team that has none, replace a team's
// coordinator, or stop it being one: package coordinator words the two paths), and describe-seats,
// which names each seat of a saved launch with its role for Quick Launch's summary. The role is
// changed by seats.ChangeRole, the code the seat menu of the picker calls, and sudo is used
// only when the person changes it. The payload is on stdout and everything else on stderr, as
// the launcher captures the output.
package choose

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/coordinator"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seats"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// exitConfig is the launcher's code for a team or a tool that is not set up here (agent-bus's
// own 78), used where agent-bus did not give a code.
const exitConfig term.ExitCode = 78

// stateNew is a seat the checkout does not have yet: its member is made when the launch starts.
const stateNew = "new"

// Words of the role question.
const (
	stopRole    = "Stop being coordinator"
	keepPrefix  = "Keep as is: "
	noRoleAsked = "Its role is left as it is."
)

// Choose is the launcher's chooser. Every dependency is a field so a test stands in for each.
type Choose struct {
	Host  *host.Host
	Bus   *agentbus.Client
	Gum   *ui.Gum
	Kit   *flow.Kit
	Seats *seats.Seats
}

// New is the chooser over a host.
func New(h *host.Host, kit *flow.Kit, s *seats.Seats) *Choose {
	return &Choose{Host: h, Bus: h.Bus, Gum: h.Gum, Kit: kit, Seats: s}
}

// row is a seat as the chooser shows it: the seat, its member's full handle, what runs in it,
// and whether it is free.
func row(c agentbus.Choice) string {
	state := c.State
	switch c.State {
	case "free":
	case "held":
		state = "held by a running session"
	case stateNew:
		state = "new seat"
	}
	return fmt.Sprintf("seat %-6s  %-40s  %-15s  %s", c.Seat, c.Handle, c.Words, state)
}

func keepRole(role string) string { return keepPrefix + role }

// Seat is `ccy-teams choose-seat TEAM`: the seat this session takes in team, as SEAT@TEAM on
// stdout. Leaving the chooser prints nothing and succeeds: the person chose not to start.
func (c *Choose) Seat(ctx context.Context, args []string) term.ExitCode {
	if len(args) != 1 {
		c.say("ccy-teams: 'choose-seat' takes one team. See ccy-teams --help.")
		return term.ExitUsage
	}
	ref, code := c.PickSeat(ctx, flow.LaunchTeam(1, 1, args[0]), args[0], "")
	if ref != "" {
		c.Host.Print(c.Host.Stdout, ref+"\n")
	}
	return code
}

// PickSeat chooses the seat this session takes in team, for a launcher of memberType ("" for
// agent-bus's default), then offers its role, each question under h (which team of the launch
// this is). The ref is SEAT@TEAM; it is empty with ExitOK when the person left a question, and
// nothing is started.
func (c *Choose) PickSeat(ctx context.Context, h ui.Heading, team, memberType string) (string, term.ExitCode) {
	choices, err := c.Bus.SeatChooseAs(ctx, team, memberType)
	if err != nil {
		return "", c.listFailed(team, err)
	}
	if !c.Kit.HasTool("gum") {
		c.say("--teams " + team + " asks which seat to take, and gum is not installed: play-claude-yolo.yml installs it.")
		c.say("  Or name the seat yourself: --teams " + choices.Default + "@" + team)
		return "", exitConfig
	}
	picked, ok, code := c.pickSeat(ctx, h.AtStep(1, 2, "Seat").About("team "+team), team, choices)
	if !ok {
		return "", code
	}
	c.say("Taking seat " + picked.Seat + "@" + team + ": " + picked.Handle + " (" + picked.Words + ").")
	head := h.AtStep(2, 2, "Role").About("team " + team + " · seat " + picked.Seat + " · " + picked.Handle)
	if code := c.offerRole(ctx, head, team, picked, choices); code != term.ExitOK {
		return "", code
	}
	return picked.Seat + "@" + team, term.ExitOK
}

// Describe is `ccy-teams describe-seats [--prefix TEXT] LIST`: each seat of a launch's --teams
// list followed by its role, for Quick Launch to show, on stdout after the prefix. A role that
// cannot be read is said so with the reason; it never stops the launch showing what it would do.
func (c *Choose) Describe(ctx context.Context, args []string) term.ExitCode {
	if len(args) > 0 && args[0] == "--block" {
		return c.block(ctx, args[1:])
	}
	prefix := ""
	if len(args) > 0 && args[0] == "--prefix" {
		if len(args) < 2 {
			c.say("ccy-teams: 'describe-seats --prefix' takes the text. See ccy-teams --help.")
			return term.ExitUsage
		}
		prefix, args = args[1], args[2:]
	}
	if len(args) != 1 {
		c.say("ccy-teams: 'describe-seats' takes the list. See ccy-teams --help.")
		return term.ExitUsage
	}
	if args[0] == "" {
		c.Host.Print(c.Host.Stdout, prefix+"none\n")
		return term.ExitOK
	}
	d := &describer{c: c, rosters: map[string]rosterResult{}, about: c.descriptions(ctx)}
	var parts []string
	for ref := range strings.SplitSeq(args[0], ",") {
		parts = append(parts, d.describe(ctx, strings.TrimSpace(ref)))
	}
	c.Host.Print(c.Host.Stdout, prefix+strings.Join(parts, ", ")+"\n")
	return term.ExitOK
}

// block is `describe-seats --block LIST`: on stdout, the block a launch prints once its seats are
// taken, naming every team the session is on with its seat, the seat's role and what the team is
// for. LIST is the canonical SEAT@TEAM list; anything else is a usage mistake.
func (c *Choose) block(ctx context.Context, args []string) term.ExitCode {
	seats, ok := c.seatItems("--block", args)
	if !ok {
		return term.ExitUsage
	}
	width := 0
	for _, st := range seats {
		width = max(width, len(st.team))
	}
	d := &describer{c: c, rosters: map[string]rosterResult{}, about: c.descriptions(ctx)}
	noun := "teams"
	if len(seats) == 1 {
		noun = "team"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "This session is on %d %s:\n", len(seats), noun)
	for _, st := range seats {
		about := d.about[st.team]
		if about == "" {
			about = "(no description)"
		}
		fmt.Fprintf(&b, "  %-*s  seat %s (%s)  %s\n", width, st.team, st.seat, d.roleWords(ctx, st.seat, st.team), about)
	}
	// Security audit L1: a team human's request carries the owner's authority over the session.
	which := "these teams"
	if len(seats) == 1 {
		which = "this team"
	}
	fmt.Fprintf(&b, "Any human listed in %s can direct this session as you can.\n", which)
	c.Host.Print(c.Host.Stdout, b.String())
	return term.ExitOK
}

type seatTeam struct{ seat, team string }

// seatItems reads the one canonical SEAT@TEAM list a describe-seats mode takes; anything else is
// a usage mistake, said on stderr.
func (c *Choose) seatItems(mode string, args []string) ([]seatTeam, bool) {
	if len(args) != 1 || args[0] == "" {
		c.say("ccy-teams: 'describe-seats " + mode + "' takes the seat list. See ccy-teams --help.")
		return nil, false
	}
	var seats []seatTeam
	for item := range strings.SplitSeq(args[0], ",") {
		seat, team, ok := strings.Cut(strings.TrimSpace(item), "@")
		if !ok {
			c.say("ccy-teams: 'describe-seats " + mode + "' takes SEAT@TEAM items; '" + item + "' is not one. See ccy-teams --help.")
			return nil, false
		}
		seats = append(seats, seatTeam{seat, team})
	}
	return seats, true
}

// descriptions is what each declared team is for. Descriptions that cannot be read are said on
// stderr and left out: the launch still shows its seats.
func (c *Choose) descriptions(ctx context.Context) map[string]string {
	teams, err := c.Host.Declare.Declared(ctx)
	if err != nil {
		c.Kit.ReportErr(err)
		return nil
	}
	about := make(map[string]string, len(teams))
	for _, t := range teams {
		if t.Description != "" {
			about[t.Name] = t.Description
		}
	}
	return about
}

func (c *Choose) say(msg string) { c.Host.Print(c.Host.Stderr, msg+"\n") }

func (c *Choose) listFailed(team string, err error) term.ExitCode {
	cmd, ok := errors.AsType[*agentbus.CommandError](err)
	if !ok || cmd == nil {
		c.Kit.ReportErr(err)
		return exitConfig
	}
	code := term.ExitCode(cmd.Code)
	c.Host.Print(c.Host.Stderr, fmt.Sprintf("ccy-teams: --teams %s: the seats of team %s could not be listed "+
		"(agent-bus seat choose exited %d; the reason is above).\n", team, team, cmd.Code))
	return code
}

// pickSeat asks which of the choices; ok is false when the person left (no error: nothing is
// started) or the question could not be asked, with the code to exit with.
func (c *Choose) pickSeat(ctx context.Context, h ui.Heading, team string, choices agentbus.Choices) (agentbus.Choice, bool, term.ExitCode) {
	rows := make([]string, len(choices.Rows))
	var def string
	for i, r := range choices.Rows {
		rows[i] = row(r)
		if r.Seat == choices.Default {
			def = rows[i]
		}
	}
	answer, err := c.Gum.Choose(ctx, h, "Team "+team+": which seat does this session take?", def, rows)
	if errors.Is(err, ui.ErrCancelled) {
		c.say("No seat chosen; nothing started.")
		return agentbus.Choice{}, false, term.ExitOK
	}
	if err != nil {
		c.Kit.ReportErr(err)
		return agentbus.Choice{}, false, term.ExitFailure
	}
	for i, r := range rows {
		if r == answer {
			return choices.Rows[i], true, term.ExitOK
		}
	}
	c.say("ccy-teams: gum returned a row that was not offered: " + answer)
	return agentbus.Choice{}, false, exitConfig
}

// offerRole asks what the seat's role should be and changes it when the person says so. A seat
// that is new has no member yet, so it cannot be given a role until the launch has made it.
func (c *Choose) offerRole(ctx context.Context, h ui.Heading, team string, seat agentbus.Choice, choices agentbus.Choices) term.ExitCode {
	if seat.State == stateNew {
		c.say("Seat " + agentbus.Label(seat.Handle, "") + " is new: it joins as a worker. To make it the coordinator, " +
			"choose it on its row in ccy-teams, or in this chooser the next time.")
		return term.ExitOK
	}
	roster, err := c.Bus.List(ctx, team)
	if err != nil {
		c.Kit.ReportRootErr(err)
		c.say("ccy-teams: the members of " + team + " could not be read, so the seat's role cannot be offered.")
		return term.ExitFailure
	}
	member, ok := roster.Member(seat.Handle)
	if !ok {
		c.say("Seat " + agentbus.Label(seat.Handle, "") + " has no member in " + team + " yet: it joins as a worker.")
		return term.ExitOK
	}
	look := running(choices)
	holder := coordinator.Holding(roster, member.Handle, look)
	options, wantCoordinator := roleOptions(team, member, holder)
	answer, err := c.Gum.Choose(ctx, h, roleHeader(team, seat, member, holder), options[0], options)
	if errors.Is(err, ui.ErrCancelled) {
		c.say(noRoleAsked)
		return term.ExitOK
	}
	if err != nil {
		c.Kit.ReportErr(err)
		return term.ExitFailure
	}
	if answer == options[0] {
		return term.ExitOK
	}
	changed, err := c.Seats.ChangeRole(ctx, team, seat.Handle, wantCoordinator, look)
	if err != nil {
		c.Kit.Term.PrintError(err.Error())
		return term.ExitFailure
	}
	if !changed {
		c.say("Nothing was changed.")
	}
	return term.ExitOK
}

// running is what the chooser's seats of this checkout say of a member's session: a held seat
// runs, a free one does not, and a member with no seat here is unknown.
func running(choices agentbus.Choices) coordinator.Lookup {
	var seats []agentbus.Seat
	for _, r := range choices.Rows {
		if r.State != stateNew {
			seats = append(seats, agentbus.Seat{Seat: r.Seat, Handle: r.Handle, Held: r.State == "held"})
		}
	}
	return coordinator.FromSeats(seats)
}

// roleOptions are the choices for a member: keeping its role first (the default), then the
// change, and whether that change makes it the coordinator. A team with no coordinator offers
// the role plainly; a team that has one offers the takeover, worded as one.
func roleOptions(team string, member agentbus.Member, holder coordinator.Holder) ([]string, bool) {
	switch {
	case member.Role == agentbus.RoleCoordinator:
		return []string{keepRole(agentbus.RoleCoordinator), stopRole}, false
	case holder.Handle != "":
		return []string{keepRole(agentbus.RoleWorker), coordinator.TakeoverChoice(holder)}, true
	}
	return []string{keepRole(agentbus.RoleWorker), coordinator.NoneChoice(team)}, true
}

func roleHeader(team string, seat agentbus.Choice, member agentbus.Member, holder coordinator.Holder) string {
	who := team + " has no coordinator"
	switch {
	case member.Role == agentbus.RoleCoordinator:
		who = "it is " + team + "'s coordinator"
	case holder.Handle != "":
		who = team + "'s coordinator is " + holder.Named() + ", and a team has one"
	}
	return "Role of seat " + seat.Seat + ": what is its role? Now " + member.Role + "; " + who + "."
}

type rosterResult struct {
	roster agentbus.Roster
	err    error
}

// describer reads each team once and the checkout's seats once.
type describer struct {
	c       *Choose
	rosters map[string]rosterResult
	handles map[string]string // "<seat>@<team>" -> handle
	about   map[string]string // team -> what it is for
	loaded  bool
	loadErr error
}

// describe is the seat with its role, and what its team is for when the team has a description.
func (d *describer) describe(ctx context.Context, ref string) string {
	seat, team, ok := strings.Cut(ref, "@")
	if !ok {
		return ref
	}
	if about := d.about[team]; about != "" {
		return d.role(ctx, ref, seat, team) + ": " + about
	}
	return d.role(ctx, ref, seat, team)
}

func (d *describer) role(ctx context.Context, ref, seat, team string) string {
	return ref + " (" + d.roleWords(ctx, seat, team) + ")"
}

// roleWords is the seat's role, or why it cannot be said.
func (d *describer) roleWords(ctx context.Context, seat, team string) string {
	handle, found, err := d.handle(ctx, seat, team)
	if err != nil {
		return "role unknown: " + err.Error()
	}
	if !found {
		return newSeatWords
	}
	res, read := d.rosters[team]
	if !read {
		roster, rerr := d.c.Bus.List(ctx, team)
		res = rosterResult{roster, rerr}
		d.rosters[team] = res
	}
	if res.err != nil {
		return "role unknown: " + res.err.Error()
	}
	member, ok := res.roster.Member(handle)
	if !ok {
		return newSeatWords
	}
	return member.Role
}

const newSeatWords = "new seat: it joins as a worker"

// handle is the member's handle of a seat of this checkout; found is false for a seat it does
// not have.
func (d *describer) handle(ctx context.Context, seat, team string) (string, bool, error) {
	if !d.loaded {
		d.loaded = true
		d.handles = map[string]string{}
		list, _, err := d.c.Bus.SeatList(ctx, "")
		d.loadErr = err
		for _, s := range list {
			d.handles[s.Seat+"@"+s.Team] = s.Handle
		}
	}
	if d.loadErr != nil {
		return "", false, d.loadErr
	}
	handle, ok := d.handles[seat+"@"+team]
	return handle, ok, nil
}
