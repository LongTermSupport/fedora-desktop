package chooser

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// Words of the list.
const (
	noDescription = "(no description)"
	listHeader    = "Tab marks several, Enter takes them (or the highlighted one), Esc leaves"
	nothingChosen = "No team chosen; nothing started."
	notGitHub     = "not a GitHub checkout"
	createPrefix  = "+ Create a team"
)

// take shows the list, with several teams markable, and returns the teams the person took in
// list order, the held ones (marked in an earlier round) among them. again is true when the list
// should be shown once more, and the teams returned are then the marks to keep; otherwise an empty
// result ends the run with the code (leaving the list succeeds).
func (c *Chooser) take(ctx context.Context, repo string, entries []entry, first bool, held []entry) ([]entry, term.ExitCode, bool) {
	if first && len(entries) == 1 && entries[0].Standing == declare.StandingCovers && !entries[0].down {
		only := entries[0]
		c.say("Team " + only.Team + " works on " + repo + " (this checkout): " + describe(only.Choice) + ".")
		return entries, term.ExitOK, false
	}
	shown := slices.DeleteFunc(slices.Clone(entries), func(e entry) bool { return inTeams(held, e.Team) })
	rows := listRows(repo, shown)
	answer, err := c.Picker.Choose(ctx, listHeading(repo, held), ui.ChooseSpec{Prompt: "Team> ", Header: listHeader, Height: "50%", Multi: true, Rows: rows})
	switch {
	case errors.Is(err, ui.ErrLeave):
		c.say(nothingChosen)
		return nil, term.ExitOK, false
	case err != nil:
		c.Host.ReportErr(err)
		return nil, term.ExitFailure, false
	case len(answer) == 0:
		c.say("ccy-teams: the list returned no row.")
		return nil, term.ExitFailure, false
	}
	j, ok := c.judge(repo, shown, rows, answer, held)
	if !ok {
		return nil, term.ExitFailure, false
	}
	taken, blockedAny := j.taken, j.blockedAny
	slices.SortStableFunc(taken, func(a, b entry) int {
		return slices.IndexFunc(entries, func(e entry) bool { return e.Team == a.Team }) -
			slices.IndexFunc(entries, func(e entry) bool { return e.Team == b.Team })
	})
	if j.wantCreate {
		made, code, again := c.created(ctx, repo)
		if code != term.ExitOK {
			return nil, code, false
		}
		blockedAny = blockedAny || again
		taken = append(taken, made...)
	}
	if blockedAny {
		if len(taken) > 0 {
			c.say("Keeping " + teamNames(taken) + " marked; choose more, or Esc to leave.")
		}
		return taken, term.ExitOK, true
	}
	return taken, term.ExitOK, false
}

// listHeading is the team list's heading: the launch, the checkout it is for, and the teams
// already marked in an earlier round.
func listHeading(repo string, held []entry) ui.Heading {
	context := "for " + repo + " (this checkout)"
	if repo == "" {
		context = notGitHub
	}
	if len(held) > 0 {
		context += " · marked: " + teamNames(held)
	}
	return ui.Here(flow.LaunchCrumb, "Teams").About(context)
}

// judge reads the marked rows: the teams that can be taken (after the held ones), whether the
// create row was marked, and whether any mark was blocked (its reason is said). ok is false when
// fzf returned a row that was not offered.
func (c *Chooser) judge(repo string, shown []entry, rows, answer []string, held []entry) (judgement, bool) {
	j := judgement{taken: slices.Clone(held)}
	for _, line := range answer {
		at := slices.Index(rows, line)
		switch {
		case at < 0:
			c.say("ccy-teams: fzf returned a row that was not offered: " + line)
			return judgement{}, false
		case at == len(shown):
			j.wantCreate = true
		case blocked(repo, shown[at]) != "":
			c.say(blocked(repo, shown[at]))
			j.blockedAny = true
		default:
			j.taken = append(j.taken, shown[at])
		}
	}
	return j, true
}

