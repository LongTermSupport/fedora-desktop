package installplay

import (
	"slices"
	"sort"
	"testing"

	"go.yaml.in/yaml/v3"
)

// guardTasks are the play's checks of its inputs and of the host: under --tags X Ansible skips
// every task without X or always, so each of these is tagged always, or a tagged run would
// change a host the play refuses.
var guardTasks = []string{
	"Check the Play's Inputs",
	"Refuse a Scope or an Approval Naming a Team This Run Does Not Apply",
	"Check That Firewalld Is Already Running",
	"Refuse a Host Whose Firewalld Is Not Running",
}

// tagFindings judges a task list for a run by tag (issue 244): every task but a block has a tag of
// its own or one its block gives it, each guard named is tagged always, and no task is a dynamic include,
// whose tag reaches the include and not the tasks it reads.
func tagFindings(tasks []map[string]any, guards []string) []string {
	var out []string
	var walk func(tasks []map[string]any, inherited []string)
	walk = func(tasks []map[string]any, inherited []string) {
		for _, task := range tasks {
			tags := append(slices.Clone(inherited), stringsOf(task["tags"])...)
			name := taskName(task)
			for _, key := range []string{"include_tasks", "ansible.builtin.include_tasks"} {
				if _, found := task[key]; found {
					out = append(out, name+" is a dynamic include")
				}
			}
			// A block runs no module of its own: it is judged by its children.
			if _, isBlock := task["block"]; len(tags) == 0 && !isBlock {
				out = append(out, name+" has no tag")
			}
			if n, ok := task["name"].(string); ok && slices.Contains(guards, n) && !slices.Contains(tags, "always") {
				out = append(out, name+" is a guard not tagged always")
			}
			for _, section := range []string{"block", "rescue", "always"} {
				walk(asTasks(task[section]), tags)
			}
		}
	}
	walk(tasks, nil)
	return out
}

func stringsOf(v any) []string {
	var out []string
	switch value := v.(type) {
	case string:
		out = append(out, value)
	case []any:
		for _, item := range value {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// The check can fail: an untagged task, an untagged child of an untagged block, a guard tagged
// only with what it guards and a dynamic include are each caught; a child that takes its block's
// tag, and an untagged block itself, are not.
func TestTheTagCheckDiscriminates(t *testing.T) {
	fixture := `
- name: untagged
  ansible.builtin.debug: {msg: x}
- name: guard
  ansible.builtin.assert: {that: [true]}
  tags: [units]
- name: tagged block
  tags: [teams]
  block:
    - name: inherits
      ansible.builtin.debug: {msg: x}
- name: untagged block
  block:
    - name: orphan
      ansible.builtin.debug: {msg: x}
      when: true
- name: include
  ansible.builtin.include_tasks: x.yml
  tags: [admin]
`
	var tasks []map[string]any
	if err := yaml.Unmarshal([]byte(fixture), &tasks); err != nil {
		t.Fatal(err)
	}
	got := tagFindings(tasks, []string{"guard"})
	want := []string{
		"task guard is a guard not tagged always",
		"task include is a dynamic include",
		"task orphan has no tag",
		"task untagged has no tag",
	}
	sort.Strings(got)
	if !slices.Equal(got, want) {
		t.Errorf("tagFindings = %q, want %q", got, want)
	}
}

// Every task of the play can be run by tag, and a tagged run still meets every guard.
func TestEveryTaskIsTaggedAndEveryGuardRunsAlways(t *testing.T) {
	tasks := loadPlay(t).Tasks
	names := map[string]bool{}
	var collect func([]map[string]any)
	collect = func(tasks []map[string]any) {
		for _, task := range tasks {
			if n, ok := task["name"].(string); ok {
				names[n] = true
			}
			for _, section := range []string{"block", "rescue", "always"} {
				collect(asTasks(task[section]))
			}
		}
	}
	collect(tasks)
	for _, guard := range guardTasks {
		if !names[guard] {
			t.Errorf("the play has no task %q; update guardTasks", guard)
		}
	}
	if bad := tagFindings(tasks, guardTasks); len(bad) > 0 {
		t.Errorf("%q", bad)
	}
}
