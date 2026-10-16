package picker

import (
	"context"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
)

func TestActRoutesKeyAndKindToTheFlowThatOwnsThem(t *testing.T) {
	cases := []struct {
		key, member, want string
	}{
		{"ctrl-n", "owner", "is not a project folder"},
		{"ctrl-e", board.NoDescription, "has no Element profile"},
		{"ctrl-p", "guest", "Sets a new password for guest"},
		// A key acts only on a row whose menu and footer show it (CLAUDE/InteractiveScripts.md).
		{"ctrl-r", "proj.box+role.lxc", nothingOnThisRow},
		{"ctrl-x", "owner", nothingOnThisRow},
		{"ctrl-p", "1", nothingOnThisRow},
		{"ctrl-u", "dev", nothingOnThisRow},
		{"", "owner", "What to do with owner in alpha"},
		{"?", "owner", "Each team is a bold row"},
		{"", board.NoDescription, "What to do with team alpha"},
		{"", "dev", "What to do with dev in alpha"},
		{keyAttach, "dev", "has no running session to attach"},
		{"", "proj.box+role.lxc", "What to do with proj.box+role.lxc in alpha"},
		{"ctrl-z", "1", nothingOnThisRow},
	}
	for _, c := range cases {
		h := newRig(t)
		h.Script.Gum = []string{"NO"}
		h.Script.Fzf = []string{"|EXIT"}
		r := h.run1(t)
		if _, err := r.act(context.Background(), c.key, h.row(t, r.snap, c.member)); err != nil {
			t.Fatalf("%q on %s: %v", c.key, c.member, err)
		}
		seen := h.ErrOut.String() + strings.Join(h.headerLines(), "\n")
		if !strings.Contains(seen, c.want) {
			t.Fatalf("%q on %s: want %q in %q", c.key, c.member, c.want, seen)
		}
		if c.want == nothingOnThisRow && h.plays() != 0 {
			t.Fatalf("%q on %s ran the apply play", c.key, c.member)
		}
	}
}

// headerLines is the headers of every fzf call so far.
func (h rig) headerLines() []string {
	out := make([]string, 0, len(h.Script.FzfArgs))
	for n := range h.Script.FzfArgs {
		out = append(out, h.HeaderOf(n))
	}
	return out
}

func TestUpdateAllIsApplyAll(t *testing.T) {
	h := newRig(t)
	h.outdatedUntilApplied("unstamped")
	r := h.run1(t)
	if _, err := r.act(context.Background(), "ctrl-u", h.row(t, r.snap, board.NoDescription)); err != nil {
		t.Fatal(err)
	}
	if h.plays() != 1 {
		t.Fatalf("the apply play ran %d times, want once for the team that was behind", h.plays())
	}
}
