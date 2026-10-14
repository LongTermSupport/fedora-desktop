package ansible

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

const (
	guardBlock = "  tasks:\n    - name: " + guardAssertName + "\n      ansible.builtin.assert:\n        that: true\n" +
		"    - name: " + guardEndName + "\n      ansible.builtin.meta: end_play\n      when: " + guardEndWhen + "\n"
	plainTasks = "  tasks:\n    - name: Work\n      ansible.builtin.debug:\n        msg: hi\n"
)

// play is a playbook text; scope "" leaves the scope line out.
func play(scope string, guard bool) string {
	text := playbookShebang + "\n---\n- hosts: all\n  name: P\n  vars:\n"
	if scope != "" {
		text += "    scope: " + scope + "\n"
	}
	if guard {
		return text + guardBlock
	}
	return text + plainTasks
}

type scenario struct {
	name  string
	files map[string]string
	// notExec lists the playbooks left without the execute bit; every other playbook has it.
	notExec []string
	code    ExitCode
	// want is the violation lines (those starting with two spaces and ERROR, or four spaces), in order.
	want []string
}

func baseTree() map[string]string {
	return map[string]string{
		"playbooks/play-a.yml": play("general", false),
		"playbooks/play-b.yml": play("gnome", true),
		"tasks/t.yml":          "k: v\n",
	}
}

func with(extra map[string]string) map[string]string {
	files := baseTree()
	maps.Copy(files, extra)
	return files
}

func violationLines(out string) []string {
	var lines []string
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "  ERROR") || strings.HasPrefix(line, "    ") {
			lines = append(lines, line)
		}
	}
	return lines
}

func scenarios() []scenario {
	return slices.Concat(failFastScenarios(), fileAndQuoteScenarios(), scopeAndFactScenarios())
}

func failFastScenarios() []scenario {
	ff := "      failed_when: false\n"
	return []scenario{
		{name: "clean", files: baseTree(), code: ExitOK},
		// Check 1: fail-fast.
		{
			name: "ff in tasks", files: with(map[string]string{"tasks/x.yml": ff}), code: ExitFailed,
			want: []string{"  ERROR (fail-fast): tasks/x.yml:1:      failed_when: false"},
		},
		{
			name: "ff in yaml extension, vars, environment, roles", code: ExitFailed,
			files: with(map[string]string{"vars/x.yaml": ff, "environment/e.yml": ff, "roles/r/t.yml": ff}),
			want: []string{
				"  ERROR (fail-fast): vars/x.yaml:1:      failed_when: false",
				"  ERROR (fail-fast): environment/e.yml:1:      failed_when: false",
				"  ERROR (fail-fast): roles/r/t.yml:1:      failed_when: false",
			},
		},
		{
			name: "ff annotated, commented, in roles/vendor and in a non-yaml file are fine", code: ExitOK,
			files: with(map[string]string{
				"tasks/a.yml":              "      failed_when: false # FAIL-FAST-OK: probe\n",
				"tasks/b.yml":              "      # failed_when: false was removed\n",
				"roles/vendor/v/t.yml":     ff,
				"tasks/c.txt":              ff,
				"tasks/d.yml":              "      failed_when: not x.stat.exists\n",
				"tasks/e.yml":              "      ignore_errors: false\n",
				"tasks/f.yml":              "      changed_when: false\n",
				"tasks/g.yml":              "      ignore_errors: yes # FAIL-FAST-OK: r\n",
				"tasks/h.yml":              "      ignore_unreachable: \"{{ x }}\" # fail-fast-ok: r\n",
				"playbooks/optional/n.yml": "k: v\n",
			}),
		},
	}
}

