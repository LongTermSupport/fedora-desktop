package picker

import (
	"context"
	"strings"
	"testing"
)

func TestActRoutesKeyAndKindToTheFlowThatOwnsThem(t *testing.T) {
	cases := []struct {
		key, member, want string
	}{
		{"ctrl-n", "owner", "is not a project folder"},
		{"ctrl-r", "proj.box+role.lxc", "this row has no session"},
		{"ctrl-x", "owner", "A team's people are"},
		{"ctrl-e", "-", "has no Element profile"},
		{"ctrl-p", "1", "Ctrl-P sets a person's Element password"},
		{"", "owner", "Sets a new password for owner"},
		{"?", "owner", "Each team is a bold row"},
		{"", "-", "Each team is a bold row"},
		{"", "dev", "has no running session to attach"},
		{"", "proj.box+role.lxc", "Enter on proj.box+role.lxc does nothing"},
		{"ctrl-z", "1", "Enter on 1 does nothing"},
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
	if _, err := r.act(context.Background(), "ctrl-u", h.row(t, r.snap, "-")); err != nil {
		t.Fatal(err)
	}
	if h.plays() != 1 {
		t.Fatalf("the apply play ran %d times, want once for the team that was behind", h.plays())
	}
}
