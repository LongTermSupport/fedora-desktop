package podfreeze

import (
	"slices"
	"sort"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze"
)

// identityAxis is one of the three things CCY stamps on a session: the GitHub account, the
// Anthropic token, the SSH keys. They work the same way and differ only in which field they read
// and whether it holds several values, so one definition is parameterised rather than copied.
type identityAxis struct {
	name  string
	value func(record) string
	multi bool // the label can name several values, so membership is a word match
}

var identityAxes = []identityAxis{
	{name: "github", value: func(r record) string { return r.github }},
	{name: "token", value: func(r record) string { return r.token }},
	{name: "ssh-key", value: func(r record) string { return r.sshKeys }, multi: true},
}

// axisByName resolves an axis name; one that is not one of the three is an internal error and
// fatal, never an empty value (which would be an empty group, silently).
func axisByName(name string) (identityAxis, error) {
	for _, ax := range identityAxes {
		if ax.name == name {
			return ax, nil
		}
	}
	return identityAxis{}, freeze.Die("internal error: unknown identity axis '%s'.", name)
}

// matches is whole-string for the single-valued axes and whole-WORD for ssh-key (split on
// whitespace only, no globbing, no prefix match). Getting this wrong in the lenient direction
// would silently widen a freeze.
func (a identityAxis) matches(want, have string) bool {
	if !a.multi {
		return have == want
	}
	return slices.Contains(strings.Fields(have), want)
}

// identityValues is the distinct values present in the inventory, sorted bytewise, so the menu
// never offers a group with no members.
func identityValues(a identityAxis, inv freeze.Inventory) []string {
	seen := map[string]bool{}
	var values []string
	for _, entry := range inv.Entries() {
		value := a.value(recordOf(entry))
		if value == "" {
			continue
		}
		words := []string{value}
		if a.multi {
			words = strings.Fields(value)
		}
		for _, w := range words {
			if !seen[w] {
				seen[w] = true
				values = append(values, w)
			}
		}
	}
	sort.Strings(values)
	return values
}

// identityNames are the members of one value, in inventory order.
func identityNames(a identityAxis, want string, inv freeze.Inventory) []string {
	var names []string
	for _, entry := range inv.Entries() {
		if a.matches(want, a.value(recordOf(entry))) {
			names = append(names, entry.Name)
		}
	}
	return names
}

// discriminates reports whether the axis tells any two CCY sessions apart, i.e. whether the menu
// should offer its rows. It is COVERAGE, not cardinality: one labelled account plus four
// unlabelled sessions is a single distinct value covering 2 of 6, and suppressing that row would
// leave only the WIDER "all CCY containers", steering toward freezing sessions nobody asked about.
func discriminates(a identityAxis, inv freeze.Inventory) bool {
	values := identityValues(a, inv)
	switch len(values) {
	case 0:
		return false
	case 1:
		return len(identityNames(a, values[0], inv)) < len(ccyNames(inv))
	}
	return true
}

// ccyNames are the CCY sessions, in inventory order.
func ccyNames(inv freeze.Inventory) []string {
	var names []string
	for _, entry := range inv.Entries() {
		if recordOf(entry).ccy {
			names = append(names, entry.Name)
		}
	}
	return names
}

// unlabelledCCY are the sessions that predate the labels. Keyed on label PRESENCE, not on the
// identity values being empty: a 3.40.0 session with "none" on every axis has none either, and
// telling its owner to relaunch it so it can be labelled is advice that cannot work.
func unlabelledCCY(inv freeze.Inventory) []string {
	var names []string
	for _, entry := range inv.Entries() {
		if rec := recordOf(entry); rec.ccy && !rec.labelled {
			names = append(names, entry.Name)
		}
	}
	return names
}

// warnBlindSpot discloses, never blocks: a session from before CCY 3.40.0 is in --ccy but in NO
// identity group, and a partial answer to "everything running as account X" reads exactly like a
// complete one.
func (e *Engine) warnBlindSpot(inv freeze.Inventory) error {
	unlabelled := unlabelledCCY(inv)
	if len(unlabelled) == 0 {
		return nil
	}
	if err := e.say("%s: NOTE — %d CCY session(s) carry no identity labels,\n"+
		"  so they are in no identity group and are NOT in this selection:\n", tool, len(unlabelled)); err != nil {
		return err
	}
	for _, name := range unlabelled {
		if err := e.say("    %s\n", name); err != nil {
			return err
		}
	}
	return e.say("  They predate CCY 3.40.0. Use --ccy to cover every session regardless\n" +
		"  of identity, or relaunch them to make them selectable by identity.\n")
}

// networkMap is network -> member names, in inventory order, rebuilt from the inventory each
// time so the menu only ever offers a network something is attached to. A container can be on
// several networks, so the column is comma-separated, and spaces within it are noise.
func networkMap(inv freeze.Inventory) map[string][]string {
	m := map[string][]string{}
	for _, entry := range inv.Entries() {
		nets := recordOf(entry).nets
		if nets == noNetwork {
			continue
		}
		for net := range strings.SplitSeq(nets, ",") {
			net = strings.ReplaceAll(net, " ", "")
			if net != "" {
				m[net] = append(m[net], entry.Name)
			}
		}
	}
	return m
}

// networkNames are the networks in use, sorted bytewise.
func networkNames(m map[string][]string) []string {
	names := make([]string, 0, len(m))
	for net := range m {
		names = append(names, net)
	}
	sort.Strings(names)
	return names
}
