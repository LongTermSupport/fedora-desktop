package teams

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
		h := newPickerHarness(t)
		h.script.gum = []string{"NO"}
		h.script.fzf = []string{"|EXIT"}
		r := h.pass(t)
		if _, err := r.act(context.Background(), c.key, r.row(t, c.member)); err != nil {
			t.Fatalf("%q on %s: %v", c.key, c.member, err)
		}
		seen := h.errOut.String() + strings.Join(h.headerLines(), "\n")
		if !strings.Contains(seen, c.want) {
			t.Fatalf("%q on %s: want %q in %q", c.key, c.member, c.want, seen)
		}
	}
}

// headerLines is the headers of every fzf call so far.
func (h *pickerHarness) headerLines() []string {
	all := make([]string, 0, len(h.script.fzfArgs))
	for n := range h.script.fzfArgs {
		all = append(all, h.headerOf(n))
	}
	return all
}

func TestUpdateAllIsApplyAll(t *testing.T) {
	h := newPickerHarness(t)
	r := h.pass(t)
	if _, err := r.act(context.Background(), "ctrl-u", r.row(t, "-")); err != nil {
		t.Fatal(err)
	}
	if len(h.ops.calls) != 1 || h.ops.calls[0] != "apply --all" {
		t.Fatalf("ops %v", h.ops.calls)
	}
}
