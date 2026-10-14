package freeze

import (
	"slices"
	"strings"
	"testing"
)

func TestIndexOf(t *testing.T) {
	for _, v := range vocabs {
		inv := fixtureInventory(v)
		for name, want := range map[string]int{"alpha": 0, "charlie": 2, "echo": 4} {
			t.Run(v.name+": found "+name, func(t *testing.T) {
				got, ok := inv.IndexOf(name)
				if !ok || got != want {
					t.Fatalf("got %d, %v", got, ok)
				}
			})
		}
		// A prefix or a suffix of a real name must not match it. Substring matching is the quiet
		// way a selection acts on a container nobody chose.
		for _, absent := range []string{"nosuch", "alph", "harlie", ""} {
			t.Run(v.name+": absent "+absent, func(t *testing.T) {
				if _, ok := inv.IndexOf(absent); ok {
					t.Fatalf("%q must not match", absent)
				}
			})
		}
	}
}

// count is of the NAMES GIVEN, not the inventory: the whole fixture holds 2 running and 3
// frozen, so each subset below differs from both.
func TestCountInState(t *testing.T) {
	for _, v := range vocabs {
		inv := fixtureInventory(v)
		t.Run(v.name+": two frozen out of three named", func(t *testing.T) {
			mustEq(t, "count", inv.CountInState(v.frozen, "alpha", "bravo", "charlie"), 2)
		})
		t.Run(v.name+": one running out of three named", func(t *testing.T) {
			mustEq(t, "count", inv.CountInState(v.running, "alpha", "bravo", "charlie"), 1)
		})
		t.Run(v.name+": no names at all is zero", func(t *testing.T) {
			mustEq(t, "count", inv.CountInState(v.running), 0)
		})
		t.Run(v.name+": an unknown name does not count", func(t *testing.T) {
			mustEq(t, "count", inv.CountInState(v.running, "charlie", "nosuch"), 1)
		})
		t.Run(v.name+": a state nothing is in", func(t *testing.T) {
			mustEq(t, "count", inv.CountInState("STOPPED", "alpha", "charlie"), 0)
		})
		// Pinned by the bash suite: the count is of arguments, so selection is what de-duplicates.
		t.Run(v.name+": duplicates count twice", func(t *testing.T) {
			mustEq(t, "count", inv.CountInState(v.running, "charlie", "charlie"), 2)
		})
	}
}

func TestInferAction(t *testing.T) {
	cases := []struct {
		what     string
		selected []string
		want     Verb
	}{
		{"one running container", []string{"charlie"}, VerbFreeze},
		{"one frozen container", []string{"alpha"}, VerbThaw},
		{"a wholly frozen set", []string{"alpha", "bravo", "delta"}, VerbThaw},
		// THE case a first-element-only predicate fails: two frozen names before the running one.
		{"a running container behind two frozen", []string{"alpha", "bravo", "charlie"}, VerbFreeze},
		{"an empty selection", nil, VerbThaw},
		{"a vanished name infers thaw", []string{"nosuch"}, VerbThaw},
	}
	for _, v := range vocabs {
		inv := fixtureInventory(v)
		for _, c := range cases {
			t.Run(v.name+": "+c.what, func(t *testing.T) {
				mustEq(t, "action", inv.InferAction(c.selected), c.want)
			})
		}
	}
}

func TestTargetEffect(t *testing.T) {
	cases := []struct {
		what   string
		action Verb
		names  []string
		want   string
	}{
		{"derived, running present", VerbDerived, []string{"alpha", "bravo", "charlie"}, "FREEZE 1"},
		{"derived, only frozen", VerbDerived, []string{"alpha", "bravo"}, "THAW   2"},
		{"derived, neither", VerbDerived, []string{"nosuch"}, "nothing to do"},
		{"derived, no names at all", VerbDerived, nil, "nothing to do"},
		// The count is of what the verb will TOUCH, not the size of the group.
		{"the label counts the acted-on, not the selected", VerbDerived, []string{"alpha", "bravo", "delta", "charlie"}, "FREEZE 1"},
		{"explicit freeze, running present", VerbFreeze, []string{"alpha", "charlie"}, "FREEZE 1"},
		{"explicit freeze, nothing running", VerbFreeze, []string{"alpha", "bravo"}, "nothing to freeze"},
		{"explicit thaw, frozen present", VerbThaw, []string{"alpha", "bravo"}, "THAW   2"},
		{"explicit thaw, nothing frozen", VerbThaw, []string{"charlie", "echo"}, "nothing to thaw"},
	}
	for _, v := range vocabs {
		inv := fixtureInventory(v)
		for _, c := range cases {
			t.Run(v.name+": "+c.what, func(t *testing.T) {
				mustEq(t, "effect", inv.TargetEffect(c.action, c.names...), c.want)
			})
		}
		// Distinct refusals: one shared "nothing to do" would tell a user who asked to thaw about
		// freezing, and would let a single string satisfy three cases.
		t.Run(v.name+": the three refusals are distinct", func(t *testing.T) {
			f := inv.TargetEffect(VerbFreeze, "alpha", "bravo")
			th := inv.TargetEffect(VerbThaw, "charlie", "echo")
			d := inv.TargetEffect(VerbDerived, "nosuch")
			if f == th || f == d || th == d {
				t.Fatalf("freeze %q thaw %q derived %q", f, th, d)
			}
		})
		// THAW carries padding FREEZE does not; a label that lost it would shift the count
		// column on thaw rows only.
		t.Run(v.name+": FREEZE n and THAW n are the same width", func(t *testing.T) {
			mustEq(t, "width", len(inv.TargetEffect(VerbDerived, "charlie")), len(inv.TargetEffect(VerbDerived, "alpha")))
		})
	}
}

