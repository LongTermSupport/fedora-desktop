package chooser

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
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
	// Seats are the seat parts of this checkout's pool seats of team, in pool order.
	Seats func(team string) ([]string, error)
	// Teams are the teams with a pool seat in this checkout, sorted.
	Teams func() ([]string, error)
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

// teamchatSeat is the SEAT@TEAM of a teamchat team, from this checkout's pool: its one seat,
// or the one the person chooses of several. No seat here is refused (78); leaving the question
// is "" and ExitOK, said.
func (c *Chooser) teamchatSeat(ctx context.Context, h ui.Heading, team string) (string, term.ExitCode) {
	seats, err := c.Teamchat.Seats(team)
	if err != nil {
		return "", c.failed(err)
	}
	switch len(seats) {
	case 0:
		c.say("ccy-teams: Team " + team + " is on teamchat, and this checkout has no seat of it: " +
			"ask its coordinator for a seat issued to this checkout, then launch again.")
		return "", exitConfig
	case 1:
		c.say("Team " + team + " is on teamchat: seat " + seats[0] + ", this checkout's one seat of it.")
		return seats[0] + "@" + team, term.ExitOK
	}
	head := h.Push("Seat").About("team " + team + " · on teamchat · " + strconv.Itoa(len(seats)) + " seats in this checkout")
	answer, err := c.Gum.Choose(ctx, head, "Which seat of "+team+"?", seats[0], seats)
	switch {
	case errors.Is(err, ui.ErrCancelled):
		c.say(nothingChosen)
		return "", term.ExitOK
	case err != nil:
		return "", c.failed(err)
	case !slices.Contains(seats, answer):
		c.say("ccy-teams: gum returned an answer that was not offered: " + answer)
		return "", term.ExitFailure
	}
	return answer + "@" + team, term.ExitOK
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
		seats, seatsErr := tc.Seats(team)
		if seatsErr != nil {
			return nil, seatsErr
		}
		v2 = append(v2, entry{Choice: declare.Choice{Team: team, Description: teamchatAbout, Standing: declare.StandingCovers}, teamchatSeats: len(seats)})
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
