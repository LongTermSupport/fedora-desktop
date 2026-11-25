package chooser

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seatchoice"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// Teamchat is this checkout's side of teamchat (v2, Plan 00209) for a podman or a host (cc)
// launch: a team named alone that is live on teamchat gets one of this checkout's pool seats,
// and the list shows the checkout's teamchat teams beside the agent-bus ones. A host seat on
// teamchat is a pool seat too (Task 5.1, gate B), and its Matrix consent is asked as on agent-bus.
type Teamchat struct {
	// Live is whether team is live on teamchat for this checkout (system.Cmd.Lines).
	Live func(ctx context.Context, team string) (bool, error)
	// SeatCount is how many pool seats of team this checkout has, for the list. It names no seat:
	// a seat is chosen only from Pool, which says who holds each (issue 423).
	SeatCount func(team string) (int, error)
	// Handles are the member handles of this checkout's pool seats of team, in pool order: what
	// a host seat's Matrix consent names.
	Handles func(team string) ([]string, error)
	// Teams are the teams with a pool seat in this checkout, sorted.
	Teams func() ([]string, error)
	// Pool is this checkout's pool seats of team, each with the session a take would be refused
	// for: the only source a seat is chosen from, so a held seat is never taken (issue 423).
	Pool func(team string) ([]seatchoice.Seat, error)
	// Reattach puts this terminal on the running session that holds a seat; an error says why it
	// cannot (open in another terminal, not running).
	Reattach func(ctx context.Context, session string) error
}

// teamchatAbout is a teamchat team's description in the list: its roster is not on this machine.
const teamchatAbout = "a teamchat team: its description is on the team's roster"

// teamchatFor is the teamchat side for memberType: nil for any type but podman and host, or
// when not wired.
func (c *Chooser) teamchatFor(memberType string) *Teamchat {
	if memberType != typePodman && memberType != typeHost {
		return nil
	}
	return c.Teamchat
}

// teamchatTeam is whether a team named alone is live on teamchat for this launch; a failure to
// decide is said and is the run's code.
func (c *Chooser) teamchatTeam(ctx context.Context, memberType, team string) (bool, term.ExitCode) {
	tc := c.teamchatFor(memberType)
	if tc == nil {
		return false, term.ExitOK
	}
	live, err := tc.Live(ctx, team)
	if err != nil {
		return false, c.failed(err)
	}
	return live, term.ExitOK
}

// teamchatSeat is the SEAT@TEAM of a teamchat team, from this checkout's pool, never a seat a
// running session holds (issue 423; v1's agent-bus seat choose is the floor). One seat, free, is
// taken; otherwise every seat is listed, free and held, the lowest free one the default, and a
// held one offered as the re-attach of its session, which starts nothing new. Every seat held
// says so with each way on before the list. No seat here is refused (78); leaving the question,
// or a re-attach made, is "" and ExitOK; a re-attach that cannot be made is refused as held (75).
func (c *Chooser) teamchatSeat(ctx context.Context, h ui.Heading, team, memberType string) (string, term.ExitCode) {
	pool, err := c.Teamchat.Pool(team)
	if err != nil {
		return "", c.failed(err)
	}
	seats := seatchoice.Order(pool)
	def, anyFree := seatchoice.Default(seats)
	switch {
	case len(seats) == 0:
		c.say("ccy-teams: Team " + team + " is on teamchat, and this checkout has no seat of it: " +
			"ask its coordinator for a seat issued to this checkout, then launch again.")
		return "", exitConfig
	case len(seats) == 1 && anyFree:
		c.say("Team " + team + " is on teamchat: seat " + def.Name + ", this checkout's one seat of it.")
		return def.Name + "@" + team, term.ExitOK
	case !anyFree:
		def = seats[0]
		c.say("ccy-teams: every seat of " + team + " in this checkout is held by a running session, so none can be taken.")
		if code := c.waysOn(ctx, team, memberType, seats); code != term.ExitOK {
			return "", code
		}
	}
	rows := make([]string, len(seats))
	held := 0
	for i, s := range seats {
		rows[i] = seatchoice.Row(s)
		if s.Held() {
			held++
		}
	}
	head := h.Push("Seat").About("team " + team + " · on teamchat · " + strconv.Itoa(len(seats)) +
		" seats in this checkout, " + strconv.Itoa(held) + " held")
	answer, err := c.Gum.Choose(ctx, head, "Which seat of "+team+"? A held seat re-attaches the session holding it.", seatchoice.Row(def), rows)
	i := slices.Index(rows, answer)
	switch {
	case errors.Is(err, ui.ErrCancelled):
		c.say(nothingChosen)
		return "", term.ExitOK
	case err != nil:
		return "", c.failed(err)
	case i < 0:
		c.say("ccy-teams: gum returned an answer that was not offered: " + answer)
		return "", term.ExitFailure
	case seats[i].Held():
		return "", c.reattach(ctx, team, memberType, seats[i], seats)
	}
	return seats[i].Name + "@" + team, term.ExitOK
}

