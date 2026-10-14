package ansible

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestSummaryPass(t *testing.T) {
	got := Findings{Playbooks: 3}.Summary("/tmp/x.json")
	want := []string{"✓ ansible: fail-fast patterns OK; no self-referential vars; no deprecated ansible_<fact> vars; scope+guard declarations OK; 3 playbook(s) have correct shebang+exec (scoped to playbooks/ tasks/ vars/ environment/ roles/ — qa-ansible-syntax counts repo-wide and will be higher)"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestSummaryFailCountsEachList(t *testing.T) {
	f := Findings{
		FailFast: []string{"a"}, Hygiene: []Entry{{"b", "x"}, {"c", "y"}}, SelfRef: []string{"d"}, Scope: []Entry{{"e", "z"}},
		FactVars: []string{"f", "g", "h"},
	}
	got := f.Summary("/tmp/x.json")
	want := []string{
		"✗ ansible: 8 violation(s) — 1 fail-fast, 2 hygiene, 1 self-ref var, 1 scope/guard, 3 deprecated fact var",
		"  Hygiene fixer: ./scripts/make-playbooks-executable.bash",
		"  Details: jq '.failures[]' /tmp/x.json",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
	if f.Errors() != 8 {
		t.Fatalf("Errors() = %d", f.Errors())
	}
}

func TestJSONShapeAndTheSplitQuirks(t *testing.T) {
	f := Findings{
		Playbooks: 1,
		FailFast:  []string{"tasks/a.yml:1:x"},
		Hygiene:   []Entry{{"playbooks/a.yml", "missing shebang + not executable"}},
		SelfRef:   []string{"vars/a.yml:2:y"},
		Scope:     []Entry{{"playbooks/b.yml", "invalid vars.scope: a (b)"}},
		FactVars:  []string{"tasks/c.yml:3:<z> & é"},
	}
	body, err := f.JSON()
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.HasPrefix(text, "{\n  \"type\": \"ansible\",\n  \"status\": \"fail\",\n  \"summary\": {\n    \"total\": 2,\n    \"passed\": 0,\n    \"failed\": 5\n  },\n  \"failures\": [\n") || !strings.HasSuffix(text, "}\n") {
		t.Fatalf("shape:\n%s", text)
	}
	if strings.Contains(text, "\\u003c") || !strings.Contains(text, "<z> & é") {
		t.Fatalf("HTML escaped:\n%s", text)
	}
	var got struct {
		Failures []struct {
			File   string `json:"file"`
			Type   string `json:"type"`
			Status string `json:"status"`
			Error  string `json:"error"`
		} `json:"failures"`
		Results []string `json:"results"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	wantErrors := []string{
		"fail-fast pattern without FAIL-FAST-OK annotation",
		"hygiene: missing shebang + not executable",
		"self-referential var (Ansible 2.19 recursive-loop error at runtime)",
		"scope: invalid vars.scope: a (b)",
		"top-level ansible_<fact> var (removed in ansible-core 2.24)",
	}
	wantFiles := []string{"tasks/a.yml:1:x", "playbooks/a.yml", "vars/a.yml:2:y", "playbooks/b.yml", "tasks/c.yml:3:<z> & é"}
	for i, fl := range got.Failures {
		if fl.Error != wantErrors[i] || fl.File != wantFiles[i] || fl.Type != "ansible" || fl.Status != "fail" {
			t.Errorf("failure %d = %+v", i, fl)
		}
	}
	if len(got.Failures) != 5 || got.Results == nil || len(got.Results) != 0 {
		t.Fatalf("failures %d results %v", len(got.Failures), got.Results)
	}
}

func TestJSONOfAPassIsEmptyArrays(t *testing.T) {
	body, err := Findings{Playbooks: 2}.JSON()
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"type\": \"ansible\",\n  \"status\": \"pass\",\n  \"summary\": {\n    \"total\": 3,\n    \"passed\": 3,\n    \"failed\": 0\n  },\n  \"failures\": [],\n  \"results\": [],\n  \"checks\": {\n    \"fail_fast\": [],\n    \"hygiene\": [],\n    \"self_ref\": [],\n    \"scope\": [],\n    \"fact_vars\": []\n  }\n}\n"
	if string(body) != want {
		t.Fatalf("got\n%s", body)
	}
}