func fileAndQuoteScenarios() []scenario {
	ff := "      failed_when: false\n"
	return []scenario{
		{
			name: "ff with a quoted value is still found", code: ExitFailed,
			files: with(map[string]string{"tasks/q.yml": "  ignore_errors: \"true\"\n  ignore_errors: 'yes'\n" +
				"  failed_when: \"false\"\n  ignore_unreachable: 'on'\n  ignore_errors: '{{ maybe }}'\n"}),
			want: []string{
				"  ERROR (fail-fast): tasks/q.yml:1:  ignore_errors: \"true\"",
				"  ERROR (fail-fast): tasks/q.yml:2:  ignore_errors: 'yes'",
				"  ERROR (fail-fast): tasks/q.yml:3:  failed_when: \"false\"",
				"  ERROR (fail-fast): tasks/q.yml:4:  ignore_unreachable: 'on'",
				"  ERROR (fail-fast): tasks/q.yml:5:  ignore_errors: '{{ maybe }}'",
			},
		},
		{
			name: "ff under any other vendor directory is still checked", code: ExitFailed,
			files: with(map[string]string{"tasks/vendor/x.yml": ff}),
			want:  []string{"  ERROR (fail-fast): tasks/vendor/x.yml:1:      failed_when: false"},
		},
		{
			name: "ff justified by the path name is not justified", code: ExitFailed,
			files: with(map[string]string{"tasks/fail-fast-ok.yml": ff}),
			want:  []string{"  ERROR (fail-fast): tasks/fail-fast-ok.yml:1:      failed_when: false"},
		},
		{
			name: "ff in a path with a colon is judged on its text", code: ExitFailed,
			files: with(map[string]string{"tasks/a:b.yml": "      failed_when: false # still a violation\n"}),
			want:  []string{"  ERROR (fail-fast): tasks/a:b.yml:1:      failed_when: false # still a violation"},
		},
		{
			name: "ff in a file with a NUL byte is found", code: ExitFailed,
			files: with(map[string]string{"tasks/bin.yml": ff + "\x00\n"}),
			want:  []string{"  ERROR (fail-fast): tasks/bin.yml:1:      failed_when: false"},
		},
		{
			name: "ff with carriage returns", code: ExitFailed,
			files: with(map[string]string{"tasks/x.yml": "      ignore_errors: yes\r\n"}),
			want:  []string{"  ERROR (fail-fast): tasks/x.yml:1:      ignore_errors: yes\r"},
		},
		// Check 2: hygiene.
		{
			name: "hygiene: no shebang", code: ExitFailed,
			files: with(map[string]string{"playbooks/x.yml": strings.Replace(play("general", false), playbookShebang+"\n", "", 1)}),
			want:  []string{"  ERROR (hygiene): playbooks/x.yml — missing shebang"},
		},
		{
			name: "hygiene: not executable", code: ExitFailed, notExec: []string{"playbooks/x.yml"},
			files: with(map[string]string{"playbooks/x.yml": play("general", false)}),
			want:  []string{"  ERROR (hygiene): playbooks/x.yml — not executable"},
		},
		{
			name: "hygiene: both", code: ExitFailed, notExec: []string{"playbooks/x.yml"},
			files: with(map[string]string{"playbooks/x.yml": strings.Replace(play("general", false), playbookShebang+"\n", "", 1)}),
			want:  []string{"  ERROR (hygiene): playbooks/x.yml — missing shebang + not executable"},
		},
		{
			name: "hygiene: a yaml file with no play is not a playbook", code: ExitOK,
			files: with(map[string]string{"playbooks/notes.yml": "- name: n\n"}),
		},
	}
}