// reattach puts this terminal on the session holding s instead of starting one; when it cannot,
// the refusal is held (75) and names each way on.
func (c *Chooser) reattach(ctx context.Context, team, memberType string, s seatchoice.Seat, seats []seatchoice.Seat) term.ExitCode {
	c.say("Seat " + s.Name + "@" + team + " is held by session " + s.HeldBy + ": re-attaching it; nothing new is started.")
	err := c.Teamchat.Reattach(ctx, s.HeldBy)
	if err == nil {
		return term.ExitOK
	}
	c.say("ccy-teams: session " + s.HeldBy + " could not be re-attached here: " + err.Error())
	if code := c.waysOn(ctx, team, memberType, seats); code != term.ExitOK {
		return code
	}
	return term.ExitBusy
}

// waysOn says the seats of team in this checkout, which are held, and the command for each way
// on, the launcher named as the person ran it (cc for a host launch).
func (c *Chooser) waysOn(ctx context.Context, team, memberType string, seats []seatchoice.Seat) term.ExitCode {
	here, err := c.Host.Checkouts.Find(ctx, "")
	if err != nil {
		return c.failed(err)
	}
	launcher := "ccy"
	if memberType == typeHost {
		launcher = "cc"
	}
	for _, line := range seatchoice.WaysOn(launcher, team, term.AbbrevHome(here.Top, c.Host.Home), seats) {
		c.say(line)
	}
	return term.ExitOK
}

// unattendedWays is the refusal of a list with a team named alone when nobody can be asked: each
// teamchat team alone gets its seats and the command for each way on; nothing is prompted.
func (c *Chooser) unattendedWays(ctx context.Context, memberType, list string) term.ExitCode {
	for item := range strings.SplitSeq(list, ",") {
		item = strings.TrimSpace(item)
		if item == "" || strings.Contains(item, "@") {
			continue
		}
		v2, code := c.teamchatTeam(ctx, memberType, item)
		if code != term.ExitOK {
			return code
		}
		if !v2 {
			continue
		}
		pool, err := c.Teamchat.Pool(item)
		if err != nil {
			return c.failed(err)
		}
		if code := c.waysOn(ctx, item, memberType, pool); code != term.ExitOK {
			return code
		}
	}
	return term.ExitUsage
}

// withTeamchat puts this checkout's teamchat teams first in the list (they cover it: their
// seats were issued to it), leaving out a name the list already has; that one is refused by the
// launch, which names both.
func (c *Chooser) withTeamchat(memberType string, entries []entry) ([]entry, error) {
	tc := c.teamchatFor(memberType)
	if tc == nil {
		return entries, nil
	}
	teams, err := tc.Teams()
	if err != nil {
		return nil, err
	}
	var v2 []entry
	for _, team := range teams {
		if inTeams(entries, team) {
			continue
		}
		seats, seatsErr := tc.SeatCount(team)
		if seatsErr != nil {
			return nil, seatsErr
		}
		v2 = append(v2, entry{Choice: declare.Choice{Team: team, Description: teamchatAbout, Standing: declare.StandingCovers}, teamchatSeats: seats})
	}
	return append(v2, entries...), nil
}

// teamchatStanding is a teamchat team's standing column.
func teamchatStanding(e entry) string {
	if e.teamchatSeats == 1 {
		return "on teamchat, 1 seat here"
	}
	return "on teamchat, " + strconv.Itoa(e.teamchatSeats) + " seats here"
}

// bothSystems is the refusal of teams taken on both systems (Plan 00209 OD-D2 (a)), or "".
func bothSystems(taken []entry) string {
	var v1, v2 []string
	for _, e := range taken {
		if e.teamchatSeats > 0 {
			v2 = append(v2, e.Team)
		} else {
			v1 = append(v1, e.Team)
		}
	}
	if len(v1) == 0 || len(v2) == 0 {
		return ""
	}
	return "ccy-teams: these teams are on both systems (agent-bus: " + strings.Join(v1, ", ") + "; teamchat: " +
		strings.Join(v2, ", ") + "), and one session joins one system. Run two sessions, one per system."
}
