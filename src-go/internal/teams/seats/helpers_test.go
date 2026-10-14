package seats

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/ops"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

// rig is the fake machine, on a terminal with scripted fzf and gum, and the Seats flows over it.
type rig struct {
	*teamtest.World
	*teamtest.Console

	b *board.Collector
	s *Seats
}

func newRig(t *testing.T) rig {
	t.Helper()
	w := teamtest.Populated(t)
	c := teamtest.Interactive(t, w)
	b := board.New(w.Host)
	o := ops.New(w.Host, b.Statuses, func() { b.ForgetDevices("") })
	return rig{World: w, Console: c, b: b, s: New(w.Host, flow.New(w.Host), o)}
}

// pass is one collection of the machine.
func (h rig) pass(t *testing.T) *board.Snapshot {
	t.Helper()
	s, err := h.b.Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// row is the row of member in the pass.
func (h rig) row(t *testing.T, snap *board.Snapshot, member string) board.Row {
	t.Helper()
	for _, row := range snap.Rows {
		if row.Member == member {
			return row
		}
	}
	t.Fatalf("no row for %s", member)
	return board.Row{}
}

// inProject makes the working directory a work tree whose top is the checkout.
func (h rig) inProject() {
	h.On("git rev-parse --is-inside-work-tree", "true\n", 0)
	h.On("git rev-parse --show-toplevel", h.Checkout()+"\n", 0)
}
