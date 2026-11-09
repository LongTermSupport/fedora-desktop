package picker

import (
	"context"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
)

// The whole list is driven through ui: Enter on a row, then a line of its menu, so what a
// line does is what ui resolves it to on that row.
func TestAMenuLineRoutesToTheFlowThatOwnsIt(t *testing.T) {
	cases := []struct {
		line, member, want string
	}{
		{"Start a ccy session", "owner", "is not a project folder"},
		{"Open alpha in Element", board.NoDescription, "has no Element profile"},
		{"New password", "guest", "Sets a new password for guest"},
		{"", "owner", "What to do with owner in alpha"},
		{"What the columns", "owner", "Each team is a bold row"},
		{"", board.NoDescription, "What to do with team alpha"},
		{"", "dev", "What to do with proj.dev+local.podman in alpha"},
		{"Attach this terminal", "dev", "has no running session to attach"},
		{"", "proj.box+role.lxc", "What to do with proj.box+role.lxc in alpha"},
	}
	for _, c := range cases {
		h := newRig(t)
		h.Script.Gum = []string{"NO"}
		r := h.run1(t)
		row := h.text(t, r.snap, c.member)
		h.Script.Fzf = []string{"|" + row, "|EXIT"}
		if c.line != "" {
			h.Script.Fzf = []string{"|" + row, "|" + c.line, "|EXIT"}
		}
		if err := r.listPass(context.Background()); err != nil {
			t.Fatalf("%q on %s: %v", c.line, c.member, err)
		}
		seen := h.ErrOut.String() + strings.Join(h.headerLines(), "\n")
		if !strings.Contains(seen, c.want) {
			t.Fatalf("%q on %s: want %q in %q", c.line, c.member, c.want, seen)
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
	h.Script.Fzf = []string{"|" + h.text(t, r.snap, board.NoDescription), "|Update every team"}
	if err := r.listPass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.plays() != 1 {
		t.Fatalf("the apply play ran %d times, want once for the team that was behind", h.plays())
	}
}

// seatRestore is whether session ccy-proj, which holds seat 1, is started again after a reboot.
func (h rig) seatRestore(t *testing.T) bool {
	t.Helper()
	store, err := h.p.Seats.Registry()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := store.Get("ccy-proj")
	if err != nil {
		t.Fatal(err)
	}
	return rec.Restore
}

// Plan 00177 Task 10.6: turning a seat's restore off, from its menu line, asks first (default
// No); No and Esc leave it on, Yes turns it off.
func TestTurningRestoreOffAsksFirst(t *testing.T) {
	cases := []struct {
		answer string
		off    bool
	}{
		{"|EXIT", false},
		{"|ABORT", false},
		{"|YES", true},
	}
	for _, c := range cases {
		h := newRig(t)
		// wide enough that the heading's context line, which holds the test's temporary
		// path, stays on one line
		h.Script.Columns = 400
		r := h.run1(t)
		row := h.text(t, r.snap, "1")
		h.Script.Fzf = []string{"|" + row, "|Stop starting its session again after a reboot", c.answer}
		if err := r.listPass(context.Background()); err != nil {
			t.Fatalf("%s: %v", c.answer, err)
		}
		if got := !h.seatRestore(t); got != c.off {
			t.Fatalf("%s: restore off %v, want %v", c.answer, got, c.off)
		}
		if !strings.Contains(h.HeaderOf(2), "Turn restore off for seat 1@alpha? Its session, ccy-proj, will not come back after a reboot") {
			t.Fatalf("%s: header %q", c.answer, h.HeaderOf(2))
		}
		h.Script.HeadingAt(2, "Teams › alpha › proj.1+local.podman › Restore off\nseat 1@alpha · session ccy-proj · in "+h.Checkout())
	}
}

// Turning restore back on asks nothing: the menu, then the list again.
func TestTurningRestoreOnAsksNothing(t *testing.T) {
	h := newRig(t)
	r := h.run1(t)
	h.Script.Fzf = []string{"|" + h.text(t, r.snap, "1"), "|Stop starting its session again after a reboot", "|YES"}
	if err := r.listPass(context.Background()); err != nil {
		t.Fatal(err)
	}
	r = h.run1(t)
	h.Script.Fzf = []string{"|" + h.text(t, r.snap, "1"), "|Start its session again after a reboot"}
	before := len(h.Script.Headings)
	if err := r.listPass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !h.seatRestore(t) || len(h.Script.Headings)-before != 2 {
		t.Fatalf("restore %v, screens %q", h.seatRestore(t), h.Script.Headings[before:])
	}
}
