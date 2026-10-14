package lxcfreeze

import (
	"context"
	"slices"
	"sort"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze/cli"
)

const (
	keyAll    = "all"
	keyBridge = "bridge:"
)

// bridgeNames are the bridges in use, sorted bytewise: the two non-bridge labels are not bridges.
func bridgeNames(inv freeze.Inventory[record]) []string {
	var bridges []string
	for _, entry := range inv.Entries() {
		switch bridge := entry.Record.bridge; bridge {
		case bridgeNone, bridgeUnreadable:
		default:
			bridges = append(bridges, bridge)
		}
	}
	sort.Strings(bridges)
	return slices.Compact(bridges)
}

// membersOn are the names on a bridge, in inventory order.
func membersOn(inv freeze.Inventory[record], bridge string) []string {
	var members []string
	for _, entry := range inv.Entries() {
		if entry.Record.bridge == bridge {
			members = append(members, entry.Name)
		}
	}
	return members
}

// Groups are the menu rows in order: everything, then each bridge in use. There are no
// per-container rows; the drill-down is that screen.
func (e *Engine) Groups(inv freeze.Inventory[record]) []freeze.Group {
	bridges := bridgeNames(inv)
	groups := make([]freeze.Group, 0, 1+len(bridges))
	groups = append(groups, freeze.Group{Key: keyAll, Description: "all LXC containers", Members: inv.Names()})
	for _, bridge := range bridges {
		groups = append(groups, freeze.Group{Key: keyBridge + bridge, Description: "bridge: " + bridge, Members: membersOn(inv, bridge)})
	}
	return groups
}

// Select resolves a menu key to its members. A bridge that emptied is said on stderr and returned
// as freeze.ErrGroupGone, so the menu re-prompts instead of ending.
func (e *Engine) Select(_ context.Context, inv freeze.Inventory[record], key string) ([]string, error) {
	var names []string
	var err error
	switch bridge, isBridge := strings.CutPrefix(key, keyBridge); {
	case key == keyAll:
		names = inv.SelectAll()
	case isBridge:
		names, err = selectBridge(inv, bridge)
	default:
		err = freeze.Die("internal error: unknown target key '%s'.", key)
	}
	return names, freeze.SelectGone(e.Err, tool, err)
}

// SelectTarget resolves the one-shot command line to a selection: a mode (--all, --bridge, or
// cli.ModeNames), its value, and the names given. A vanished bridge is fatal here, with the same
// words the menu path says.
func (e *Engine) SelectTarget(_ context.Context, inv freeze.Inventory[record], mode, value string, names []string) ([]string, error) {
	var selected []string
	var err error
	switch mode {
	case "--all":
		selected = inv.SelectAll()
	case "--bridge":
		selected, err = selectBridge(inv, value)
	case cli.ModeNames:
		selected, err = inv.SelectNames(names)
	default:
		err = freeze.Die("internal error: unknown target mode '%s'.", mode)
	}
	return selected, freeze.DieGone(err)
}

// selectBridge is the members of a bridge in use. A bridge nothing is on is a vanished group, and
// says what is in use so the next step is clear.
func selectBridge(inv freeze.Inventory[record], bridge string) ([]string, error) {
	known := bridgeNames(inv)
	if !slices.Contains(known, bridge) {
		if len(known) == 0 {
			return nil, freeze.Gone("no running or frozen container is on any bridge.")
		}
		return nil, freeze.Gone("no running or frozen container is on bridge '%s'.\n  In use right now: %s", bridge, strings.Join(known, " "))
	}
	return membersOn(inv, bridge), nil
}
