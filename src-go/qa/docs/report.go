package docs

import (
	"encoding/json"
	"fmt"
	"slices"
)

const (
	reportType = "docs"
	statusPass = "pass"
	statusFail = "fail"
)

// summary is the count the stage reader takes from the report.
type summary struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
}

// failure is one finding as the merged report lists it.
type failure struct {
	File   string `json:"file"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

// machineReport is the report written for qa-all.
type machineReport struct {
	Type     string         `json:"type"`
	Status   string         `json:"status"`
	Summary  summary        `json:"summary"`
	Vendored vendoredCounts `json:"vendored"`
	Results  []finding      `json:"results"`
	Failures []failure      `json:"failures"`
}

// summaryOf counts files: the failed are the distinct files with a finding, the total is the
// population plus any such file outside it (the gate table), so what passed is never negative.
func summaryOf(population []string, findings []finding) summary {
	failed := map[string]bool{}
	for _, f := range findings {
		failed[f.File] = true
	}
	total := len(population)
	for file := range failed {
		if !slices.Contains(population, file) {
			total++
		}
	}
	return summary{Total: total, Passed: total - len(failed), Failed: len(failed)}
}

// vendoredWarning is the block for links into a vendored repository that IS present here and no
// longer has the target. It is built on every run and empty on a clean one, so the formatting is
// exercised on the common path. The detail lines are indented: a stage symbol belongs in column 0
// only on the lines that are stages.
func vendoredWarning(broken []finding) []string {
	if len(broken) == 0 {
		return nil
	}
	lines := []string{fmt.Sprintf("⚠ docs: %d link(s) into a PRESENT vendored repo are broken — it has probably moved the file:", len(broken))}
	for _, b := range broken {
		lines = append(lines, fmt.Sprintf("    %s:%d  %s", b.File, b.Line, b.Target))
	}
	return lines
}

// finish prints the verdict and writes the report.
func (g *gate) finish(population []string, tracked trackedPaths, findings []finding, vendored vendoredCounts) (ExitCode, error) {
	rep := machineReport{
		Type: reportType, Status: statusPass, Summary: summaryOf(population, findings),
		Vendored: vendored, Results: findings, Failures: []failure{},
	}
	for _, f := range findings {
		rep.Failures = append(rep.Failures, failure{File: f.File, Type: reportType, Status: statusFail, Error: f.Target + " — " + f.Problem})
	}
	if len(findings) > 0 {
		rep.Status = statusFail
	}
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return ExitCannotRun, fmt.Errorf("cannot encode the report: %w", err)
	}
	if err := g.env.WriteFile(g.report, append(data, '\n')); err != nil {
		return ExitCannotRun, fmt.Errorf("cannot write the report %s: %w", g.report, err)
	}
	for _, line := range vendoredWarning(vendored.Broken) {
		fmt.Fprintln(&g.stdout, line)
	}
	files := fmt.Sprintf("%d files (%d tracked)", len(population), tracked.trackedCount(population))
	counts := fmt.Sprintf("VENDORED: %d verified, %d unverifiable (repo absent), %d broken", vendored.OK, vendored.Unverifiable, len(vendored.Broken))
	if len(findings) == 0 {
		fmt.Fprintf(&g.stdout, "✓ docs: %s OK (links, anchors, playbook catalogue, topic index, gate inventory) — %s\n", files, counts)
		return ExitOK, nil
	}
	fmt.Fprintf(&g.stdout, "✗ docs: %d finding(s) across %s — %s\n", len(findings), files, counts)
	for _, f := range findings {
		fmt.Fprintf(&g.stdout, "  %s:%d  %s  — %s\n", f.File, f.Line, f.Target, f.Problem)
	}
	fmt.Fprintf(&g.stdout, "  Details: jq '.failures[]' %s\n", g.report)
	return ExitFailed, nil
}
