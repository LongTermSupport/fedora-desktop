package ansible

import (
	"slices"
	"strings"
	"testing"
)

func lines(text string) []string { return splitLines([]byte(text)) }

func TestScanScopeReadsScopeAndTheFirstTwoTasks(t *testing.T) {
	const play = `- hosts: all
  name: P
  vars:
    other: 1
    scope: gnome   # why
  tasks:
    - name: First
      ansible.builtin.assert:
        that: true
    - name: Second | with pipe
      ansible.builtin.meta: end_play  # e
      when: some > thing   # w
    - name: Third
      ansible.builtin.meta: nope
      when: never
`
	got := scanScope(lines(play))
	want := scopeInfo{count: 1, value: "gnome", task1: "First", task2: "Second | with pipe", meta: "end_play", when: "some > thing"}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestScanScopeRules(t *testing.T) {
	cases := []struct {
		label string
		text  string
		want  scopeInfo
	}{
		{"no vars", "- hosts: all\n  tasks:\n    - name: A\n", scopeInfo{task1: "A"}},
		{"empty scope is not counted", "  vars:\n    scope:\n", scopeInfo{}},
		{"two scopes, last wins", "  vars:\n    scope: general\n    scope: server\n", scopeInfo{count: 2, value: "server"}},
		{"wrong indent", "  vars:\n      scope: general\n", scopeInfo{}},
		{"vars ended by a 2-space key", "  vars:\n    a: 1\n  gather_facts: no\n    scope: general\n", scopeInfo{}},
		{"vars ended by a column-0 line", "  vars:\n    a: 1\n- hosts: b\n    scope: general\n", scopeInfo{}},
		{"scope with hash directly", "  vars:\n    scope: general#x\n", scopeInfo{count: 1, value: "general"}},
		{"scope only a comment", "  vars:\n    scope: # general\n", scopeInfo{}},
		{"crlf stripped", "  vars:\r\n    scope: general\r\n", scopeInfo{count: 1, value: "general"}},
		{"task name verbatim", "  tasks:\n    - name: A # c\r\n", scopeInfo{task1: "A # c\r"}},
		{"meta before second task is ignored", "  tasks:\n    - name: A\n      ansible.builtin.meta: x\n", scopeInfo{task1: "A"}},
		{"tasks reset", "  tasks:\n    - name: A\n  tasks:\n    - name: B\n", scopeInfo{task1: "B"}},
		{"handlers end tasks", "  tasks:\n    - name: A\n    - name: B\n  handlers:\n    - name: H\n      when: z\n", scopeInfo{task1: "A", task2: "B"}},
		{"third task ends reading", "  tasks:\n    - name: A\n    - name: B\n    - name: C\n      when: z\n", scopeInfo{task1: "A", task2: "B"}},
		{"empty name", "  tasks:\n    - name:\n", scopeInfo{}},
	}
	for _, c := range cases {
		if got := scanScope(lines(c.text)); got != c.want {
			t.Errorf("%s: got %+v want %+v", c.label, got, c.want)
		}
	}
}

func guardedPlay(scope string) []string {
	return lines("  vars:\n    scope: " + scope + "\n  tasks:\n    - name: " + guardAssertName + "\n    - name: " + guardEndName +
		"\n      ansible.builtin.meta: end_play\n      when: " + guardEndWhen + "\n")
}

func TestHygieneNamesEachFailure(t *testing.T) {
	good := playbook{rel: "playbooks/a.yml", lines: lines(playbookShebang + "\n- hosts: all\n"), executable: true}
	noShebang := playbook{rel: "playbooks/b.yml", lines: lines("---\n- hosts: all\n"), executable: true}
	noExec := playbook{rel: "playbooks/c.yml", lines: good.lines}
	both := playbook{rel: "playbooks/d.yml", lines: noShebang.lines}
	crlf := playbook{rel: "playbooks/e.yml", lines: lines(playbookShebang + "\r\n- hosts: all\n"), executable: true}
	result := hygiene([]playbook{good, noShebang, noExec, both, crlf})
	wantOut := []string{
		"  ERROR (hygiene): playbooks/b.yml — missing shebang",
		"  ERROR (hygiene): playbooks/c.yml — not executable",
		"  ERROR (hygiene): playbooks/d.yml — missing shebang + not executable",
		"  ERROR (hygiene): playbooks/e.yml — missing shebang",
	}
	wantRecorded := []string{
		"playbooks/b.yml (missing shebang)",
		"playbooks/c.yml (not executable)",
		"playbooks/d.yml (missing shebang + not executable)",
		"playbooks/e.yml (missing shebang)",
	}
	if !slices.Equal(result.out, wantOut) || !slices.Equal(entryStrings(result.entries), wantRecorded) {
		t.Fatalf("out %q\nrecorded %q", result.out, entryStrings(result.entries))
	}
}

func TestScopeChecksEachRule(t *testing.T) {
	const root = "/r"
	mk := func(rel string, body []string) playbook {
		return playbook{rel: rel, lines: append([]string{"- hosts: all"}, body...), hosts: 1}
	}
	cases := []struct {
		label   string
		pb      playbook
		wantOut string
		wantRec string
	}{
		{
			"missing", mk("playbooks/a.yml", lines("  vars:\n    x: 1\n")),
			"  ERROR (scope): playbooks/a.yml — missing vars.scope (need exactly one of general|gnome|server)", "playbooks/a.yml (missing vars.scope)",
		},
		{
			"multiple", mk("playbooks/a.yml", lines("  vars:\n    scope: general\n    scope: general\n")),
			"  ERROR (scope): playbooks/a.yml — multiple vars.scope entries declared (exactly one required)", "playbooks/a.yml (multiple vars.scope entries)",
		},
		{
			"invalid", mk("playbooks/a.yml", lines("  vars:\n    scope: Bogus\n")),
			"  ERROR (scope): playbooks/a.yml — invalid vars.scope value: Bogus (must be exactly one of general|gnome|server)", "playbooks/a.yml (invalid vars.scope: Bogus)",
		},
		{"general ok", mk("playbooks/a.yml", lines("  vars:\n    scope: general\n")), "", ""},
		{"gnome guarded", mk("playbooks/a.yml", guardedPlay("gnome")), "", ""},
		{"server guarded", mk("playbooks/a.yml", guardedPlay("server")), "", ""},
		{
			"gnome unguarded", mk("playbooks/a.yml", lines("  vars:\n    scope: gnome\n")),
			"  ERROR (scope): playbooks/a.yml — scope=gnome requires the 2-task canonical guard (assert + meta:end_play) as its first two tasks; missing or incorrect", "playbooks/a.yml (scope=gnome missing/incorrect guard)",
		},
		{
			"general with guard", mk("playbooks/a.yml", guardedPlay("general")),
			"  ERROR (scope): playbooks/a.yml — unnecessary scope guard on a general-scope play (guard never fires for general; remove it)", "playbooks/a.yml (unnecessary guard on general play)",
		},
		{
			"multi play",
			playbook{rel: "playbooks/m.yml", lines: lines("- hosts: a\n- hosts: b\n"), hosts: 2},
			"  ERROR (scope): playbooks/m.yml — file contains 2 separate '- hosts:' plays; this gate cannot safely vouch for a multi-play file. Split it (one play per file, matching every other playbook in the repo), then give each its own vars.scope (+ guard if needed).",
			"playbooks/m.yml (multi-play file: 2 plays in one file, not supported)",
		},
		{"archived skipped", mk("playbooks/optional/archived/a.yml", lines("  vars:\n    x: 1\n")), "", ""},
	}
	for _, c := range cases {
		result := scope([]playbook{c.pb}, root)
		var wantOut, wantRec []string
		if c.wantOut != "" {
			wantOut, wantRec = []string{c.wantOut}, []string{c.wantRec}
		}
		if !slices.Equal(result.out, wantOut) || !slices.Equal(entryStrings(result.entries), wantRec) {
			t.Errorf("%s:\n out %q\n rec %q", c.label, result.out, entryStrings(result.entries))
		}
	}
}

func TestScopeArchivedTestIsOnTheWholeAbsolutePath(t *testing.T) {
	pb := playbook{rel: "playbooks/a.yml", lines: []string{"- hosts: all"}, hosts: 1}
	archived := scope([]playbook{pb}, "/x/optional/archived/checkout")
	if len(archived.out) != 0 || len(archived.entries) != 0 {
		t.Fatalf("a root under optional/archived was checked: %q", archived.out)
	}
	plain := scope([]playbook{pb}, "/x/checkout")
	if len(plain.out) == 0 || len(plain.entries) == 0 {
		t.Fatal("a plain root was skipped")
	}
}

func TestHostsLinePattern(t *testing.T) {
	cases := map[string]bool{
		"- hosts: all":   true,
		"  - hosts: all": true,
		"\t-\thosts:":    true,
		"-hosts: x":      false,
		"- hostsx: y":    false,
		"k: hosts: x":    false,
		"# - hosts: x":   false,
		"- name: x":      false,
	}
	for text, want := range cases {
		if got := hostsLine.MatchString(text); got != want {
			t.Errorf("hostsLine(%q) = %v, want %v", text, got, want)
		}
	}
	if !strings.Contains(playbookShebang, "run.bash") {
		t.Fatal("the shebang constant lost its text")
	}
}
