package js

import (
	"encoding/json"
	"fmt"
	"strings"
)

// The words the report uses more than once.
const (
	statusPass = "pass"
	statusFail = "fail"
	typeJS     = "js"
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

// eslintBlock says whether ESLint ran, how it ended, and how many findings it made.
type eslintBlock struct {
	Ran    bool   `json:"ran"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

// machineReport is the report written for qa-all.
type machineReport struct {
	Type     string      `json:"type"`
	Status   string      `json:"status"`
	Summary  summary     `json:"summary"`
	Results  []entry     `json:"results"`
	Failures []entry     `json:"failures"`
	Eslint   eslintBlock `json:"eslint"`
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

// finish prints the findings and the verdict and writes the report. files is sorted, so output
// and report are in path order.
func (g *gate) finish(files, linted []string, parse map[string]string, lint map[string][]message) (ExitCode, error) {
	rep := machineReport{Type: typeJS, Status: statusPass, Results: []entry{}, Failures: []entry{}}
	messages := 0
	for _, rel := range files {
		var parts []string
		if msg, bad := parse[rel]; bad {
			fmt.Fprintf(&g.stdout, "✗ js: %s: %s\n", rel, msg)
			parts = append(parts, msg)
		}
		for _, m := range lint[rel] {
			fmt.Fprintf(&g.stdout, "✗ js: %s:%d:%d: %s%s\n", rel, m.Line, m.Column, m.Text, ruleSuffix(m))
			parts = append(parts, fmt.Sprintf("%d:%d %s", m.Line, m.Column, strings.TrimSpace(ruleName(m)+" "+m.Text)))
			messages++
		}
		e := entry{File: rel, Type: typeJS, Status: statusPass}
		if len(parts) > 0 {
			e.Status, e.Error = statusFail, strings.Join(parts, "; ")
			rep.Failures = append(rep.Failures, e)
		}
		rep.Results = append(rep.Results, e)
	}
	rep.Eslint = eslintBlock{Ran: true, Status: statusPass}
	if messages > 0 {
		rep.Eslint.Status, rep.Eslint.Error = statusFail, fmt.Sprintf("%d message(s)", messages)
	}
	rep.Summary = summary{Total: len(files), Failed: len(rep.Failures), Passed: len(files) - len(rep.Failures)}
	if len(rep.Failures) > 0 {
		rep.Status = statusFail
	}
	if err := g.writeReport(rep); err != nil {
		return ExitCannotRun, err
	}
	if rep.Status == statusPass {
		fmt.Fprintf(&g.stdout, "✓ js: %d files OK (%d linted by eslint)\n", len(files), len(linted))
		return ExitOK, nil
	}
	fmt.Fprintf(&g.stdout, "✗ js: %d failing file(s) of %d → %s\n", len(rep.Failures), len(files), g.report)
	return ExitFailed, nil
}

// ruleName is the rule that made a finding, "" for a parse error.
func ruleName(m message) string {
	if m.Rule == nil {
		return ""
	}
	return *m.Rule
}

// ruleSuffix is " (rule)" for a finding made by a rule.
func ruleSuffix(m message) string {
	if name := ruleName(m); name != "" {
		return " (" + name + ")"
	}
	return ""
}
