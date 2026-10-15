package bash

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// The words the report and the checks use more than once.
const (
	statusPass = "pass"
	statusFail = "fail"
	typeBash   = "bash"
)

// entry is one checked file in the machine report.
type entry struct {
	File   string `json:"file"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// summary is the count the stage reader takes from the report.
type summary struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
}

// machineReport is the report written for qa-all.
type machineReport struct {
	Type        string                       `json:"type"`
	Status      string                       `json:"status"`
	Summary     summary                      `json:"summary"`
	Results     []entry                      `json:"results"`
	Failures    []entry                      `json:"failures"`
	Diagnostics []map[string]json.RawMessage `json:"shellcheck_diagnostics"`
}

// writeReport encodes the report and writes it where the caller said.
func (g *gate) writeReport(rep machineReport) error {
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot encode the report: %w", err)
	}
	if err := g.env.WriteFile(g.report, append(data, '\n')); err != nil {
		return fmt.Errorf("cannot write the report %s: %w", g.report, err)
	}
	return nil
}

// byPlace orders findings by file, line, column and code.
func byPlace(a, b diagnostic) int {
	return cmp.Or(strings.Compare(a.file, b.file), cmp.Compare(a.line, b.line), cmp.Compare(a.column, b.column), cmp.Compare(a.code, b.code))
}

// finish prints the findings and the verdict and writes the report.
func (g *gate) finish(files []string, syntax map[string]string, diagnostics []diagnostic, hidden []dfinding) (ExitCode, error) {
	var gating []diagnostic
	for _, d := range diagnostics {
		if d.gating() {
			gating = append(gating, d)
		}
	}
	slices.SortStableFunc(gating, byPlace)
	byFile := map[string][]diagnostic{}
	for _, d := range gating {
		byFile[d.file] = append(byFile[d.file], d)
	}
	rep := machineReport{Type: typeBash, Status: statusPass, Results: []entry{}, Failures: []entry{}, Diagnostics: []map[string]json.RawMessage{}}
	hiddenBy := map[string][]dfinding{}
	for _, f := range hidden {
		hiddenBy[f.file] = append(hiddenBy[f.file], f)
	}
	for _, rel := range files {
		if msg := syntax[rel]; msg != "" {
			fmt.Fprintf(&g.stdout, "✗ bash: %s: %s\n", rel, msg)
		}
		rep.Results = append(rep.Results, judge(rel, syntax[rel], byFile[rel], hiddenBy[rel]))
		delete(hiddenBy, rel)
	}
	// A finding about a path that is not a gated file (a stale record) is its own entry.
	for rel, found := range hiddenBy {
		rep.Results = append(rep.Results, judge(rel, "", nil, found))
	}
	slices.SortStableFunc(rep.Results, func(a, b entry) int { return strings.Compare(a.File, b.File) })
	for _, d := range diagnostics {
		rep.Diagnostics = append(rep.Diagnostics, d.raw)
	}
	g.sayShellcheck(len(diagnostics), gating)
	for _, f := range hidden {
		fmt.Fprintln(&g.stdout, f.text())
	}
	for _, r := range rep.Results {
		if r.Status == statusFail {
			rep.Failures = append(rep.Failures, r)
		}
	}
	rep.Summary = summary{Total: len(rep.Results), Failed: len(rep.Failures), Passed: len(rep.Results) - len(rep.Failures)}
	if len(rep.Failures) > 0 {
		rep.Status = statusFail
	}
	if err := g.writeReport(rep); err != nil {
		return ExitCannotRun, err
	}
	if rep.Status == statusPass {
		fmt.Fprintf(&g.stdout, "✓ bash: %d files OK\n", len(rep.Results))
		return ExitOK, nil
	}
	fmt.Fprintf(&g.stdout, "✗ bash: %d/%d files failed → %s\n", len(rep.Failures), len(rep.Results), g.report)
	return ExitFailed, nil
}

// sayShellcheck prints the advisory line for any finding and the gating lines.
func (g *gate) sayShellcheck(total int, gating []diagnostic) {
	if total > 0 {
		fmt.Fprintf(&g.stdout, "⚠ shellcheck: %d issues (see %s .checks.bash.shellcheck_diagnostics)\n", total, g.merged)
	}
	if len(gating) == 0 {
		return
	}
	fmt.Fprintf(&g.stdout, "✗ shellcheck: %d error/warning-level finding(s) — gating\n", len(gating))
	for _, d := range gating {
		fmt.Fprintf(&g.stdout, "    %s:%d SC%d (%s) %s\n", d.file, d.line, d.code, d.level, d.text)
	}
}

// judge is the result for one file: failed when it does not parse or has a gating finding, its
// error being the syntax message and then the findings (already in place order).
func judge(rel, syntaxErr string, findings []diagnostic, hidden []dfinding) entry {
	var parts []string
	if syntaxErr != "" {
		parts = append(parts, syntaxErr)
	}
	for _, d := range findings {
		parts = append(parts, fmt.Sprintf("SC%d:%d (%s) %s", d.code, d.line, d.level, d.text))
	}
	for _, f := range hidden {
		parts = append(parts, f.errorPart())
	}
	r := entry{File: rel, Type: typeBash, Status: statusPass}
	if len(parts) > 0 {
		r.Status, r.Error = statusFail, strings.Join(parts, "; ")
	}
	return r
}
