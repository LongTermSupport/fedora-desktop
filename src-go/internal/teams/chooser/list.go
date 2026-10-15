package chooser

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// Words of the list.
const (
	noDescription = "(no description)"
	listHeader    = "Enter takes the highlighted team, Esc leaves"
	nothingChosen = "No team chosen; nothing started."
	notGitHub     = "not a GitHub checkout"
	createPrefix  = "+ Create a team"
)

// take shows the list and returns the team the person chose. again is true when nothing was
// taken and the list should be shown once more; otherwise an empty result ends the run with
// the code (leaving the list succeeds).
func (c *Chooser) take(ctx context.Context, repo string, entries []entry, first bool) ([]entry, term.ExitCode, bool) {
	if first && len(entries) == 1 && entries[0].Standing == declare.StandingCovers && !entries[0].down {
		only := entries[0]
		c.say("Team " + only.Team + " works on " + repo + " (this checkout): " + describe(only.Choice) + ".")
		return entries, term.ExitOK, false
	}
	rows := listRows(repo, entries)
	answer, err := c.Picker.Choose(ctx, ui.ChooseSpec{Prompt: "Team> ", Header: listHeader, Height: "50%", Rows: rows})
	switch {
	case errors.Is(err, ui.ErrLeave):
		c.say(nothingChosen)
		return nil, term.ExitOK, false
	case err != nil:
		c.Host.ReportErr(err)
		return nil, term.ExitFailure, false
	case len(answer) != 1:
		c.say(fmt.Sprintf("ccy-teams: the list returned %d rows for one choice.", len(answer)))
		return nil, term.ExitFailure, false
	}
	at := slices.Index(rows, answer[0])
	if at < 0 {
		c.say("ccy-teams: fzf returned a row that was not offered: " + answer[0])
		return nil, term.ExitFailure, false
	}
	if at == len(entries) {
		return c.created(ctx, repo)
	}
	picked := entries[at]
	if reason := blocked(repo, picked); reason != "" {
		c.say(reason)
		return nil, term.ExitOK, true
	}
	return []entry{picked}, term.ExitOK, false
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
