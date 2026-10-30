package containerimages

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// itemKey names an item: its kind and key.
type itemKey struct{ kind, key string }

// decisions is a decisions file (O11): the old range, and one action per item.
type decisions struct {
	old     Range
	actions map[itemKey]string
}

// parseDecisions reads a decisions file: `old-range START COUNT` once, then `ACTION KIND KEY`
// lines, fields split on any white space; # starts a comment line.
func parseDecisions(text string) (decisions, error) {
	d := decisions{actions: map[itemKey]string{}}
	seenRange := false
	n := 0
	for line := range strings.Lines(text) {
		n++
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		if len(f) != 3 {
			return d, fmt.Errorf("decisions line %d has %d fields, not 3", n, len(f))
		}
		if f[0] == "old-range" {
			start, err1 := strconv.ParseInt(f[1], 10, 64)
			count, err2 := strconv.ParseInt(f[2], 10, 64)
			if err := errors.Join(err1, err2); err != nil || seenRange {
				return d, fmt.Errorf("decisions line %d: one old-range START COUNT line, in numbers, is wanted", n)
			}
			d.old, seenRange = Range{Start: start, Count: count}, true
			continue
		}
		if !slices.Contains([]string{actRemove, actReown, actLeave, actAsk}, f[0]) || !slices.Contains(kindOrder, f[1]) {
			return d, fmt.Errorf("decisions line %d: %q is not ACTION (remove, reown, leave) KIND (image, container, volume, layer, path) KEY", n, strings.TrimSpace(line))
		}
		k := itemKey{f[1], f[2]}
		if _, dup := d.actions[k]; dup {
			return d, fmt.Errorf("decisions line %d: %s %s is decided twice", n, f[1], f[2])
		}
		d.actions[k] = f[0]
	}
	if !seenRange {
		return d, errors.New("the decisions name no old-range START COUNT line")
	}
	return d, nil
}

// proposedAction is the action a proposal writes: ask for ASK, leave for HELD.
func proposedAction(proposal string) string {
	switch proposal {
	case proposeRemove:
		return actRemove
	case proposeReown:
		return actReown
	case proposeHeld:
		return actLeave
	default:
		return actAsk
	}
}

// proposedDecisions is the decisions file the triage proposes: every ask is the host agent's
// to replace.
func proposedDecisions(items []item, old Range) string {
	var b strings.Builder
	b.WriteString("# container-images old-range decisions (Plan 00235 Q6). One ACTION KIND KEY per item:\n" +
		"# remove (podman removes it, after root re-owns its files), reown (kept, its files re-owned),\n" +
		"# leave (untouched; the store stays held off). Replace every ask. Keep this file under untracked/.\n")
	fmt.Fprintf(&b, "old-range\t%d\t%d\n", old.Start, old.Count)
	for _, it := range items {
		fmt.Fprintf(&b, "\n# %s %s %s%s: %s (may: %s)\n%s\t%s\t%s\n", it.proposal, it.kind, it.key, namesOf(it), it.why,
			strings.Join(it.allowed, ", "), proposedAction(it.proposal), it.kind, it.key)
	}
	return b.String()
}

func namesOf(it item) string {
	if len(it.names) == 0 {
		return ""
	}
	return " (" + strings.Join(it.names, ", ") + ")"
}

// PlanStep is one step of a plan: a store directory to re-own or remove as root, or a thing
// podman removes as the user.
type PlanStep struct {
	Kind     string `json:"kind"`
	Key      string `json:"key"`
	Name     string `json:"name"`
	Why      string `json:"why"`
	External bool   `json:"external,omitempty"`
}

// Plan is what `old-range plan` prints and `reown` and `remove` carry out.
type Plan struct {
	Store      string     `json:"store"`
	Owners     Owners     `json:"owners"`
	Old        Range      `json:"old"`
	Reown      []PlanStep `json:"reown"`
	RootRemove []PlanStep `json:"rootRemove"`
	Remove     []PlanStep `json:"remove"`
	Held       []PlanStep `json:"held"`
	Left       []string   `json:"left"`
	Done       []string   `json:"done"`
}

// check is O11 for one item's action: nil when it is safe.
func check(it item, action string, actions map[itemKey]string) error {
	what := it.kind + " " + it.key
	switch {
	case action == actAsk:
		return fmt.Errorf("%s is still ask: decide it", what)
	case action != actLeave && !slices.Contains(it.allowed, action):
		return fmt.Errorf("%s may not be %s; it may be %s", what, action, strings.Join(it.allowed, ", "))
	case action != actLeave && it.proposal == proposeHeld:
		return fmt.Errorf("%s is held (%s); only leave is possible while that lasts", what, it.why)
	case action == actReown && it.unshiftable, action == actRemove && it.unshiftable && it.kind != kindPath:
		return fmt.Errorf("%s has owners outside the old range, so it cannot be shifted; only leave is possible", what)
	case action == actRemove && it.used:
		return fmt.Errorf("%s is used by %s, so it cannot be removed", what, strings.Join(it.users, ", "))
	case action == actRemove && it.kind == kindImage:
		return childrenRemoved(it, actions)
	}
	return nil
}