// judgement is what the marked rows came to.
type judgement struct {
	taken      []entry
	wantCreate bool
	blockedAny bool
}

func inTeams(list []entry, team string) bool {
	return slices.ContainsFunc(list, func(e entry) bool { return e.Team == team })
}

func teamNames(list []entry) string {
	names := make([]string, len(list))
	for i, e := range list {
		names[i] = e.Team
	}
	return strings.Join(names, ", ")
}

// created runs the create wizard, then reads the checkout's standing again: the wizard lets the
// person choose the scope, so the new team is taken only if it covers this checkout. Cancelling
// the wizard goes back to the list.
func (c *Chooser) created(ctx context.Context, repo string) ([]entry, term.ExitCode, bool) {
	team, code := c.Create(ctx)
	switch {
	case code == term.ExitCancelled:
		c.say("Nothing was created; back to the list.")
		return nil, term.ExitOK, true
	case code != term.ExitOK:
		return nil, code, false
	case team == "":
		c.say("ccy-teams: the create wizard finished but named no team.")
		return nil, term.ExitFailure, false
	}
	if repo == "" {
		return []entry{{Choice: declare.Choice{Team: team}}}, term.ExitOK, false
	}
	entries, err := c.entries(ctx, repo)
	if err != nil {
		return nil, c.failed(err), false
	}
	at := slices.IndexFunc(entries, func(e entry) bool { return e.Team == team })
	if at < 0 || entries[at].Standing != declare.StandingCovers {
		c.say("ccy-teams: team " + team + " was created but does not cover " + repo + ", so nothing was started. " +
			"Run ccy --teams again to choose it and add the repository, or name it: ccy --teams " + team + ".")
		return nil, term.ExitFailure, false
	}
	return entries[at : at+1], term.ExitOK, false
}

// blocked is why the team cannot be taken, or "".
func blocked(repo string, e entry) string {
	switch {
	case e.down:
		return e.Team + "'s homeserver is down: ccy-teams apply " + e.Team + "."
	case e.Standing == declare.StandingExcluded:
		return e.Team + " excludes " + repo + " on purpose: edit its exclude in the host_vars to change that."
	case e.Standing == declare.StandingUnreadable:
		return e.Team + "'s scope cannot be read (it is vaulted), so it cannot be offered for " + repo +
			"; name it: ccy --teams " + e.Team + "."
	}
	return ""
}

// describe is what the team is for, or why that cannot be shown.
func describe(c declare.Choice) string {
	switch {
	case c.DescriptionState != "":
		return "(description " + string(c.DescriptionState) + ")"
	case c.Description != "":
		return c.Description
	}
	return noDescription
}

// standing is the standing column of a row.
func standing(repo string, e entry) string {
	var text string
	switch e.Standing {
	case declare.StandingCovers:
		text = "covers " + repo
	case declare.StandingWiden:
		text = "would add " + repo
	case declare.StandingExcluded:
		text = "excludes " + repo
	case declare.StandingUnreadable:
		text = "scope unreadable"
	default:
		text = notGitHub
	}
	if e.down {
		text += " (down)"
	}
	return text
}

// listRows are one row per team, then the row that creates one.
func listRows(repo string, entries []entry) []string {
	nameWidth, standWidth := 0, 0
	for _, e := range entries {
		nameWidth = max(nameWidth, len(e.Team))
		standWidth = max(standWidth, len(standing(repo, e)))
	}
	rows := make([]string, 0, len(entries)+1)
	for _, e := range entries {
		rows = append(rows, fmt.Sprintf("%-*s  %-*s  %s", nameWidth, e.Team, standWidth, standing(repo, e), describe(e.Choice)))
	}
	create := createPrefix
	if repo != "" {
		create += " for " + repo
	}
	return append(rows, strings.TrimRight(create, " "))
}
