package chooser

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// then is what the run does with a team named alone once known has asked about it.
type then int

const (
	// take is the team known returned: its seat is asked next.
	take then = iota
	// leaveOut is the team left out of the list; the rest are started.
	leaveOut
	// leaveRun is the run left: nothing is started, and that is not a failure.
	leaveRun
)

// The answers to the question about a team this machine does not have.
const (
	pickOther = "Choose one of this machine's teams instead"
	cancelRun = "Cancel: start nothing"
	startOn   = "Start on "
)

// known is the team the list names alone, as the run can take it; items is the list as it
// stands (what is settled so far, this team, then the rest as given). A team whose homeserver is
// up, or that the host_vars declare, is taken as named (agent-bus says what is wrong with a
// declared one that is down), and the host_vars are not read for a team that is up. A name the
// team file would refuse is a usage mistake, said with its reason. Any other name is asked
// about, maxRounds times at most: make it with the create wizard, choose a team this machine
// has whose homeserver is up in its place, start the others without it, or start nothing. A
// team made here is taken as named teams always are, without the bare list's question whether
// it covers this checkout.
func (c *Chooser) known(ctx context.Context, h ui.Heading, team string, items []string) (string, then, term.ExitCode) {
	state, err := c.Systemd.HomeserverState(ctx, team)
	if err != nil {
		return "", leaveRun, c.failed(err)
	}
	if state == systemd.HomeserverUp {
		return team, take, term.ExitOK
	}
	declared, err := c.Declare.Declared(ctx)
	if err != nil {
		return "", leaveRun, c.failed(err)
	}
	if slices.ContainsFunc(declared, func(t declare.Team) bool { return t.Name == team }) {
		return team, take, term.ExitOK
	}
	if err = c.Declare.Check(ctx, declare.Entry{Team: team}); err != nil {
		return "", leaveRun, c.failed(err)
	}
	pickable, err := c.upOnly(ctx, others(declared, items))
	if err != nil {
		return "", leaveRun, c.failed(err)
	}
	c.say("This machine has no team called " + team + ".")
	return c.askUnknown(ctx, h, team, items, pickable)
}

// upOnly are the teams whose homeserver is up: one that is not would only fail in another's
// place.
func (c *Chooser) upOnly(ctx context.Context, teams []string) ([]string, error) {
	var up []string
	for _, team := range teams {
		state, err := c.Systemd.HomeserverState(ctx, team)
		if err != nil {
			return nil, err
		}
		if state == systemd.HomeserverUp {
			up = append(up, team)
		}
	}
	return up, nil
}

// askUnknown asks what to do about team, which this machine does not have, maxRounds times at
// most; pickable are the teams it may be swapped for.
func (c *Chooser) askUnknown(ctx context.Context, h ui.Heading, team string, items, pickable []string) (string, then, term.ExitCode) {
	options := unknownOptions(team, items, pickable)
	for range maxRounds {
		answer, err := c.Gum.Choose(ctx, h.Push("Not on this machine"), "What now?", options[0], options)
		if errors.Is(err, ui.ErrCancelled) {
			answer, err = cancelRun, nil
		}
		if err != nil {
			return "", leaveRun, c.failed(err)
		}
		chosen, next, code, again := c.answerUnknown(ctx, h, team, answer, pickable)
		if !again {
			return chosen, next, code
		}
	}
	c.say("ccy-teams: nothing was settled for " + team + " in " + strconv.Itoa(maxRounds) + " tries; nothing started.")
	return "", leaveRun, term.ExitFailure
}

// answerUnknown carries out the answer about team; again is the question to be asked again,
// as the person left the wizard or the choice of another team.
func (c *Chooser) answerUnknown(ctx context.Context, h ui.Heading, team, answer string, pickable []string) (string, then, term.ExitCode, bool) {
	switch {
	case answer == cancelRun:
		c.say(nothingChosen)
		return "", leaveRun, term.ExitOK, false
	case answer == pickOther:
		chosen, err := c.Gum.Choose(ctx, h.Push("Choose a team instead"), "Team", "", pickable)
		if errors.Is(err, ui.ErrCancelled) {
			return "", take, term.ExitOK, true
		}
		if err != nil {
			return "", leaveRun, c.failed(err), false
		}
		return chosen, take, term.ExitOK, false
	case strings.HasPrefix(answer, startOn):
		c.say("Leaving " + team + " out.")
		return "", leaveOut, term.ExitOK, false
	}
	made, code := c.Create(ctx, team)
	switch {
	case code == term.ExitCancelled || code == term.ExitOK && made == "":
		c.say("Nothing was created; back to the choices.")
		return "", take, term.ExitOK, true
	case code != term.ExitOK:
		return "", leaveRun, code, false
	}
	return made, take, term.ExitOK, false
}

// unknownOptions are the answers to the question about team, at its place in items: choosing
// another team only when this machine has one the list does not name, and starting without it
// only when the list names something else.
func unknownOptions(team string, items, pickable []string) []string {
	options := []string{"Create team " + team + " now"}
	if len(pickable) > 0 {
		options = append(options, pickOther)
	}
	if rest := slices.DeleteFunc(slices.Clone(items), func(item string) bool { return item == team }); len(rest) > 0 {
		options = append(options, startOn+strings.Join(rest, ", ")+" without "+team)
	}
	return append(options, cancelRun)
}

// others are the teams present on this machine that no item of the list names.
func others(declared []declare.Team, items []string) []string {
	var names []string
	for _, t := range declared {
		named := slices.ContainsFunc(items, func(item string) bool {
			_, itemTeam, found := strings.Cut(item, "@")
			if !found {
				itemTeam = item
			}
			return itemTeam == t.Name
		})
		if t.Present && !named {
			names = append(names, t.Name)
		}
	}
	return names
}
