package choose

import (
	"context"
	"strings"
)

// Probe is what the launch summary asks of a remembered seat: whether a running session holds it
// now, and the role its member has ("coordinator" or "worker"; a seat the checkout does not have
// yet joins as a worker, and a role that cannot be read is "" with no error: the summary then
// has nothing to compare). An error is a team agent-bus could not list. It reads and changes
// nothing, and the summary names no bus system: this is the one place that does.
func (c *Choose) Probe(ctx context.Context, team, seat, memberType string) (bool, string, error) {
	choices, err := c.Bus.SeatChooseAs(ctx, team, memberType)
	if err != nil {
		return false, "", err
	}
	taken := false
	for _, r := range choices.Rows {
		if r.Seat == seat && r.State == stateHeld {
			taken = true
		}
	}
	d := &describer{c: c, rosters: map[string]rosterResult{}}
	words := d.roleWords(ctx, seat, team)
	switch {
	case strings.HasPrefix(words, "new seat"):
		return taken, "worker", nil
	case strings.HasPrefix(words, "role unknown"):
		return taken, "", nil
	}
	return taken, words, nil
}
