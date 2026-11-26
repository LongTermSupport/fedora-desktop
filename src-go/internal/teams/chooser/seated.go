package chooser

import (
	"context"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// flagSeated is plain cc's question (Plan 00265 R90, Q1): which team, if one, this checkout
// holds a host seat of, so a plain cc joins it (`cc --no-teams` opts out).
const flagSeated = "--seated"

// seated is `choose-teams --type=host --seated`: the payload is the one teamchat team whose bus
// answers and that this checkout holds a host seat of, said in words; nothing when there is
// none (quietly, since plain cc is then what it always was), or several (said, with how to join
// one). It asks nothing and makes nothing: the launch with that team does.
func (c *Chooser) seated(ctx context.Context, args []string) term.ExitCode {
	memberType, list, ok := c.parse(args) // --bus is an unknown option here
	switch {
	case !ok:
		return term.ExitUsage
	case memberType != typeHost || list != "":
		c.usage("'" + flagSeated + "' is plain cc's question: --type=host, with no list and no " + flagBus + ".")
		return term.ExitUsage
	}
	here, err := c.Host.Checkouts.Find(ctx, "")
	if err != nil {
		return c.failed(err)
	}
	if !here.Host() || c.Teamchat == nil || c.Teamchat.Answers == nil {
		return term.ExitOK
	}
	teams, err := c.Teamchat.Teams()
	if err != nil {
		return c.failed(err)
	}
	var held []string
	for _, team := range teams {
		n, countErr := c.Teamchat.SeatCount(team, memberType)
		if countErr != nil {
			return c.failed(countErr)
		}
		if n > 0 {
			held = append(held, team)
		}
	}
	dead, err := deadTeams(ctx, c.Teamchat, held)
	if err != nil {
		return c.failed(err)
	}
	var live []string
	for i, team := range held {
		if dead[i] != "" {
			c.say("Not joined: team " + team + ": " + dead[i] + ".")
			continue
		}
		live = append(live, team)
	}
	switch len(live) {
	case 0:
		return term.ExitOK
	case 1:
		c.say("This checkout holds a host seat of " + live[0] + ": cc joins it (cc --no-teams starts without it).")
		return c.printList(live)
	}
	c.say("This checkout holds host seats of " + strings.Join(live, " and ") +
		": cc joins none of them by itself. Run cc --teams <team> to join one.")
	return term.ExitOK
}
