package picker

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The row of the populated world whose menu these tests open.
const menuRow = "proj.box+role.lxc"

// startBindings is the fzf arguments that bind the start event: where the cursor starts.
func startBindings(args []string) []string {
	return slices.DeleteFunc(slices.Clone(args), func(a string) bool { return !strings.HasPrefix(a, "--bind=start:") })
}

// The host agent's finding (Plan 00177): in a row's menu, Esc then Down within about 50 ms, or
// both in one tmux send-keys, left the menu open and moved its cursor; the person, back on what
// they took for the list, acted on the menu. fzf reads the pair as alt-down. Esc then an arrow is
// Esc, then the arrow on the list: the menu closes, and the list comes back with the cursor moved
// from the row the menu was opened on. A plain Esc brings the list back on that row.
func TestEscThenAnArrowInARowMenuClosesItAndMovesTheListsCursor(t *testing.T) {
	cases := []struct {
		answer string
		// move is where the cursor starts on the list again, from the menu's row.
		move int
	}{
		{"alt-down|Back to the list", 1},
		{"alt-up|Back to the list", -1},
		{"|ABORT", 0},
	}
	for _, c := range cases {
		h := newRig(t)
		h.Script.Fzf = []string{"|" + menuRow, c.answer, "|ABORT"}
		if code := h.run(); code != term.ExitOK {
			t.Fatalf("%s: exit %d: %s", c.answer, code, h.ErrOut.String())
		}
		if len(h.Script.Rows) != 3 || !strings.Contains(h.HeaderOf(1), "What to do with "+menuRow) {
			t.Fatalf("%s: want the list, the row's menu and the list again; drawn %d", c.answer, len(h.Script.Rows))
		}
		at := slices.IndexFunc(h.Script.Rows[0], func(r string) bool { return strings.Contains(r, menuRow) })
		if at < 1 {
			t.Fatalf("the row is at %d: the up case needs a row above it", at)
		}
		want := "--bind=start:pos(" + strconv.Itoa(at+c.move+1) + ")"
		if !slices.Contains(h.Script.FzfArgs[2], want) {
			t.Errorf("%s: the list comes back with %s; its start bindings %q", c.answer, want, startBindings(h.Script.FzfArgs[2]))
		}
		if got := h.Script.FzfArgs[1]; !slices.ContainsFunc(got, func(a string) bool { return strings.HasPrefix(a, "--expect=") && strings.Contains(a, "alt-down") }) {
			t.Errorf("the menu hands Esc then Down back to the list: %q", got)
		}
	}
}
