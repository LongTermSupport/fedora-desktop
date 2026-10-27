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
	// RegexBackref is the regex-backref-escape findings, "<file>:<line>: <why>".
	RegexBackref []string
	// RootUmask is the root-umask findings, "<file>:<line>: <why>".
	RootUmask []string
	// PlayUX is the play-UX findings, "<file>:<line>: <rule>: <why>" (playux.go).
	PlayUX []string
	// Sudoers is the sudoers-rule findings, "<file>:<line>: <why>".
	Sudoers        []string
	Hygiene, Scope []Entry
	Playbooks      int
}

// Errors is how many violations there are.
func (f Findings) Errors() int {
	return len(f.FailFast) + len(f.Hygiene) + len(f.SelfRef) + len(f.Scope) + len(f.FactVars) + len(f.RegexBackref) + len(f.RootUmask) + len(f.PlayUX) + len(f.Sudoers)
}

// Summary is the closing stdout lines: one tick, or the cross with the counts and the pointers.
func (f Findings) Summary(reportPath string) []string {
	if f.Errors() == 0 {
		return []string{fmt.Sprintf("✓ ansible: fail-fast patterns OK; no self-referential vars; no deprecated ansible_<fact> vars; "+
			"regex backreferences escaped right; system files written as root set umask 022; settings checked first, asked for, Ansible CLIs run from the checkout, no_log failures readable; sudoers rules pinned to root-owned targets; scope+guard declarations OK; %d playbook(s) have correct shebang+exec "+
			"(scoped to playbooks/ tasks/ vars/ environment/ roles/ — qa-ansible-syntax counts repo-wide and will be higher)", f.Playbooks)}
	}
	return []string{
		fmt.Sprintf("✗ ansible: %d violation(s) — %d fail-fast, %d hygiene, %d self-ref var, %d scope/guard, %d deprecated fact var, %d regex backref, %d root umask, %d play ux, %d sudoers rule",
			f.Errors(), len(f.FailFast), len(f.Hygiene), len(f.SelfRef), len(f.Scope), len(f.FactVars), len(f.RegexBackref),
			len(f.RootUmask), len(f.PlayUX), len(f.Sudoers)),
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
	FailFast     []string `json:"fail_fast"`
	Hygiene      []string `json:"hygiene"`
	SelfRef      []string `json:"self_ref"`
	Scope        []string `json:"scope"`
	FactVars     []string `json:"fact_vars"`
	RegexBackref []string `json:"regex_backref"`
	RootUmask    []string `json:"root_umask"`
	PlayUX       []string `json:"play_ux"`
	Sudoers      []string `json:"sudoers_rule"`
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
	for _, entry := range f.RegexBackref {
		add(entry, "regex-backref-escape: a backreference with the wrong backslash count for its YAML style and context")
	}
	for _, entry := range f.RootUmask {
		add(entry, "root-umask: a file users read created as root under the inherited umask")
	}
	for _, entry := range f.PlayUX {
		add(entry, "play-ux: a setting checked late or demanded by hand, an Ansible CLI outside the checkout, or a failure no_log hides")
	}
	for _, entry := range f.Sudoers {
		add(entry, "sudoers-rule: a sudoers rule whose target its user can modify or steer")
	}
	report := jsonReport{
		Type:     "ansible",
		Status:   status,
		Summary:  jsonSummary{Total: total, Passed: max(0, total-f.Errors()), Failed: f.Errors()},
		Failures: failures,
		Results:  []string{},
		Checks: jsonChecks{
			FailFast: nonNil(f.FailFast), Hygiene: entryStrings(f.Hygiene), SelfRef: nonNil(f.SelfRef),
			Scope: entryStrings(f.Scope), FactVars: nonNil(f.FactVars), RegexBackref: nonNil(f.RegexBackref),
			RootUmask: nonNil(f.RootUmask), PlayUX: nonNil(f.PlayUX), Sudoers: nonNil(f.Sudoers),
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
