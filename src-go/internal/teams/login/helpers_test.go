package login

import (
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/ops"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

// rig is the fake machine, on a terminal with scripted fzf and gum, and the Login flows over it.
type rig struct {
	*teamtest.World
	*teamtest.Console

	b *board.Collector
	o *ops.Ops
	l *Login
}

func newRig(t *testing.T) rig {
	t.Helper()
	w := teamtest.Populated(t)
	c := teamtest.Interactive(t, w)
	b := board.New(w.Host)
	o := ops.New(w.Host, b.Statuses, func() { b.ForgetDevices("") })
	return rig{World: w, Console: c, b: b, o: o, l: New(w.Host, flow.New(w.Host), b, o)}
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

func (h rig) passwordable() {
	h.On("sudo -n -v", "", 0)
	h.On(teamtest.SudoBus+"human password alpha owner", "a-new-password\nignored\n", 0)
}

func (h rig) elementLauncher() string {
	return filepath.Join(h.l.Home, ".local/share/applications/agent-bus-alpha-element.desktop")
}

// elementPlays is how many times the Element play was started.
func (h rig) elementPlays() int {
	n := 0
	for _, c := range h.Fake.Calls() {
		if c.Name == h.Host.ElementPlay {
			n++
		}
	}
	return n
}
