package podfreeze

// The pure decisions of the bash suite (index_of, count_in_state, target_effect, row_verb,
// infer_action) live in the engine-neutral library and are ported once under both state
// vocabularies in internal/freeze. The bash suite pinned them against podfreeze's own fixture
// as well, so they are kept here as the same table under the podman words, run over the
// inventory the real Load builds.

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze"
)

func TestInventoryIndexOfAFoundNameOrNotThere(t *testing.T) {
	_, inv := fixture(t)
	for name, want := range map[string]int{"zulu_yolo": 0, "alpha_yolo_2": 2, "echo_yolo_3": 5} {
		if got, ok := inv.IndexOf(name); !ok || got != want {
			t.Errorf("%s: %d, %v", name, got, ok)
		}
	}
	// A prefix or suffix of a real name must not match: every CCY name here shares the _yolo
	// tail with three others, and substring matching is the quiet way a selection acts on a
	// container nobody chose.
	for _, name := range []string{"nosuch", "zulu", "yolo", ""} {
		if _, ok := inv.IndexOf(name); ok {
			t.Errorf("%q must not match", name)
		}
	}
}

func TestSelectAllAndCCYLandInInventoryOrder(t *testing.T) {
	_, inv := fixture(t)
	eqStrings(t, "all", inv.SelectAll(), []string{"zulu_yolo", "mike", "alpha_yolo_2", "kilo", "bravo_browser", "echo_yolo_3"})
	eqStrings(t, "ccy", ccyNames(inv), []string{"zulu_yolo", "alpha_yolo_2", "bravo_browser", "echo_yolo_3"})
	_, noCCY := load(t, "mike|running|none\n", "", "")
	if got := ccyNames(noCCY); len(got) != 0 {
		t.Fatalf("no CCY sessions gives an empty selection: %v", got)
	}
}

// count_in_state counts the NAMES GIVEN, not the inventory: the fixture holds 3 running and 3
// paused, and every case asks about a subset whose answer differs from both.
func TestCountInStateCountsTheNamesGiven(t *testing.T) {
	_, inv := fixture(t)
	cases := []struct {
		state string
		names []string
		want  int
	}{
		{"running", []string{"mike", "kilo", "zulu_yolo"}, 2},
		{"paused", []string{"mike", "kilo", "zulu_yolo"}, 1},
		{"paused", []string{"zulu_yolo"}, 1},
		{"running", []string{"zulu_yolo"}, 0},
		{"running", nil, 0},
		{"running", []string{"mike", "nosuch"}, 1}, // an unknown name contributes nothing
		{"exited", []string{"mike", "zulu_yolo"}, 0},
		// Selection de-duplicates (bug 4), not the count: a repeated name is counted twice here.
		{"running", []string{"mike", "mike"}, 2},
	}
	for _, tc := range cases {
		if got := inv.CountInState(tc.state, tc.names...); got != tc.want {
			t.Errorf("%s %v: %d, want %d", tc.state, tc.names, got, tc.want)
		}
	}
}

func TestNewARepeatedNameLabelsOneContainerOnce(t *testing.T) {
	_, inv := fixture(t)
	names, err := inv.SelectNames([]string{"mike", "mike"})
	if err != nil {
		t.Fatal(err)
	}
	if got := inv.TargetEffect(freeze.VerbDerived, names...); got != "FREEZE 1" {
		t.Fatalf("the label for one container: %q", got)
	}
}

