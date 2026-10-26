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
	want := audienceInfo{count: 1, value: "developer", guards: 1, guardOK: true}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
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
		{"guard in pre_tasks counts", "  pre_tasks:\n" + audienceGuardTask(), audienceInfo{guards: 1, guardOK: true}},
		{
			"guard with the wrong condition", "  tasks:\n    - name: " + audienceGuardName + "\n      ansible.builtin.meta: end_play\n      when: true\n",
			audienceInfo{guards: 1},
		},
		{
			"guard that is not end_play", "  tasks:\n    - name: " + audienceGuardName + "\n      ansible.builtin.meta: noop\n      when: " + audienceGuardWhen + "\n",
			audienceInfo{guards: 1},
		},
		{
			"the next task's fields are not the guard's", "  tasks:\n    - name: " + audienceGuardName + "\n    - name: B\n      ansible.builtin.meta: end_play\n      when: " + audienceGuardWhen + "\n",
			audienceInfo{guards: 1},
		},
		{"two guards", "  tasks:\n" + audienceGuardTask() + audienceGuardTask(), audienceInfo{guards: 2, guardOK: true}},
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
