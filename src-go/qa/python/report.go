package python

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// The words the report and the checks use more than once.
const (
	statusPass = "pass"
	statusFail = "fail"
	typePython = "python"
	toolRuff   = "ruff"
)

// entry is one checked file or template in the machine report.
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
	Type            string                       `json:"type"`
	Status          string                       `json:"status"`
	Summary         summary                      `json:"summary"`
	Results         []entry                      `json:"results"`
	Failures        []entry                      `json:"failures"`
	RuffDiagnostics []map[string]json.RawMessage `json:"ruff_diagnostics"`
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

// finish prints the findings and the verdict and writes the report.
func (g *gate) finish(f sources, rendered []renderedTemplate, compiled map[string]string, diagnostics []diagnostic) (ExitCode, error) {
	byFile := map[string][]diagnostic{}
	for _, d := range diagnostics {
		byFile[d.file] = append(byFile[d.file], d)
	}
	rep := machineReport{Type: typePython, Status: statusPass, Results: []entry{}, Failures: []entry{}, RuffDiagnostics: []map[string]json.RawMessage{}}
	for _, rel := range f.files {
		r := g.judge(rel, typePython, compiled[rel], byFile[rel])
		if msg := compiled[rel]; msg != "" {
			fmt.Fprintf(&g.stdout, "✗ python: %s: %s\n", rel, msg)
		}
		rep.Results = append(rep.Results, r)
	}
	for _, t := range rendered {
		r := g.judge(t.rel, "python-template", compiled[t.rel], nil)
		if msg := compiled[t.rel]; msg != "" {
			fmt.Fprintf(&g.stdout, "✗ python: %s (rendered): %s\n", t.rel, msg)
		}
		rep.Results = append(rep.Results, r)
	}
	for _, d := range diagnostics {
		rep.RuffDiagnostics = append(rep.RuffDiagnostics, d.raw)
	}
	if len(diagnostics) > 0 {
		fmt.Fprintf(&g.stdout, "✗ python: ruff: %d issues in %d file(s) (see %s .ruff_diagnostics)\n", len(diagnostics), len(byFile), g.report)
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
		fmt.Fprintf(&g.stdout, "✓ python: %d files OK; %d rendered template(s) compile (syntax only, not linted)\n", len(f.files), len(rendered))
		return ExitOK, nil
	}
	fmt.Fprintf(&g.stdout, "✗ python: failed → %s\n", g.report)
	return ExitFailed, nil
}

// judge is the result for one file: failed when it did not compile or ruff has a diagnostic on
// it, its error being the compile message and then the diagnostics in line order.
func (g *gate) judge(rel, typ, compileErr string, diags []diagnostic) entry {
	slices.SortStableFunc(diags, func(a, b diagnostic) int {
		if a.row != b.row {
			return a.row - b.row
		}
		return strings.Compare(a.code, b.code)
	})
	var parts []string
	if compileErr != "" {
		parts = append(parts, compileErr)
	}
	for _, d := range diags {
		parts = append(parts, d.code+":"+strconv.Itoa(d.row)+" "+d.text)
	}
	r := entry{File: rel, Type: typ, Status: statusPass}
	if len(parts) > 0 {
		r.Status, r.Error = statusFail, strings.Join(parts, "; ")
	}
	return r
}