func TestTargetEffectTheMenuRowCanNeverDisagreeWithTheOutcome(t *testing.T) {
	_, inv := fixture(t)
	cases := []struct {
		verb  freeze.Verb
		names []string
		want  string
	}{
		{freeze.VerbDerived, []string{"zulu_yolo", "mike", "alpha_yolo_2"}, "FREEZE 1"},
		{freeze.VerbDerived, []string{"zulu_yolo", "alpha_yolo_2"}, "THAW   2"},
		{freeze.VerbDerived, []string{"nosuch"}, "nothing to do"},
		{freeze.VerbDerived, nil, "nothing to do"},
		// The count is of what the verb will TOUCH, not the size of the selection.
		{freeze.VerbDerived, []string{"zulu_yolo", "alpha_yolo_2", "bravo_browser"}, "FREEZE 1"},
		{freeze.VerbDerived, []string{"zulu_yolo", "bravo_browser"}, "FREEZE 1"}, // mixed freezes
		{freeze.VerbFreeze, []string{"zulu_yolo", "mike"}, "FREEZE 1"},
		{freeze.VerbFreeze, []string{"zulu_yolo", "alpha_yolo_2"}, "nothing to freeze"},
		{freeze.VerbThaw, []string{"zulu_yolo", "alpha_yolo_2"}, "THAW   2"},
		{freeze.VerbThaw, []string{"mike", "kilo"}, "nothing to thaw"},
	}
	for _, tc := range cases {
		if got := inv.TargetEffect(tc.verb, tc.names...); got != tc.want {
			t.Errorf("%q %v: %q, want %q", tc.verb, tc.names, got, tc.want)
		}
	}
	// The three refusals are distinct, or a person who asked to thaw is told about freezing.
	refusals := map[string]bool{
		inv.TargetEffect(freeze.VerbFreeze, "zulu_yolo", "alpha_yolo_2"): true,
		inv.TargetEffect(freeze.VerbThaw, "mike", "kilo"):                true,
		inv.TargetEffect(freeze.VerbDerived, "nosuch"):                   true,
	}
	if len(refusals) != 3 {
		t.Fatalf("the three refusals must be distinct: %v", refusals)
	}
	// The two verbs share a column, so THAW carries padding FREEZE does not.
	if len(inv.TargetEffect(freeze.VerbDerived, "mike")) != len(inv.TargetEffect(freeze.VerbDerived, "zulu_yolo")) {
		t.Fatal("the two verb labels are the same width")
	}
}

func TestRowVerbIsWhatThisOneContainerIsAboutToHaveDoneToIt(t *testing.T) {
	_, inv := fixture(t)
	cases := []struct {
		verb freeze.Verb
		name string
		want string
	}{
		{freeze.VerbDerived, "mike", "FREEZE"},
		{freeze.VerbDerived, "zulu_yolo", "THAW"},
		{freeze.VerbFreeze, "mike", "FREEZE"},
		{freeze.VerbFreeze, "zulu_yolo", "-"},
		{freeze.VerbThaw, "zulu_yolo", "THAW"},
		{freeze.VerbThaw, "mike", "-"},
		// A name with no inventory entry is "?" whatever the verb; it must not read as "-"
		// ("nothing will happen to this one"), a claim about a container that is not there.
		{freeze.VerbDerived, "nosuch", "?"},
		{freeze.VerbFreeze, "nosuch", "?"},
		{freeze.VerbThaw, "nosuch", "?"},
	}
	for _, tc := range cases {
		if got := inv.RowVerb(tc.verb, tc.name); got != tc.want {
			t.Errorf("%q %s: %q, want %q", tc.verb, tc.name, got, tc.want)
		}
	}
}

func TestInferActionAnythingRunningGetsFrozen(t *testing.T) {
	_, inv := fixture(t)
	cases := []struct {
		selected []string
		want     freeze.Verb
	}{
		{[]string{"mike"}, freeze.VerbFreeze},
		{[]string{"zulu_yolo"}, freeze.VerbThaw},
		{[]string{"zulu_yolo", "alpha_yolo_2", "echo_yolo_3"}, freeze.VerbThaw},
		// THE case a first-element-only predicate fails: two paused names before the running one.
		{[]string{"zulu_yolo", "alpha_yolo_2", "mike"}, freeze.VerbFreeze},
		{[]string{"mike", "zulu_yolo"}, freeze.VerbFreeze},
		{nil, freeze.VerbThaw},
		{[]string{"nosuch"}, freeze.VerbThaw},
		{[]string{"nosuch", "mike"}, freeze.VerbFreeze},
	}
	for _, tc := range cases {
		if got := inv.InferAction(tc.selected); got != tc.want {
			t.Errorf("%v: %q, want %q", tc.selected, got, tc.want)
		}
	}
}
