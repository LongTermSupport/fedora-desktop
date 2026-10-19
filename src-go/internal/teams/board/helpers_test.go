package board

import (
	"context"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

// rig is the fake machine and a Collector reading it.
type rig struct {
	*teamtest.World

	c *Collector
}

func collectorOf(w *teamtest.World) *Collector { return New(w.Host) }

func newRig(t *testing.T) rig {
	t.Helper()
	w := teamtest.New(t)
	return rig{World: w, c: collectorOf(w)}
}

// collectMembers is one team's share of a pass: its roster and its people's sessions read, then
// noted in s.
func (c *Collector) collectMembers(ctx context.Context, s *Snapshot, team string) {
	p := newPool(c.Parallel)
	read := &rosterRead{}
	p.run(func() { c.readRoster(ctx, p, team, read) })
	p.wait()
	c.addMembers(s, team, read)
}

// populated is a machine with team alpha declared, built and up.
func populated(t *testing.T) rig {
	t.Helper()
	w := teamtest.Populated(t)
	return rig{World: w, c: collectorOf(w)}
}
