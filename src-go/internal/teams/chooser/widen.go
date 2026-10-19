package chooser

import (
	"context"
	"errors"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// scopeChoice are the three answers to "this team does not cover the checkout"; add is the default.
type scopeChoice struct{ add, join, leave string }

func scopeOptions(repo, team string) scopeChoice {
	return scopeChoice{
		add:   "Add " + repo + " to " + team + "'s scope",
		join:  "Join without adding (" + team + " will treat " + repo + " references as external)",
		leave: "Leave " + team + " out",
	}
}

// settle decides, for each taken team that does not cover the checkout, whether to add the
// repository to its scope, join without adding, or leave it out; applies every add in one play
// run; and returns the teams that go on to the seat chooser. again is true when every team was
// left out and the list should be shown once more; otherwise an empty result ends the run with
// the code.
func (c *Chooser) settle(ctx context.Context, repo string, taken []entry) ([]entry, term.ExitCode, bool) {
	if !hasWidening(taken) {
		return taken, term.ExitOK, false
	}
	if _, err := c.Host.LookPath("gum"); err != nil {
		c.say("ccy-teams: choosing what to do about " + repo + " needs gum, and it is not installed; " +
			"playbooks/imports/play-claude-yolo.yml installs it.")
		return nil, term.ExitFailure, false
	}
	c.say("Adding " + repo + " to a team's scope edits the host_vars and runs play-agent-bus.yml for it " +
		"(sudo may ask for your password). There is no homeserver restart, and every member of the team then trusts " + repo + ".")
	var kept []entry
	var adds []string
	for i, e := range taken {
		if e.Standing != declare.StandingWiden {
			kept = append(kept, e)
			continue
		}
		o := scopeOptions(repo, e.Team)
		head := flow.LaunchTeam(i+1, len(taken), e.Team).Push("Scope").About("team " + e.Team + " · does not cover " + repo + " (this checkout)")
		answer, err := c.Gum.Choose(ctx, head, e.Team+" does not trust "+repo+". Add "+repo+" (any branch) to it?", o.add, []string{o.add, o.join, o.leave})
		if errors.Is(err, ui.ErrCancelled) {
			c.say(nothingChosen)
			return nil, term.ExitOK, false
		}
		if err != nil {
			c.Host.ReportErr(err)
			return nil, term.ExitFailure, false
		}
		switch answer {
		case o.add:
			adds = append(adds, e.Team)
			kept = append(kept, e)
		case o.join:
			c.say(e.Team + ": " + repo + " references external to " + e.Team + " (its agents will see them as outside content).")
			kept = append(kept, e)
		case o.leave:
		default:
			c.say("ccy-teams: gum returned an answer that was not offered: " + answer)
			return nil, term.ExitFailure, false
		}
	}
	if code := c.widen(ctx, repo, adds); code != term.ExitOK {
		return nil, code, false
	}
	return kept, term.ExitOK, len(kept) == 0
}

func hasWidening(taken []entry) bool {
	for _, e := range taken {
		if e.Standing == declare.StandingWiden {
			return true
		}
	}
	return false
}

// widen adds the repository to each team's scope, then applies them all in one play run.
func (c *Chooser) widen(ctx context.Context, repo string, teams []string) term.ExitCode {
	if len(teams) == 0 {
		return term.ExitOK
	}
	for _, team := range teams {
		change, err := c.Declare.AddRepo(ctx, team, repo)
		if err != nil {
			return c.failed(err)
		}
		c.say(change.Text)
	}
	if err := c.Apply(ctx, teams); err != nil {
		names := strings.Join(teams, " ")
		c.say("ccy-teams: " + repo + " is now in the host_vars scope of " + names + " but it is not applied as declared, " +
			"so nothing was started. Once the cause above is fixed, ccy-teams apply " + names + " applies it.")
		return term.ExitFailure
	}
	return term.ExitOK
}

// seats chooses the seat of each team in turn and prints the list. Leaving any seat chooser
// starts nothing.
func (c *Chooser) seats(ctx context.Context, memberType string, taken []entry) term.ExitCode {
	refs := make([]string, 0, len(taken))
	for i, e := range taken {
		ref, code := c.Seat(ctx, flow.LaunchTeam(i+1, len(taken), e.Team), e.Team, memberType)
		if code != term.ExitOK {
			return code
		}
		if ref == "" {
			return term.ExitOK
		}
		refs = append(refs, ref)
	}
	return c.finish(ctx, memberType, refs)
}
