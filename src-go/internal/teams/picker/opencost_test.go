package picker

import (
	"maps"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// SPEC-view.md V42: an open with nothing to offer starts, for team reads, one systemctl and one
// Python (declare status): no Python seat list and no Python declare read; a redraw with nothing
// changed starts one systemctl and no Python. The open is the picker started on the populated
// world (alpha current, its description offer answered never, no limit, this folder no
// checkout), up to the list's first draw; the redraw is the pass after leaving alpha's menu.
// Both are pinned as every process they start, by program and subcommand, so a process that
// comes back (a Python read by any route) fails it.
func TestAnOpenWithNothingToOfferAndARedrawStartFewTeamReads(t *testing.T) {
	h := newRig(t)
	h.Spawned()
	var draws []map[string]int
	// Rewrite is called as each fzf answers: the moment a screen is drawn, so the processes
	// started up to it are those of the draw it shows.
	h.Script.Rewrite = func(out string) string {
		draws = append(draws, h.Spawned())
		return out
	}
	h.Script.Fzf = []string{"|alpha", "|ABORT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	// The list, alpha's menu (left: back to the list) and the list again (left: the picker ends).
	// Only the first is answered with a row, so only it reaches Rewrite.
	if len(h.Script.Rows) != 3 || len(draws) != 1 {
		t.Fatalf("%d screens, %d answered with a row; want the list, alpha's menu and the list again", len(h.Script.Rows), len(draws))
	}
	// Beside the team reads, the open reads this folder's work tree (git), the sessions (tmux),
	// the room's members (agent-bus list) and who is signed in (one devices read per person).
	if want := map[string]int{
		"systemctl": 1, "declare.py status": 1, "git": 3, "tmux": 1, "agent-bus list": 1, "agent-bus human devices": 3,
	}; !maps.Equal(draws[0], want) {
		t.Errorf("the open started %v, want %v", draws[0], want)
	}
	// The redraw keeps the work tree and who is signed in; the units, sessions and members are read again.
	if got, want := h.Spawned(), map[string]int{"systemctl": 1, "tmux": 1, "agent-bus list": 1}; !maps.Equal(got, want) {
		t.Errorf("the redraw started %v, want %v", got, want)
	}
}
