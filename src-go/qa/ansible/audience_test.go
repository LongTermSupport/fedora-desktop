package ansible

import (
	"slices"
	"testing"
)

func audienceGuardTask() string {
	return "    - name: " + audienceGuardName + "\n      ansible.builtin.meta: end_play\n      when: " + audienceGuardWhen + "\n"
}

func TestScanAudienceReadsTheKeyAndTheGuard(t *testing.T) {
	text := "- hosts: all\n  vars:\n    scope: general\n    audience: developer   # why\n  tasks:\n" + audienceGuardTask() +
		"    - name: Work\n      ansible.builtin.debug:\n        msg: hi\n"
	got := scanAudience(lines(text))
	want := audienceInfo{count: 1, value: "developer", guards: 1, guardOK: true, guardFirst: true}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

// The guard must be the first task the play RUNS: Ansible runs pre_tasks, then roles, then
// tasks, whatever order the file lists them in; only the scope guard may precede it in tasks.
func TestScanAudienceGuardFirst(t *testing.T) {
	scopeGuard := "    - name: " + guardAssertName + "\n      ansible.builtin.assert:\n        that: true\n" +
		"    - name: " + guardEndName + "\n      ansible.builtin.meta: end_play\n"
	work := "    - name: Work\n      ansible.builtin.debug:\n        msg: hi\n"
	cases := []struct {
		label string
		text  string
		want  bool
	}{
		{"first in tasks", "  tasks:\n" + audienceGuardTask() + work, true},
		{"after the scope guard", "  tasks:\n" + scopeGuard + audienceGuardTask() + work, true},
		{"first in pre_tasks", "  pre_tasks:\n" + audienceGuardTask() + work + "  tasks:\n" + work, true},
		{"after a task", "  tasks:\n" + work + audienceGuardTask(), false},
		{"after an unnamed task", "  tasks:\n    - ansible.builtin.debug:\n        msg: hi\n" + audienceGuardTask(), false},
		{"in tasks, after a pre_task", "  pre_tasks:\n" + work + "  tasks:\n" + audienceGuardTask(), false},
		{"in tasks, pre_tasks listed later", "  tasks:\n" + audienceGuardTask() + "  pre_tasks:\n" + work, false},
		{"in pre_tasks, after a pre_task", "  pre_tasks:\n" + work + audienceGuardTask(), false},
		{"in tasks, after roles", "  roles:\n    - some_role\n  tasks:\n" + audienceGuardTask(), false},
		{"handlers do not run first", "  tasks:\n" + audienceGuardTask() + "  handlers:\n" + work, true},
		{"vars list items are not tasks", "  vars:\n    audience: developer\n    pkgs:\n    - a\n  tasks:\n" + audienceGuardTask(), true},
	}
	for _, c := range cases {
		if got := scanAudience(lines(c.text)).guardFirst; got != c.want {
			t.Errorf("%s: guardFirst %v want %v", c.label, got, c.want)
		}
	}
}

func TestScanAudienceRules(t *testing.T) {
	cases := []struct {
		label string
		text  string
		want  audienceInfo
	}{
		{"no vars", "- hosts: all\n  tasks:\n    - name: A\n", audienceInfo{}},
		{"empty value is not counted", "  vars:\n    audience:\n", audienceInfo{}},
		{"two entries, last wins", "  vars:\n    audience: all\n    audience: developer\n", audienceInfo{count: 2, value: "developer"}},
		{"wrong indent", "  vars:\n      audience: all\n", audienceInfo{}},
		{"vars ended by a 2-space key", "  vars:\n    a: 1\n  tasks:\n    audience: all\n", audienceInfo{}},
		{"guard in pre_tasks counts", "  pre_tasks:\n" + audienceGuardTask(), audienceInfo{guards: 1, guardOK: true, guardFirst: true}},
		{
			"guard with the wrong condition", "  tasks:\n    - name: " + audienceGuardName + "\n      ansible.builtin.meta: end_play\n      when: true\n",
			audienceInfo{guards: 1, guardFirst: true},
		},
		{
			"guard that is not end_play", "  tasks:\n    - name: " + audienceGuardName + "\n      ansible.builtin.meta: noop\n      when: " + audienceGuardWhen + "\n",
			audienceInfo{guards: 1, guardFirst: true},
		},
		{
			"the next task's fields are not the guard's", "  tasks:\n    - name: " + audienceGuardName + "\n    - name: B\n      ansible.builtin.meta: end_play\n      when: " + audienceGuardWhen + "\n",
			audienceInfo{guards: 1, guardFirst: true},
		},
		{"two guards", "  tasks:\n" + audienceGuardTask() + audienceGuardTask(), audienceInfo{guards: 2, guardOK: true, guardFirst: true}},
	}
	for _, c := range cases {
		if got := scanAudience(lines(c.text)); got != c.want {
			t.Errorf("%s: got %+v want %+v", c.label, got, c.want)
		}
	}
}

func TestAudienceChecksEachRule(t *testing.T) {
	const root = "/r"
	mk := func(rel, body string) playbook {
		return playbook{rel: rel, lines: lines("- hosts: all\n" + body), hosts: 1}
	}
	cases := []struct {
		label   string
		pb      playbook
		wantOut string
		wantRec string
	}{
		{
			"missing", mk("playbooks/a.yml", "  vars:\n    scope: general\n"),
			"  ERROR (audience): playbooks/a.yml — missing vars.audience (need exactly one of all|developer, CLAUDE/AnsibleStyle.md)", "playbooks/a.yml (missing vars.audience)",
		},
		{
			"multiple", mk("playbooks/a.yml", "  vars:\n    audience: all\n    audience: all\n"),
			"  ERROR (audience): playbooks/a.yml — multiple vars.audience entries declared (exactly one required)", "playbooks/a.yml (multiple vars.audience entries)",
		},
		{
			"invalid", mk("playbooks/a.yml", "  vars:\n    audience: consumer\n"),
			"  ERROR (audience): playbooks/a.yml — invalid vars.audience value: consumer (must be exactly one of all|developer)", "playbooks/a.yml (invalid vars.audience: consumer)",
		},
		{"all ok", mk("playbooks/a.yml", "  vars:\n    audience: all\n"), "", ""},
		{"developer guarded", mk("playbooks/a.yml", "  vars:\n    audience: developer\n  tasks:\n"+audienceGuardTask()), "", ""},
		{
			"developer unguarded", mk("playbooks/a.yml", "  vars:\n    audience: developer\n"),
			"  ERROR (audience): playbooks/a.yml — audience=developer requires exactly one audience guard task (meta: end_play when: " + audienceGuardWhen + ")", "playbooks/a.yml (audience=developer missing/incorrect guard)",
		},
		{
			"developer guard wrong", mk("playbooks/a.yml", "  vars:\n    audience: developer\n  tasks:\n    - name: "+audienceGuardName+"\n      ansible.builtin.meta: end_play\n"),
			"  ERROR (audience): playbooks/a.yml — audience=developer requires exactly one audience guard task (meta: end_play when: " + audienceGuardWhen + ")", "playbooks/a.yml (audience=developer missing/incorrect guard)",
		},
		{
			"developer guard not first", mk("playbooks/a.yml", "  vars:\n    audience: developer\n  tasks:\n    - name: Work\n      ansible.builtin.debug:\n        msg: hi\n"+audienceGuardTask()),
			"  ERROR (audience): playbooks/a.yml — the audience guard must be the first task the play runs (only the scope guard may precede it, CLAUDE/AnsibleStyle.md)", "playbooks/a.yml (audience guard not the first task)",
		},
		{
			"all with guard", mk("playbooks/a.yml", "  vars:\n    audience: all\n  tasks:\n"+audienceGuardTask()),
			"  ERROR (audience): playbooks/a.yml — unnecessary audience guard on an audience=all play (remove it)", "playbooks/a.yml (unnecessary guard on audience=all play)",
		},
		{"multi play left to the scope check", playbook{rel: "playbooks/m.yml", lines: lines("- hosts: a\n- hosts: b\n"), hosts: 2}, "", ""},
		{"archived skipped", mk("playbooks/optional/archived/a.yml", "  vars:\n    x: 1\n"), "", ""},
	}
	for _, c := range cases {
		result := audience([]playbook{c.pb}, root)
		var wantOut, wantRec []string
		if c.wantOut != "" {
			wantOut, wantRec = []string{c.wantOut}, []string{c.wantRec}
		}
		if !slices.Equal(result.out, wantOut) || !slices.Equal(entryStrings(result.entries), wantRec) {
			t.Errorf("%s:\n out %q\n rec %q", c.label, result.out, entryStrings(result.entries))
		}
	}
}
