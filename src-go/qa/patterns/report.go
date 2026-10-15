package patterns

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// failure is one failed file in the machine report.
type failure struct {
	File   string `json:"file"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

// summary is the count the stage reader takes from the report.
type summary struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
}

// machineReport is the report written for qa-all.
type machineReport struct {
	Type     string    `json:"type"`
	Status   string    `json:"status"`
	Scanned  []string  `json:"scanned"`
	Summary  summary   `json:"summary"`
	Failures []failure `json:"failures"`
}

// finish prints the coverage notes and the verdict and writes the report.
func (g *gate) finish(scripts []script, res scanResult) (ExitCode, error) {
	g.printPartial(res.partial)
	ratchet, err := g.parseRatchet(res.partial)
	if err != nil {
		return ExitCannotRun, err
	}
	for file, found := range ratchet {
		res.findings[file] = append(res.findings[file], found...)
	}
	rep := machineReport{Type: "patterns", Scanned: make([]string, 0, len(scripts)), Failures: []failure{}}
	for _, s := range scripts {
		rep.Scanned = append(rep.Scanned, s.rel)
	}
	slices.Sort(rep.Scanned)
	for _, file := range slices.Sorted(maps.Keys(res.findings)) {
		found := res.findings[file]
		slices.SortStableFunc(found, func(a, b finding) int {
			if a.line != b.line {
				return a.line - b.line
			}
			return strings.Compare(a.rule, b.rule)
		})
		parts := make([]string, 0, len(found))
		for _, f := range found {
			parts = append(parts, f.rule+":"+strconv.Itoa(f.line)+" "+f.text)
		}
		rep.Failures = append(rep.Failures, failure{File: file, Type: "patterns", Status: "fail", Error: strings.Join(parts, "; ")})
	}
	total := len(scripts)
	rep.Summary = summary{Total: total, Failed: len(rep.Failures), Passed: total - len(rep.Failures)}
	rep.Status = "pass"
	if len(rep.Failures) > 0 {
		rep.Status = "fail"
	}
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return ExitCannotRun, fmt.Errorf("cannot encode the report: %w", err)
	}
	if err := g.env.WriteFile(g.report, append(data, '\n')); err != nil {
		return ExitCannotRun, fmt.Errorf("cannot write the report %s: %w", g.report, err)
	}
	if len(rep.Failures) == 0 {
		fmt.Fprintf(&g.stdout, "✓ patterns: %d files OK\n", total)
		return ExitOK, nil
	}
	fmt.Fprintf(&g.stdout, "✗ patterns: %d/%d files failed → %s\n", len(rep.Failures), total, g.report)
	for _, f := range rep.Failures {
		fmt.Fprintf(&g.stdout, "  ✗ %s\n    %s\n", f.File, f.Error)
	}
	return ExitFailed, nil
}

// printPartial reports the files semgrep parsed only in part: every rule here is a regex over
// raw text, so the gap costs nothing today, and the numbers stay visible for the day an AST rule
// is added.
func (g *gate) printPartial(partial map[string][]lineRange) {
	if len(partial) == 0 {
		return
	}
	fmt.Fprintf(&g.stdout, "⚠ patterns: %d file(s) semgrep parsed only in part (regex rules unaffected):\n", len(partial))
	for _, file := range slices.Sorted(maps.Keys(partial)) {
		gap := unionOf(partial[file])
		fmt.Fprintf(&g.stdout, "    %s — %d of %s lines outside the parse tree (first gap from %d, last to %d)\n",
			file, gap.covered, g.lineCount(file), gap.first, gap.last)
	}
}

// lineCount is the number of newlines in a file, or ? when it cannot be read.
func (g *gate) lineCount(rel string) string {
	n, err := g.lineTotal(rel)
	if err != nil {
		return "?"
	}
	return strconv.Itoa(n)
}

// lineTotal is the number of newlines in a file.
func (g *gate) lineTotal(rel string) (int, error) {
	data, err := os.ReadFile(filepath.Join(g.root, filepath.FromSlash(rel)))
	if err != nil {
		return 0, fmt.Errorf("cannot count the lines of %s: %w", rel, err)
	}
	return strings.Count(string(data), "\n"), nil
}

// unionOf is how many lines the ranges cover together (touching ranges merge), and the first and
// last line they reach.
func unionOf(ranges []lineRange) gaps {
	sorted := slices.Clone(ranges)
	slices.SortFunc(sorted, func(a, b lineRange) int { return a.first - b.first })
	total := gaps{first: sorted[0].first, last: slices.MaxFunc(sorted, func(a, b lineRange) int { return a.last - b.last }).last}
	cur := sorted[0]
	for _, r := range sorted[1:] {
		if r.first <= cur.last+1 {
			cur.last = max(cur.last, r.last)
			continue
		}
		total.covered += cur.last - cur.first + 1
		cur = r
	}
	total.covered += cur.last - cur.first + 1
	return total
}

// gaps is what a set of skipped ranges amounts to.
type gaps struct{ covered, first, last int }
