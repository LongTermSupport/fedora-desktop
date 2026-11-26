package chooser

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

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
	// Every seat question below names a kind (the launch's memberType: podman for ccy, host for
	// cc) and is answered with that kind's seats only: a launch never sees, counts or takes
	// another launcher's seat (register R55).

	// SeatCount is how many pool seats of team of kind this checkout has, for the list. It names
	// no seat: a seat is chosen only from Pool, which says who holds each (issue 423).
	SeatCount func(team, kind string) (int, error)
	// Handles are the member handles of this checkout's pool seats of team of kind, in pool
	// order: what a host seat's Matrix consent names.
	Handles func(team, kind string) ([]string, error)
	// Teams are the teams with a pool seat of any kind in this checkout, sorted: each is a
	// teamchat team here, whether or not it has a seat of the launch's kind yet.
	Teams func() ([]string, error)
	// Answers is "" when team's bus answers this checkout's pool seats, else the words of why it
	// does not (system.Cmd.Answers, bounded); an error is a bus that cannot be judged. The list
	// offers a team only when it answers (issue 430).
	Answers func(ctx context.Context, team string) (string, error)
	// Pool is this checkout's pool seats of team of kind, each with the session a take would be
	// refused for: the only source a seat is chosen from, so a held seat is never taken (issue 423).
	Pool func(team, kind string) ([]seatchoice.Seat, error)
	// Reattach puts this terminal on the running session that holds a seat; an error says why it
	// cannot (open in another terminal, not running).
	Reattach func(ctx context.Context, session string) error
	// NewSeat is the "no free seat" outcome (and the new-seat row): it adds a seat of team of the
	// launch's kind to this checkout and is its SEAT@TEAM, or "" with the code to exit with (said
	// on stderr). The wiring is seatadd.Adder.NewSeat (register R5); nil says the way on,
	// `ccy-teams seats add <team>`, and ends the launch (78).
	NewSeat NewSeatFunc
}

// NewSeatFunc adds a new seat of team for a launcher of memberType and is its SEAT@TEAM, or ""
// with the code to exit with (said on stderr).
type NewSeatFunc func(ctx context.Context, team, memberType string) (string, term.ExitCode)

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
// taken; no seat at all goes to newSeat; otherwise every seat is listed, free and held, then a
// new seat, the lowest free one the default (the new seat when none is free), and a held one
// offered as the re-attach of its session, which starts nothing new. Leaving the question, or a
// re-attach made, is "" and ExitOK; a re-attach that cannot be made is refused as held (75).
func (c *Chooser) teamchatSeat(ctx context.Context, h ui.Heading, team, memberType string) (string, term.ExitCode) {
	pool, err := c.Teamchat.Pool(team, memberType)
	if err != nil {
		return "", c.failed(err)
	}
	seats := seatchoice.Order(pool)
	free, anyFree := seatchoice.Default(seats)
	switch {
	case len(seats) == 0:
		c.say("Team " + team + " is on teamchat, and this checkout has no " + memberType + " seat of it.")
		return c.newSeat(ctx, team, memberType, seats)
	case len(seats) == 1 && anyFree:
		c.say("Team " + team + " is on teamchat: seat " + free.Name + ", this checkout's one " + memberType + " seat of it.")
		return free.Name + "@" + team, term.ExitOK
	}
	rows := make([]string, len(seats), len(seats)+1)
	held := 0
	for i, s := range seats {
		rows[i] = seatchoice.Row(s)
		if s.Held() {
			held++
		}
	}
	rows = append(rows, seatchoice.NewRow(team, memberType))
	def := rows[len(rows)-1]
	if anyFree {
		def = seatchoice.Row(free)
	} else {
		c.say("Every seat of " + team + " in this checkout is held by a running session: re-attach one, or start a new seat.")
	}
	head := h.Push("Seat").About("team " + team + " · on teamchat · " + strconv.Itoa(len(seats)) +
		" seats in this checkout, " + strconv.Itoa(held) + " held")
	answer, err := c.Gum.Choose(ctx, head, "Which seat of "+team+"? A held seat re-attaches the session holding it.", def, rows)
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
	case i == len(seats):
		return c.newSeat(ctx, team, memberType, seats)
	case seats[i].Held():
		return "", c.reattach(ctx, team, memberType, seats[i], seats)
	}
	return seats[i].Name + "@" + team, term.ExitOK
}

