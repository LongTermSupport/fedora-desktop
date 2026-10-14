package ansible

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Findings is what a scan found: the stdout lines of the violations in the order they were found,
// the recorded strings of each check, and how many playbooks the hygiene check examined.
type Findings struct {
	// Out is the violation lines as printed, in check order.
	Out                         []string
	FailFast, SelfRef, FactVars []string
	Hygiene, Scope              []Entry
	Playbooks                   int
}

// Errors is how many violations there are.
func (f Findings) Errors() int {
	return len(f.FailFast) + len(f.Hygiene) + len(f.SelfRef) + len(f.Scope) + len(f.FactVars)
}

// Summary is the closing stdout lines: one tick, or the cross with the counts and the pointers.
func (f Findings) Summary(reportPath string) []string {
	if f.Errors() == 0 {
		return []string{fmt.Sprintf("✓ ansible: fail-fast patterns OK; no self-referential vars; no deprecated ansible_<fact> vars; "+
			"scope+guard declarations OK; %d playbook(s) have correct shebang+exec "+
			"(scoped to playbooks/ tasks/ vars/ environment/ roles/ — qa-ansible-syntax counts repo-wide and will be higher)", f.Playbooks)}
	}
	return []string{
		fmt.Sprintf("✗ ansible: %d violation(s) — %d fail-fast, %d hygiene, %d self-ref var, %d scope/guard, %d deprecated fact var",
			f.Errors(), len(f.FailFast), len(f.Hygiene), len(f.SelfRef), len(f.Scope), len(f.FactVars)),
		"  Hygiene fixer: ./scripts/make-playbooks-executable.bash",
		"  Details: jq '.failures[]' " + reportPath,
	}
}

// Entry is a violation of a whole file: the file and why.
type Entry struct {
	File, Why string
}

// String is the entry as the report's checks lists carry it, "<file> (<why>)".
func (e Entry) String() string { return e.File + " (" + e.Why + ")" }

func entryStrings(entries []Entry) []string {
	list := make([]string, 0, len(entries))
	for _, e := range entries {
		list = append(list, e.String())
	}
	return list
}

type jsonSummary struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
}

type jsonFailure struct {
	File   string `json:"file"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

type jsonChecks struct {
	FailFast []string `json:"fail_fast"`
	Hygiene  []string `json:"hygiene"`
	SelfRef  []string `json:"self_ref"`
	Scope    []string `json:"scope"`
	FactVars []string `json:"fact_vars"`
}

type jsonReport struct {
	Type     string        `json:"type"`
	Status   string        `json:"status"`
	Summary  jsonSummary   `json:"summary"`
	Failures []jsonFailure `json:"failures"`
	Results  []string      `json:"results"`
	Checks   jsonChecks    `json:"checks"`
}

func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

// JSON is the machine report, in the shape and formatting of the bash's `jq -n`.
func (f Findings) JSON() ([]byte, error) {
	total := f.Playbooks + 1
	status := "pass"
	if f.Errors() > 0 {
		status = "fail"
	}
	failures := []jsonFailure{}
	add := func(file, message string) {
		failures = append(failures, jsonFailure{File: file, Type: "ansible", Status: "fail", Error: message})
	}
	for _, entry := range f.FailFast {
		add(entry, "fail-fast pattern without FAIL-FAST-OK annotation")
	}
	for _, entry := range f.Hygiene {
		add(entry.File, "hygiene: "+entry.Why)
	}
	for _, entry := range f.SelfRef {
		add(entry, "self-referential var (Ansible 2.19 recursive-loop error at runtime)")
	}
	for _, entry := range f.Scope {
		add(entry.File, "scope: "+entry.Why)
	}
	for _, entry := range f.FactVars {
		add(entry, "top-level ansible_<fact> var (removed in ansible-core 2.24)")
	}
	report := jsonReport{
		Type:     "ansible",
		Status:   status,
		Summary:  jsonSummary{Total: total, Passed: max(0, total-f.Errors()), Failed: f.Errors()},
		Failures: failures,
		Results:  []string{},
		Checks: jsonChecks{
			FailFast: nonNil(f.FailFast), Hygiene: entryStrings(f.Hygiene), SelfRef: nonNil(f.SelfRef),
			Scope: entryStrings(f.Scope), FactVars: nonNil(f.FactVars),
		},
	}
	var body bytes.Buffer
	enc := json.NewEncoder(&body)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return nil, fmt.Errorf("cannot encode the report: %w", err)
	}
	return body.Bytes(), nil
}
