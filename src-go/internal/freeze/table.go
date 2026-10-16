package freeze

import (
	"context"
	"fmt"
	"strings"
)

// Group is one row of the top-level menu: choosing it opens Members.
type Group struct {
	Key         string // what Engine.Select is later handed
	Description string
	Members     []string
}

// Engine is everything specific to one container engine. The library reaches an engine only
// through it.
type Engine[R any] interface {
	// Preflight checks the engine is usable on this host (its tools exist, root is available).
	Preflight(ctx context.Context) error
	// Load reads the engine's current state. It owns its columns and any warnings it prints.
	Load(ctx context.Context) (Inventory[R], error)
	// Groups are the menu rows for this inventory, in menu order.
	Groups(inv Inventory[R]) []Group
	// Select resolves a group key to the names in it. ErrGroupGone means the group vanished
	// since the menu was drawn (already explained); any other error is fatal.
	Select(ctx context.Context, inv Inventory[R], key string) ([]string, error)
	// Act freezes or thaws one container and returns the engine's own words, whether or not it
	// failed; a non-nil error is a failure of this container only.
	Act(ctx context.Context, verb Verb, name string) (string, error)
	// Columns is the engine's header for the columns after NAME and STATE.
	Columns() string
	// Row is the engine's columns for one entry.
	Row(e Entry[R]) string
}

// listFooterer is an optional Engine extension: text appended to the list report.
type listFooterer[R any] interface {
	ListFooter(inv Inventory[R]) string
}

const (
	tableFormat = "  %-34s %-8s %s\n"
	menuFormat  = "%-38s %s"
	memberRow   = "%-34s %-8s %s %s"
	allRowFmt   = "%-34s %s"
)

// Table is the NAME/STATE report with the engine's columns, one row per name that is in the
// inventory (an unknown name adds none). Engine columns are padded to the last one, so lines
// end in trailing spaces.
func (s *Session[R]) Table(names ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, tableFormat, "NAME", "STATE", s.Engine.Columns())
	for _, name := range names {
		if e, ok := s.inv.Entry(name); ok {
			fmt.Fprintf(&b, tableFormat, name, e.State, s.Engine.Row(e))
		}
	}
	return b.String()
}

// menu is the keys and labels of the group menu. A label says what choosing the row does right
// now, computed by the same function the action uses, so the two cannot disagree.
func (s *Session[R]) menu() ([]string, []string) {
	groups := s.Engine.Groups(s.inv)
	keys := make([]string, 0, len(groups))
	labels := make([]string, 0, len(groups))
	for _, g := range groups {
		keys = append(keys, g.Key)
		labels = append(labels, fmt.Sprintf(menuFormat, g.Description, s.inv.TargetEffect(s.Action, g.Members...)))
	}
	return keys, labels
}

// groupLine is what the menu calls the group key: its description ("" when it has gone).
func (s *Session[R]) groupLine(key string) string {
	for _, g := range s.Engine.Groups(s.inv) {
		if g.Key == key {
			return g.Description
		}
	}
	return ""
}

// screen is the drill-down for a group: the "all" row, then one line per member that still
// resolves. Names holds only the members actually shown, so a vanished one cannot shift the
// numbering.
func (s *Session[R]) screen(line string, group []string) MemberScreen {
	m := MemberScreen{Group: line, Lines: []string{fmt.Sprintf(allRowFmt, AllRowKey, s.inv.TargetEffect(s.Action, group...))}}
	for _, name := range group {
		e, ok := s.inv.Entry(name)
		if !ok {
			continue
		}
		m.Names = append(m.Names, name)
		m.Lines = append(m.Lines, fmt.Sprintf(memberRow, name, e.State, s.Engine.Row(e), s.inv.RowVerb(s.Action, name)))
	}
	return m
}