// newSeat is the one place the "no free seat" outcome and the new-seat row go: Teamchat.NewSeat
// when wired, which adds a seat of the launch's kind and launches it (register R5); otherwise
// the seats and every way on, `ccy-teams seats add <team>` among them, are said and the launch
// ends (78), since no seat can be had here.
func (c *Chooser) newSeat(ctx context.Context, team, memberType string, seats []seatchoice.Seat) (string, term.ExitCode) {
	if c.Teamchat.NewSeat != nil {
		c.say("Adding a new " + memberType + " seat of " + team + " to this checkout (" + seatchoice.AddCommand(team, memberType) + ").")
		return c.Teamchat.NewSeat(ctx, team, memberType)
	}
	c.say("ccy-teams: a new " + memberType + " seat of " + team + " is added by " + seatchoice.AddCommand(team, memberType) + "; this launch does not add it.")
	if code := c.waysOn(ctx, team, memberType, seats); code != term.ExitOK {
		return "", code
	}
	return "", exitConfig
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

// waysOn says the seats of team of the launch's kind in this checkout, which are held, and the
// command for each way on, the launcher named as the person ran it (cc for a host launch).
func (c *Chooser) waysOn(ctx context.Context, team, memberType string, seats []seatchoice.Seat) term.ExitCode {
	here, err := c.Host.Checkouts.Find(ctx, "")
	if err != nil {
		return c.failed(err)
	}
	for _, line := range seatchoice.WaysOn(memberType, team, term.AbbrevHome(here.Top, c.Host.Home), seats) {
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
		pool, err := c.Teamchat.Pool(item, memberType)
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
// launch, which names both. A team whose bus does not answer is dead here (retired, its server
// gone, or its bus stopped): it is no row, so it cannot be chosen, and it is named above the list
// with why (issue 430).
func (c *Chooser) withTeamchat(ctx context.Context, memberType string, entries []entry) ([]entry, error) {
	tc := c.teamchatFor(memberType)
	if tc == nil {
		return entries, nil
	}
	if tc.Answers == nil {
		return nil, errors.New("ccy-teams: the teamchat teams cannot be listed: no probe of a team's bus is wired, and a team is never offered unjudged")
	}
	teams, err := tc.Teams()
	if err != nil {
		return nil, err
	}
	teams = slices.DeleteFunc(teams, func(team string) bool { return inTeams(entries, team) })
	dead, err := deadTeams(ctx, tc, teams)
	if err != nil {
		return nil, err
	}
	var v2 []entry
	for i, team := range teams {
		if dead[i] != "" {
			c.say("Not offered: team " + team + " is dead in this checkout, since its bus does not answer (" + dead[i] + ").")
			continue
		}
		seats, seatsErr := tc.SeatCount(team, memberType)
		if seatsErr != nil {
			return nil, seatsErr
		}
		v2 = append(v2, entry{
			Choice:   declare.Choice{Team: team, Description: teamchatAbout, Standing: declare.StandingCovers},
			teamchat: memberType, teamchatSeats: seats,
		})
	}
	return append(v2, entries...), nil
}

// deadTeams asks every team's bus at once (each probe is bounded by Answers itself, so the list
// waits for the slowest one, not their sum): why each does not answer, "" for one that does, in
// the order of teams. The first probe that cannot judge is the error.
func deadTeams(ctx context.Context, tc *Teamchat, teams []string) ([]string, error) {
	why := make([]string, len(teams))
	errs := make([]error, len(teams))
	var wg sync.WaitGroup
	for i, team := range teams {
		wg.Go(func() { why[i], errs[i] = tc.Answers(ctx, team) })
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("whether team %s's bus answers: %w", teams[i], err)
		}
	}
	return why, nil
}

// teamchatStanding is a teamchat team's standing column: its seats of the launch's kind.
func teamchatStanding(e entry) string {
	switch e.teamchatSeats {
	case 0:
		return "on teamchat, no " + e.teamchat + " seat here"
	case 1:
		return "on teamchat, 1 " + e.teamchat + " seat here"
	}
	return "on teamchat, " + strconv.Itoa(e.teamchatSeats) + " " + e.teamchat + " seats here"
}

// bothSystems is the refusal of teams taken on both systems (Plan 00209 OD-D2 (a)), or "".
func bothSystems(taken []entry) string {
	var v1, v2 []string
	for _, e := range taken {
		if e.teamchat != "" {
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
