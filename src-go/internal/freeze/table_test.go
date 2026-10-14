package freeze

import (
	"strings"
	"testing"
)

func TestTableUsesTheEnginesOwnColumns(t *testing.T) {
	h := newHarness(t, vocabs[0])
	table := h.s.Table("charlie", "alpha")
	mustContain(t, "the header names the shared columns", table, "NAME")
	mustContain(t, "and the engine's columns", table, "EXTRA")
	mustContain(t, "a row carries the container's name", table, "charlie")
	mustContain(t, "and its state", table, "running")
	mustContain(t, "and the engine's per-row values", table, "e-charlie")
	mustEq(t, "one header line plus one row per container", strings.Count(table, "\n"), 3)
	// A name that is not in the inventory adds no row: a blank row would look like a container.
	mustEq(t, "an unknown name adds no row", strings.Count(h.s.Table("nosuch"), "\n"), 1)
}

// Engine columns are padded including the last one, so lines end in trailing spaces, and the
// golden captures compare bytes.
func TestTableLayoutIsByteExactIncludingTrailingPadding(t *testing.T) {
	h := newHarness(t, vocabs[0])
	sp := func(n int) string { return strings.Repeat(" ", n) }
	want := "  NAME" + sp(30) + " STATE" + sp(3) + " MARK EXTRA\n" +
		"  charlie" + sp(27) + " running" + sp(1) + " m2" + sp(3) + "e-charlie\n"
	mustEq(t, "table", h.s.Table("charlie"), want)
}

// The menu rows, one shape for both tools.
func TestMenuRowsCarryKeyDescriptionAndWhatChoosingDoes(t *testing.T) {
	h := newHarness(t, vocabs[0])
	h.eng.groupsFn = func(inv Inventory[extraOf]) []Group {
		return []Group{
			{Key: "all", Description: "everything", Members: inv.Names()},
			{Key: "one", Description: "just the running one", Members: []string{"charlie"}},
			{Key: "none", Description: "an empty group"},
		}
	}
	keys, labels := h.s.menu()
	mustEq(t, "the keys are stored verbatim", keys, "[all one none]")
	mustEq(t, "keys and labels stay parallel", len(keys), len(labels))
	mustContain(t, "the label carries its description", labels[0], "everything")
	mustContain(t, "and what choosing it would do", labels[0], "FREEZE 2")
	// The effect is computed from the MEMBERS given, not from the inventory: a row that counted
	// the machine would promise to act outside its group.
	mustContain(t, "a narrower row counts only its own members", labels[1], "FREEZE 1")
	// The description column is padded to a fixed width so the effect column lines up.
	if len(labels[1]) <= 38 {
		t.Errorf("the description column is padded to a fixed width: label is %d chars: %q", len(labels[1]), labels[1])
	}
	mustEq(t, "exact label", labels[1], "just the running one"+strings.Repeat(" ", 19)+"FREEZE 1")
	// A group with no members is a legal row: "nothing to do" is an answer the user should see.
	mustContain(t, "an empty group still gets a row", labels[2], "nothing to do")
}

// A label describes what choosing the row will do under the explicit verb, so the menu reads
// the Session's Action.
func TestMenuLabelsFollowTheExplicitVerb(t *testing.T) {
	h := newHarness(t, vocabs[0])
	h.s.Action = VerbThaw
	_, labels := h.s.menu()
	mustContain(t, "thaw verb counts the frozen", labels[0], "THAW   3")
}
