package board

import (
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

// populated is a machine with team alpha declared, built and up.
func populated(t *testing.T) rig {
	t.Helper()
	w := teamtest.Populated(t)
	return rig{World: w, c: collectorOf(w)}
}
