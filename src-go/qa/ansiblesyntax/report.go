package ansiblesyntax

import (
	"encoding/json"
	"fmt"
	"strings"
)

// summary is the count the stage reader takes from the report.
type summary struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
}

// machineReport is the report written for qa-all.
type machineReport struct {
	Type     string  `json:"type"`
	Status   string  `json:"status"`
	Summary  summary `json:"summary"`
	Results  []entry `json:"results"`
	Failures []entry `json:"failures"`
}

// importsDir is the directory most playbooks live in; the pass line counts it apart so that a
// change in coverage shows in ordinary passing output.
const importsDir = "playbooks/imports/"

// finish prints the findings and the verdict and writes the report.
func (g *gate) finish(results []entry) (ExitCode, error) {
	rep := machineReport{Type: reportType, Status: statusPass, Results: results, Failures: []entry{}}
	outside := 0
	for _, r := range results {
		if !strings.HasPrefix(r.File, importsDir) {
			outside++
		}
		if r.Status == statusFail {
			rep.Failures = append(rep.Failures, r)
			fmt.Fprintf(&g.stdout, "✗ ansible-syntax: %s\n%s\n", r.File, r.Error)
		}
	}
	rep.Summary = summary{Total: len(results), Failed: len(rep.Failures), Passed: len(results) - len(rep.Failures)}
	if len(rep.Failures) > 0 {
		rep.Status = statusFail
	}
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return ExitCannotRun, fmt.Errorf("cannot encode the report: %w", err)
	}
	if err := g.env.WriteFile(g.report, append(data, '\n')); err != nil {
		return ExitCannotRun, fmt.Errorf("cannot write the report %s: %w", g.report, err)
	}
	if rep.Status == statusPass {
		fmt.Fprintf(&g.stdout, "✓ ansible-syntax: %d playbooks OK (%d under %s, %d elsewhere)\n",
			len(results), len(results)-outside, importsDir, outside)
		return ExitOK, nil
	}
	fmt.Fprintf(&g.stdout, "✗ ansible-syntax: %d/%d playbooks failed → %s\n", len(rep.Failures), len(results), g.report)
	return ExitFailed, nil
}