func TestRowVerb(t *testing.T) {
	cases := []struct {
		what   string
		action Verb
		name   string
		want   string
	}{
		{"derived, a running container", VerbDerived, "charlie", "FREEZE"},
		{"derived, a frozen container", VerbDerived, "alpha", "THAW"},
		{"explicit freeze, running", VerbFreeze, "charlie", "FREEZE"},
		{"explicit freeze, frozen", VerbFreeze, "alpha", "-"},
		{"explicit thaw, frozen", VerbThaw, "alpha", "THAW"},
		{"explicit thaw, running", VerbThaw, "charlie", "-"},
		// '?' whatever the verb: '-' would claim "nothing will happen to this one" about a
		// container that is not there.
		{"derived, an unknown name", VerbDerived, "nosuch", "?"},
		{"explicit freeze, an unknown name", VerbFreeze, "nosuch", "?"},
		{"explicit thaw, an unknown name", VerbThaw, "nosuch", "?"},
	}
	for _, v := range vocabs {
		inv := fixtureInventory(v)
		for _, c := range cases {
			t.Run(v.name+": "+c.what, func(t *testing.T) {
				mustEq(t, "verb", inv.RowVerb(c.action, c.name), c.want)
			})
		}
	}
}

func TestPartition(t *testing.T) {
	for _, v := range vocabs {
		inv := fixtureInventory(v)
		// A selection holding all three kinds at once, so no case can pass by collapsing two
		// buckets into one.
		p := inv.Partition(VerbFreeze, []string{"charlie", "alpha", "nosuch"})
		t.Run(v.name+": freeze, the running one is a target", func(t *testing.T) { mustEq(t, "act", p.Act, "[charlie]") })
		t.Run(v.name+": freeze, the frozen one is skipped", func(t *testing.T) { mustEq(t, "skipped", p.Skipped, "[alpha]") })
		t.Run(v.name+": freeze, the absent one has vanished", func(t *testing.T) { mustEq(t, "vanished", p.Vanished, "[nosuch]") })
		p = inv.Partition(VerbThaw, []string{"charlie", "alpha", "nosuch"})
		t.Run(v.name+": thaw, the frozen one is a target", func(t *testing.T) { mustEq(t, "act", p.Act, "[alpha]") })
		t.Run(v.name+": thaw, the running one is skipped", func(t *testing.T) { mustEq(t, "skipped", p.Skipped, "[charlie]") })
		t.Run(v.name+": thaw, the absent one has vanished", func(t *testing.T) { mustEq(t, "vanished", p.Vanished, "[nosuch]") })
		// A name not in the inventory must be REPORTED, not dropped, and not folded into skipped,
		// which a user reads as "already in that state".
		p = inv.Partition(VerbFreeze, []string{"nosuch"})
		t.Run(v.name+": an all-absent selection has no targets", func(t *testing.T) { mustEq(t, "act", len(p.Act), 0) })
		t.Run(v.name+": and is not reported as skipped", func(t *testing.T) { mustEq(t, "skipped", len(p.Skipped), 0) })
		t.Run(v.name+": it is reported as vanished", func(t *testing.T) { mustEq(t, "vanished", p.Vanished, "[nosuch]") })
		// Order is the order given: echo is index 4 and charlie index 2.
		p = inv.Partition(VerbFreeze, []string{"echo", "charlie"})
		t.Run(v.name+": targets keep the order they were given", func(t *testing.T) { mustEq(t, "act", p.Act, "[echo charlie]") })
		// Partitioning twice must not accumulate: a stale bucket is how the loop would act on
		// something chosen a screen ago.
		first := inv.Partition(VerbFreeze, []string{"charlie"})
		second := inv.Partition(VerbFreeze, []string{"echo"})
		t.Run(v.name+": a second partition replaces the first", func(t *testing.T) {
			mustEq(t, "first", first.Act, "[charlie]")
			mustEq(t, "second", second.Act, "[echo]")
		})
	}
}

