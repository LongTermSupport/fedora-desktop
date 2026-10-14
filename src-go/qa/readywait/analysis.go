package readywait

import (
	"slices"
	"sort"
	"strings"
)

// analysis is one script read: its blanked text and what is known of its functions.
type analysis struct {
	code        string
	functions   []function
	byName      map[string]function
	definitions map[string]int
	launchers   map[string]bool
	checkers    []string
}

func analyse(text string) (*analysis, error) {
	shell, err := readShell(text)
	if err != nil {
		return nil, err
	}
	a := &analysis{
		code:        shell.code,
		byName:      map[string]function{},
		definitions: map[string]int{},
		launchers:   map[string]bool{},
	}
	a.functions = shell.functions
	for _, f := range a.functions {
		a.byName[f.name] = f
		keepsPid := len(launchesIn(a.code, f.start, f.end)) > 0 && strings.Contains(a.code[f.start:f.end], "$!")
		if keepsPid || len(managerStarts(a.code, f.start, f.end)) > 0 {
			a.launchers[f.name] = true
		}
		if livenessRe.MatchString(a.code[f.start:f.end]) {
			a.checkers = append(a.checkers, f.name)
		}
		a.definitions[f.name] = f.defStart + strings.Index(a.code[f.defStart:], f.name)
	}
	return a, nil
}

func (a *analysis) inFunction(pos int) bool {
	return slices.ContainsFunc(a.functions, func(f function) bool { return f.defStart <= pos && pos < f.end })
}

// events lists what happens in code[start:end], in order; at the top level, what happens outside
// every function.
func (a *analysis) events(start, end int, top bool) []event {
	var found []event
	add := func(kind eventKind, positions []int) {
		for _, p := range positions {
			found = append(found, event{pos: p, kind: kind})
		}
	}
	add(evLaunch, launchesIn(a.code, start, end))
	add(evLaunch, managerStarts(a.code, start, end))
	add(evDisarm, disarms(a.code, start, end))
	add(evLoop, unguardedPolls(a.code, start, end, a.checkers))
	for _, f := range a.functions {
		for _, p := range positionsOf(a.code, start, end, f.name) {
			if p != a.definitions[f.name] && !backgrounded(a.code, p+len(f.name)) {
				found = append(found, event{pos: p, kind: evCall, name: f.name})
			}
		}
	}
	if top {
		found = slices.DeleteFunc(found, func(e event) bool { return a.inFunction(e.pos) })
	}
	sort.SliceStable(found, func(x, y int) bool { return found[x].pos < found[y].pos })
	return found
}

// flagScope adds the loops that are waits in one scope to flagged.
func (a *analysis) flagScope(start, end int, top bool, flagged map[int]bool) {
	armed := false
	for _, e := range a.events(start, end, top) {
		switch {
		case e.kind == evLaunch || (e.kind == evCall && a.launchers[e.name]):
			armed = true
		case e.kind == evDisarm:
			armed = false
		case armed && e.kind == evLoop:
			flagged[e.pos] = true
		case armed && e.kind == evCall:
			f := a.byName[e.name]
			for _, p := range unguardedPolls(a.code, f.start, f.end, a.checkers) {
				flagged[p] = true
			}
		}
	}
}

// Findings is the 1-based lines of the ready-waits in text that ignore their child, sorted, one
// per line.
func Findings(text string) ([]int, error) {
	a, err := analyse(text)
	if err != nil {
		return nil, err
	}
	flagged := map[int]bool{}
	for _, f := range a.functions {
		a.flagScope(f.start, f.end, false, flagged)
	}
	a.flagScope(0, len(a.code), true, flagged)
	lines := map[int]bool{}
	for pos := range flagged {
		lines[strings.Count(a.code[:pos], "\n")+1] = true
	}
	sorted := make([]int, 0, len(lines))
	for line := range lines {
		sorted = append(sorted, line)
	}
	slices.Sort(sorted)
	return sorted, nil
}