func scopeAndFactScenarios() []scenario {
	ff := "      failed_when: false\n"
	return []scenario{
		// Check 3: self-default.
		{
			name: "self-default", code: ExitFailed,
			files: with(map[string]string{"vars/x.yml": "foo: \"{{ foo | default(1) }}\"\nbar: \"{{ foo | default(1) }}\"\n"}),
			want:  []string{"  ERROR (self-ref var): vars/x.yml:1:foo: \"{{ foo | default(1) }}\""},
		},
		// Check 4: scope and guard.
		{
			name: "scope missing", code: ExitFailed,
			files: with(map[string]string{"playbooks/x.yml": play("", false)}),
			want:  []string{"  ERROR (scope): playbooks/x.yml — missing vars.scope (need exactly one of general|gnome|server)"},
		},
		{
			name: "scope invalid", code: ExitFailed,
			files: with(map[string]string{"playbooks/x.yml": play("bogus", false)}),
			want:  []string{"  ERROR (scope): playbooks/x.yml — invalid vars.scope value: bogus (must be exactly one of general|gnome|server)"},
		},
		{
			name: "scope gnome without guard", code: ExitFailed,
			files: with(map[string]string{"playbooks/x.yml": play("gnome", false)}),
			want:  []string{"  ERROR (scope): playbooks/x.yml — scope=gnome requires the 2-task canonical guard (assert + meta:end_play) as its first two tasks; missing or incorrect"},
		},
		{
			name: "scope server with guard is fine", code: ExitOK,
			files: with(map[string]string{"playbooks/x.yml": play("server", true)}),
		},
		{
			name: "scope general with guard", code: ExitFailed,
			files: with(map[string]string{"playbooks/x.yml": play("general", true)}),
			want:  []string{"  ERROR (scope): playbooks/x.yml — unnecessary scope guard on a general-scope play (guard never fires for general; remove it)"},
		},
		{
			name: "scope in archived plays is not checked but hygiene is", code: ExitFailed, notExec: []string{"playbooks/optional/archived/x.yml"},
			files: with(map[string]string{"playbooks/optional/archived/x.yml": play("", false)}),
			want:  []string{"  ERROR (hygiene): playbooks/optional/archived/x.yml — not executable"},
		},
		// Check 5: deprecated fact variables.
		{
			name: "fact var", code: ExitFailed,
			files: with(map[string]string{"tasks/x.yml": "a: ansible_env\nb: ansible_facts['env']\n# ansible_fqdn\nc: getent_passwd\nd: ansible_facts['getent_passwd']\n"}),
			want: []string{
				"  ERROR (deprecated fact var): tasks/x.yml:1:a: ansible_env", "    Use ansible_facts['<name>'] — top-level injection is removed in ansible-core 2.24.",
				"  ERROR (deprecated fact var): tasks/x.yml:4:c: getent_passwd", "    Use ansible_facts['<name>'] — top-level injection is removed in ansible-core 2.24.",
			},
		},
		// Check 6: guessed uid.
		{
			name: "guessed uid", code: ExitFailed,
			files: with(map[string]string{"tasks/x.yml": "a: /run/user/1000/bus\nb: /run/user/{{ ansible_facts['getent_passwd'][u][1] }}\n# /run/user/1000\nc: /run/user/{{ uid }}\n"}),
			want: []string{
				"  ERROR (guessed uid in a runtime path): tasks/x.yml:1:a: /run/user/1000/bus", "    Resolve it: ansible.builtin.getent + ansible_facts['getent_passwd'][user_login][1].",
				"  ERROR (guessed uid in a runtime path): tasks/x.yml:4:c: /run/user/{{ uid }}", "    Resolve it: ansible.builtin.getent + ansible_facts['getent_passwd'][user_login][1].",
			},
		},
		{
			name: "every check once, in check order", code: ExitFailed, notExec: []string{"playbooks/hy.yml"},
			files: with(map[string]string{
				"tasks/ff.yml": ff, "vars/sr.yml": "foo: \"{{ foo | default(1) }}\"\n",
				"environment/fv.yml": "x: ansible_env\n", "roles/r/rd.yml": "p: /run/user/1000\n",
				"playbooks/hy.yml": play("general", false), "playbooks/sc.yml": play("", false),
			}),
			want: []string{
				"  ERROR (fail-fast): tasks/ff.yml:1:      failed_when: false",
				"  ERROR (hygiene): playbooks/hy.yml — not executable",
				"  ERROR (self-ref var): vars/sr.yml:1:foo: \"{{ foo | default(1) }}\"",
				"  ERROR (scope): playbooks/sc.yml — missing vars.scope (need exactly one of general|gnome|server)",
				"  ERROR (deprecated fact var): environment/fv.yml:1:x: ansible_env", "    Use ansible_facts['<name>'] — top-level injection is removed in ansible-core 2.24.",
				"  ERROR (guessed uid in a runtime path): roles/r/rd.yml:1:p: /run/user/1000", "    Resolve it: ansible.builtin.getent + ansible_facts['getent_passwd'][user_login][1].",
			},
		},
	}
}

func TestScenarios(t *testing.T) {
	for _, sc := range scenarios() {
		t.Run(sc.name, func(t *testing.T) {
			root := writeTree(t, sc.files)
			for rel, text := range sc.files {
				if strings.HasPrefix(rel, "playbooks/") && strings.Contains(text, "- hosts:") && !slices.Contains(sc.notExec, rel) {
					chmodX(t, root, rel)
				}
			}
			res := runIn(t, root)
			if res.code != sc.code {
				t.Fatalf("exit %d, want %d\n%s%s", res.code, sc.code, res.out, res.errOut)
			}
			if got := violationLines(res.out); !slices.Equal(got, sc.want) {
				t.Fatalf("violations\n got %q\nwant %q", got, sc.want)
			}
			if sc.code == ExitOK && !strings.Contains(res.report, `"status": "pass"`) {
				t.Fatalf("report %s", res.report)
			}
		})
	}
}

func TestPassedCountDoesNotGoNegative(t *testing.T) {
	many := strings.Repeat("      failed_when: false\n", 6)
	root := writeTree(t, with(map[string]string{"tasks/many.yml": many}))
	chmodX(t, root, "playbooks/play-a.yml")
	chmodX(t, root, "playbooks/play-b.yml")
	res := runIn(t, root)
	if !strings.Contains(res.report, "\"passed\": 0,") || !strings.Contains(res.report, "\"failed\": 6") {
		t.Fatalf("report %s", res.report)
	}
}