// childrenRemoved is O6: an image is removed only with every image built on it.
func childrenRemoved(it item, actions map[itemKey]string) error {
	for _, child := range it.children {
		if actions[itemKey{kindImage, child}] != actRemove {
			return fmt.Errorf("%s %s cannot be removed unless image %s, built on it, is removed too", it.kind, it.key, short(child))
		}
	}
	return nil
}

// notItems is every decided key that is no item of the store now, in order.
func notItems(items []item, d decisions) []itemKey {
	known := map[itemKey]bool{}
	for _, it := range items {
		known[itemKey{it.kind, it.key}] = true
	}
	var out []itemKey
	for _, k := range slices.SortedFunc(maps.Keys(d.actions), func(a, b itemKey) int { return cmp.Or(cmp.Compare(a.kind, b.kind), cmp.Compare(a.key, b.key)) }) {
		if !known[k] {
			out = append(out, k)
		}
	}
	return out
}

// done is O11 for a decided key that is no item now, given whether the store still has it: a
// remove it no longer has is already gone, a reown it still has holds no old-range file; any
// other is not done.
func done(k itemKey, action string, present bool) (string, bool) {
	switch {
	case action == actRemove && !present:
		return "already gone: " + k.kind + " " + k.key, true
	case action == actReown && present:
		return "already re-owned: " + k.kind + " " + k.key, true
	}
	return "", false
}

// makePlan is O11: the decisions checked against the items, then the steps; present is whether
// the store still has each decided key that is no item now.
func makePlan(scan Scan, items []item, d decisions, present map[itemKey]bool) (Plan, error) {
	p := Plan{
		Store: scan.Store, Owners: scan.Owners, Old: d.old,
		Reown: []PlanStep{}, RootRemove: []PlanStep{}, Remove: []PlanStep{}, Held: []PlanStep{}, Left: []string{}, Done: []string{},
	}
	if err := scan.Owners.checkOld(d.old); err != nil {
		return p, err
	}
	var errs []error
	for _, it := range items {
		k := itemKey{it.kind, it.key}
		action, decided := d.actions[k]
		if !decided {
			errs = append(errs, fmt.Errorf("%s %s is not decided", it.kind, it.key))
			continue
		}
		if err := check(it, action, d.actions); err != nil {
			errs = append(errs, err)
		}
	}
	for _, k := range notItems(items, d) {
		if line, ok := done(k, d.actions[k], present[k]); ok {
			p.Done = append(p.Done, line)
			continue
		}
		errs = append(errs, fmt.Errorf("%s %s is decided %s but is no item of the store now; triage again", k.kind, k.key, d.actions[k]))
	}
	if err := errors.Join(errs...); err != nil {
		return p, err
	}
	p.steps(items, d.actions)
	return p, nil
}

// steps fills a checked plan: root's re-owns and removals, then podman's removals, containers
// first, images built on others before those, volumes last.
func (p *Plan) steps(items []item, actions map[itemKey]string) {
	seen := map[string]bool{}
	var images, volumes []item
	for _, it := range items {
		action := actions[itemKey{it.kind, it.key}]
		switch {
		case action == actLeave:
			p.Left = append(p.Left, it.kind+" "+it.key)
			continue
		case action == actRemove && it.kind == kindPath:
			p.RootRemove = append(p.RootRemove, PlanStep{Kind: kindPath, Key: it.key, Name: it.key, Why: it.why})
			continue
		}
		what := it.kind + " " + it.key + namesOf(it)
		for _, dir := range it.dirs {
			if seen[dir] {
				continue
			}
			seen[dir] = true
			if why, held := it.heldDirs[dir]; held {
				p.Held = append(p.Held, PlanStep{Kind: "dir", Key: dir, Name: dir, Why: what + ": " + why})
				continue
			}
			p.Reown = append(p.Reown, PlanStep{Kind: "dir", Key: dir, Name: dir, Why: what})
		}
		if action != actRemove {
			continue
		}
		switch it.kind {
		case kindImage:
			images = append(images, it)
		case kindVolume:
			volumes = append(volumes, it)
		default:
			p.Remove = append(p.Remove, PlanStep{Kind: it.kind, Key: it.key, Name: cmp.Or(strings.Join(it.names, ", "), it.key), Why: it.why, External: it.external})
		}
	}
	slices.SortStableFunc(images, func(a, b item) int { return cmp.Compare(b.depth, a.depth) })
	for _, it := range append(images, volumes...) {
		p.Remove = append(p.Remove, PlanStep{Kind: it.kind, Key: it.key, Name: cmp.Or(strings.Join(it.names, ", "), short(it.key)), Why: it.why})
	}
}
