package picker

import (
	"maps"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// teamReads is the part of a Spawned count that reads the teams: systemctl, and every Python
// process (declare.py, and the user's agent-bus, whose seat list was one).
func teamReads(spawned map[string]int) map[string]int {
	reads := map[string]int{}
	for key, n := range spawned {
		if key == "systemctl" || strings.HasPrefix(key, "declare.py ") || strings.HasPrefix(key, "agent-bus (user) ") {
			reads[key] = n
		}
	}
	return reads
}

// SPEC-view.md V42: an open with nothing to offer starts, for team reads, one systemctl and one
// Python (declare status): no Python seat list and no Python declare read; a redraw with nothing
// changed starts one systemctl and no Python. The open is the picker started on the populated
// world (alpha current, its description offer answered never, no limit, this folder no
// checkout), up to the list's first draw; the redraw is the pass after leaving alpha's menu.
func TestAnOpenWithNothingToOfferAndARedrawStartFewTeamReads(t *testing.T) {
	h := newRig(t)
	h.Spawned()
	var draws []map[string]int
	// Rewrite is called as each fzf answers: the moment a screen is drawn, so the processes
	// started up to it are those of the draw it shows.
	h.Script.Rewrite = func(out string) string {
		draws = append(draws, teamReads(h.Spawned()))
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
	if want := map[string]int{"systemctl": 1, "declare.py status": 1}; !maps.Equal(draws[0], want) {
		t.Errorf("the open started %v for team reads, want %v", draws[0], want)
	}
	if got, want := teamReads(h.Spawned()), map[string]int{"systemctl": 1}; !maps.Equal(got, want) {
		t.Errorf("the redraw started %v for team reads, want %v", got, want)
	}
}
