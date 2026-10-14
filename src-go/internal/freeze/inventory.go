package freeze

import (
	"fmt"
	"strings"
)

// Entry is one container the engine reported as running or frozen. Record is the engine's own
// per-container record (its columns), typed by the engine and handed back to Engine.Row.
type Entry[R any] struct {
	Name   string
	State  string
	Record R
}

// Inventory is what the engine reported on one refresh, with the pure decisions over it. It
// remembers the state words it was built with so the decisions never compare against a literal.
type Inventory[R any] struct {
	cfg     Config
	entries []Entry[R]
	index   map[string]int
}

// NewInventory indexes the entries. A name reported twice resolves to its first entry.
func NewInventory[R any](cfg Config, entries []Entry[R]) Inventory[R] {
	inv := Inventory[R]{cfg: cfg, entries: append([]Entry[R](nil), entries...), index: make(map[string]int, len(entries))}
	for i, e := range inv.entries {
		if _, seen := inv.index[e.Name]; !seen {
			inv.index[e.Name] = i
		}
	}
	return inv
}

// Len is the number of entries.
func (inv Inventory[R]) Len() int { return len(inv.entries) }

// Entries is a copy of the entries, in the engine's order.
func (inv Inventory[R]) Entries() []Entry[R] { return append([]Entry[R](nil), inv.entries...) }

// Names is every name, in the engine's order.
func (inv Inventory[R]) Names() []string {
	names := make([]string, len(inv.entries))
	for i, e := range inv.entries {
		names[i] = e.Name
	}
	return names
}

// IndexOf is an exact match: a prefix, a suffix or the empty string never matches.
func (inv Inventory[R]) IndexOf(name string) (int, bool) {
	i, ok := inv.index[name]
	return i, ok
}

// Entry looks a name up.
func (inv Inventory[R]) Entry(name string) (Entry[R], bool) {
	i, ok := inv.index[name]
	if !ok {
		return Entry[R]{}, false
	}
	return inv.entries[i], true
}

// CountInState counts how many of the names given are in that state. An unknown name counts
// nothing and a name given twice counts twice, so selection is what de-duplicates.
func (inv Inventory[R]) CountInState(state string, names ...string) int {
	n := 0
	for _, name := range names {
		if e, ok := inv.Entry(name); ok && e.State == state {
			n++
		}
	}
	return n
}

// InferAction is freeze if any selected name is running, else thaw (an empty or wholly
// vanished selection included).
func (inv Inventory[R]) InferAction(selected []string) Verb {
	if inv.CountInState(inv.cfg.StateRunning, selected...) > 0 {
		return VerbFreeze
	}
	return VerbThaw
}

// TargetEffect says what acting on these names would do. The count is of what the verb will
// touch, not of the group, and the three refusals are three different strings.
func (inv Inventory[R]) TargetEffect(action Verb, names ...string) string {
	running := inv.CountInState(inv.cfg.StateRunning, names...)
	frozen := inv.CountInState(inv.cfg.StateFrozen, names...)
	switch action {
	case VerbFreeze:
		if running > 0 {
			return fmt.Sprintf("FREEZE %d", running)
		}
		return "nothing to freeze"
	case VerbThaw:
		if frozen > 0 {
			return fmt.Sprintf("THAW   %d", frozen)
		}
		return "nothing to thaw"
	case VerbDerived:
	}
	switch {
	case running > 0:
		return fmt.Sprintf("FREEZE %d", running)
	case frozen > 0:
		return fmt.Sprintf("THAW   %d", frozen)
	}
	return "nothing to do"
}

// RowVerb is what this one container is about to have done to it: "?" for a name not in the
// inventory (whatever the verb), "-" for nothing.
func (inv Inventory[R]) RowVerb(action Verb, name string) string {
	e, ok := inv.Entry(name)
	if !ok {
		return "?"
	}
	running := e.State == inv.cfg.StateRunning
	switch action {
	case VerbFreeze:
		if running {
			return "FREEZE"
		}
		return "-"
	case VerbThaw:
		if e.State == inv.cfg.StateFrozen {
			return "THAW"
		}
		return "-"
	case VerbDerived:
	}
	if running {
		return "FREEZE"
	}
	return "THAW"
}

// Buckets is a selection sorted by what a verb can do to each name.
type Buckets struct {
	Act      []string // in the wanted state: the verb applies
	Skipped  []string // present, but not in the wanted state
	Vanished []string // not in the inventory any more
}

// Partition sorts names, in the order given, for a freeze (wants running) or a thaw (wants
// frozen).
func (inv Inventory[R]) Partition(verb Verb, names []string) Buckets {
	want := inv.cfg.StateFrozen
	if verb == VerbFreeze {
		want = inv.cfg.StateRunning
	}
	var b Buckets
	for _, name := range names {
		switch e, ok := inv.Entry(name); {
		case !ok:
			b.Vanished = append(b.Vanished, name)
		case e.State == want:
			b.Act = append(b.Act, name)
		default:
			b.Skipped = append(b.Skipped, name)
		}
	}
	return b
}

// SelectAll is every name in inventory order.
func (inv Inventory[R]) SelectAll() []string { return inv.Names() }

// SelectNames keeps the order given and drops repeats (the first occurrence wins). One unknown
// name selects nothing and is fatal, even when the others exist: acting on fewer containers than
// were asked for, silently, is the failure.
func (inv Inventory[R]) SelectNames(names []string) ([]string, error) {
	var selected, unknown []string
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if _, ok := inv.IndexOf(name); !ok {
			if !seen[name] {
				unknown = append(unknown, name)
			}
		} else if !seen[name] {
			selected = append(selected, name)
		}
		seen[name] = true
	}
	if len(unknown) == 0 {
		return selected, nil
	}
	msg := fmt.Sprintf("not a running or frozen container: %s\n  Run '%s list' to see what is available.",
		strings.Join(unknown, " "), inv.cfg.Tool)
	if inv.cfg.ListNote != "" {
		msg += "\n" + inv.cfg.ListNote
	}
	return nil, Die("%s", msg)
}
