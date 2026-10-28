package picker

import (
	"context"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
)

// The whole list is driven through ui: a key on a row, or Enter on a row and a line of its
// menu, so what a key does is what ui resolves it to on that row.
func TestAKeyOrAMenuLineRoutesToTheFlowThatOwnsIt(t *testing.T) {
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
		{"", "dev", "What to do with proj.dev+local.podman in alpha"},
		{"attach", "dev", "has no running session to attach"},
		{"", "proj.box+role.lxc", "What to do with proj.box+role.lxc in alpha"},
		{"ctrl-z", "1", nothingOnThisRow},
	}
	for _, c := range cases {
		h := newRig(t)
		h.Script.Gum = []string{"NO"}
		r := h.run1(t)
		row := h.text(t, r.snap, c.member)
		switch c.key {
		case "attach":
			h.Script.Fzf = []string{"|" + row, "|Attach this terminal", "|EXIT"}
		case "":
			h.Script.Fzf = []string{"|" + row, "|EXIT"}
		default:
			h.Script.Fzf = []string{c.key + "|" + row, "|EXIT"}
		}
		if err := r.listPass(context.Background()); err != nil {
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
	h.Script.Fzf = []string{"ctrl-u|" + h.text(t, r.snap, board.NoDescription)}
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

// Plan 00177 Task 10.6: turning a seat's restore off, from its menu line or with Ctrl-R on
// its row, asks first (default No); No and Esc leave it on, Yes turns it off.
func TestTurningRestoreOffAsksFirstFromTheMenuAndFromCtrlR(t *testing.T) {
	cases := []struct {
		route, answer string
		off           bool
	}{
		{"menu", "|EXIT", false},
		{"menu", "|ABORT", false},
		{"menu", "|YES", true},
		{keyRestore, "|EXIT", false},
		{keyRestore, "|ABORT", false},
		{keyRestore, "|YES", true},
	}
	for _, c := range cases {
		h := newRig(t)
		// wide enough that the heading's context line, which holds the test's temporary
		// path, stays on one line
		h.Script.Columns = 400
		r := h.run1(t)
		row := h.text(t, r.snap, "1")
		confirmAt := 1
		if c.route == "menu" {
			h.Script.Fzf = []string{"|" + row, "|Stop starting its session again after a reboot", c.answer}
			confirmAt = 2
		} else {
			h.Script.Fzf = []string{keyRestore + "|" + row, c.answer}
		}
		if err := r.listPass(context.Background()); err != nil {
			t.Fatalf("%s %s: %v", c.route, c.answer, err)
		}
		if got := !h.seatRestore(t); got != c.off {
			t.Fatalf("%s %s: restore off %v, want %v", c.route, c.answer, got, c.off)
		}
		if !strings.Contains(h.HeaderOf(confirmAt), "Turn restore off for seat 1@alpha? Its session, ccy-proj, will not come back after a reboot") {
			t.Fatalf("%s %s: header %q", c.route, c.answer, h.HeaderOf(confirmAt))
		}
		h.Script.HeadingAt(confirmAt, "Teams › alpha › proj.1+local.podman › Restore off\nseat 1@alpha · session ccy-proj · in "+h.Checkout())
	}
}

// Turning restore back on asks nothing, from either route.
func TestTurningRestoreOnAsksNothing(t *testing.T) {
	for _, route := range []string{"menu", keyRestore} {
		h := newRig(t)
		r := h.run1(t)
		h.Script.Fzf = []string{keyRestore + "|" + h.text(t, r.snap, "1"), "|YES"}
		if err := r.listPass(context.Background()); err != nil {
			t.Fatal(err)
		}
		r = h.run1(t)
		row := h.text(t, r.snap, "1")
		h.Script.Fzf = []string{keyRestore + "|" + row}
		screens := 1
		if route == "menu" {
			h.Script.Fzf = []string{"|" + row, "|Start its session again after a reboot"}
			screens = 2
		}
		before := len(h.Script.Headings)
		if err := r.listPass(context.Background()); err != nil {
			t.Fatalf("%s: %v", route, err)
		}
		if !h.seatRestore(t) || len(h.Script.Headings)-before != screens {
			t.Fatalf("%s: restore %v, screens %q", route, h.seatRestore(t), h.Script.Headings[before:])
		}
	}
}