func TestSelectAllAndNames(t *testing.T) {
	for _, v := range vocabs {
		inv := fixtureInventory(v)
		t.Run(v.name+": every container, in inventory order", func(t *testing.T) {
			mustEq(t, "all", inv.SelectAll(), "[alpha bravo charlie delta echo]")
		})
		t.Run(v.name+": an empty inventory gives an empty selection", func(t *testing.T) {
			empty := NewInventory[extraOf](testConfig(v), nil)
			mustEq(t, "all", len(empty.SelectAll()), 0)
		})
		// echo is index 4 and charlie index 2: a selector that walked the inventory would swap
		// them, and the drill-down numbers these rows.
		t.Run(v.name+": names keep the order they were given", func(t *testing.T) {
			got, err := inv.SelectNames([]string{"echo", "charlie"})
			if err != nil {
				t.Fatal(err)
			}
			mustEq(t, "names", got, "[echo charlie]")
		})
	}
}

func TestSelectNamesUnknownNameIsFatalAndSaysWhatToRun(t *testing.T) {
	inv := fixtureInventory(vocabs[0])
	_, err := inv.SelectNames([]string{"nosuch"})
	msg := dieMsg(err)
	if msg == "" {
		t.Fatalf("an unknown name is fatal: %v", err)
	}
	mustContain(t, "it names the container it could not find", msg, "nosuch")
	mustContain(t, "and points at the command that lists them", msg, "testfreeze list")
	mustEq(t, "first line", strings.SplitN(msg, "\n", 2)[0], "not a running or frozen container: nosuch")

	// Fatal even when every OTHER name is real: selecting the subset that happened to exist
	// would act on fewer containers than were asked for and say nothing about it.
	_, err = inv.SelectNames([]string{"charlie", "nosuch"})
	msg = dieMsg(err)
	if msg == "" {
		t.Fatal("one unknown name among known ones is still fatal")
	}
	mustContain(t, "and names only the one it could not find", msg, "nosuch")
	mustLack(t, "not the ones it could", msg, "charlie")

	_, err = inv.SelectNames([]string{"nosuch", "other", "nosuch"})
	mustContain(t, "every unknown name, space-joined, once each", dieMsg(err), "not a running or frozen container: nosuch other\n")
}

// ListNote is the engine's extra sentence on that error. Absent by default, so podfreeze's
// message stays exactly the two lines it has always been.
func TestSelectNamesAppendsTheEnginesListNoteOnlyWhenSet(t *testing.T) {
	inv := fixtureInventory(vocabs[0])
	_, err := inv.SelectNames([]string{"nosuch"})
	msg := dieMsg(err)
	mustLack(t, "the note is absent when the engine set none", msg, "STOPPED")
	mustEq(t, "two lines only", strings.Count(msg, "\n"), 1)

	cfg := testConfig(vocabs[0])
	cfg.ListNote = "  A STOPPED container is not listed."
	inv = NewInventory(cfg, fixtureEntries(vocabs[0]))
	_, err = inv.SelectNames([]string{"nosuch"})
	mustContain(t, "and is printed when the engine set one", dieMsg(err), "A STOPPED container is not listed.")
}

// Bug 4: the bash acted twice on a name given twice, so the second pause failed and the label
// said FREEZE 2 for one container. The first occurrence wins.
func TestSelectNamesDeduplicatesFirstOccurrenceWins(t *testing.T) {
	inv := fixtureInventory(vocabs[0])
	got, err := inv.SelectNames([]string{"echo", "charlie", "echo", "charlie"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"echo", "charlie"}) {
		t.Fatalf("got %v", got)
	}
}

func TestNewInventoryKeepsTheFirstOfADuplicatedName(t *testing.T) {
	inv := NewInventory(testConfig(vocabs[0]), []Entry[extraOf]{
		{Name: "a", State: "running"}, {Name: "a", State: "paused"},
	})
	i, ok := inv.IndexOf("a")
	if !ok || i != 0 {
		t.Fatalf("index %d %v", i, ok)
	}
	e, ok := inv.Entry("a")
	if !ok || e.State != "running" {
		t.Fatalf("entry %+v", e)
	}
	if _, ok := inv.Entry("b"); ok {
		t.Fatal("b is absent")
	}
	mustEq(t, "len", inv.Len(), 2)
	mustEq(t, "entries", len(inv.Entries()), 2)
}
