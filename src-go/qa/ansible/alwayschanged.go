package ansible

import (
	"bytes"
	"errors"
	"io"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The command-always-changed rule (CLAUDE/QA.md, "command-always-changed"). Ansible cannot know
// what a command did, so a command, shell, raw, script or expect task reports changed on every
// run it makes unless changed_when says otherwise. Such a task makes a second run of a play,
// with nothing to do, report changes, and a recap that is never clean hides the run that did
// change something.
//
// The rule reports a task of one of those modules whose changed_when is absent or the plain
// boolean true (true, yes, on, quoted or not), in a play's pre_tasks, tasks and post_tasks
// (blocks, rescues and always opened) or in a task list outside a play. A when on the task does
// not excuse it: a guard says when the command runs, not whether that run changed anything,
// and one of the three play-ftp-camera.yml tasks that showed the hazard (issue 224) was
// guarded. Not reported: a task whose changed_when is anything else (false, an expression, a
// list of conditions, a template), which is a decision the author made from what was observed;
// a task with creates or removes, which Ansible skips without a change once that path says the
// work is done; and a handler, which runs only after a task reported a change.

// ruleAlwaysChanged is the rule ID, printed with every finding and the anchor of its section.
const ruleAlwaysChanged = "command-always-changed"

// unseenChangeModules are the modules whose change Ansible cannot see.
var unseenChangeModules = map[string]bool{modCommand: true, modShell: true, modRaw: true, "script": true, "expect": true}

// freeFormSkip is a creates= or removes= in a free-form command line.
var freeFormSkip = regexp.MustCompile(`(?:^|\s)(?:creates|removes)=`)

// commandAlwaysChanged returns the rule's findings of one YAML file, "<rel>:<line>: <rule>:
// <why>", in document order. A file the YAML library cannot parse is a finding.
func commandAlwaysChanged(rel string, data []byte) []string {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var findings []string
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return findings
		}
		if err != nil {
			line := "1"
			if m := yamlErrorLine.FindStringSubmatch(err.Error()); m != nil {
				line = m[1]
			}
			return append(findings, rel+":"+line+": cannot be parsed as YAML, so its command tasks cannot be judged: "+err.Error())
		}
		for _, top := range doc.Content {
			for _, t := range runTasks(top) {
				if alwaysChanged(t) {
					findings = append(findings, rel+":"+t.line()+": "+ruleAlwaysChanged+": this command task reports "+
						"changed on every run it makes, whether it changed anything or not; set changed_when from what "+
						"the command printed or a check made before it, or changed_when: false if it changes nothing")
				}
			}
		}
	}
}

// runTasks is the tasks of a document's top-level list that run unasked: every play's
// pre_tasks, tasks and post_tasks, and a task list's own tasks. Handlers are left out.
func runTasks(top *yaml.Node) []uxTask {
	if top.Kind != yaml.SequenceNode {
		return nil
	}
	var tasks []uxTask
	for _, item := range top.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if mappingValue(item, "hosts") == nil {
			if mappingValue(item, "import_playbook") == nil {
				flatten(&yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{item}}, false, &tasks)
			}
			continue
		}
		for _, key := range []string{keyPreTasks, tasksKey, keyPostTasks} {
			flatten(mappingValue(item, key), false, &tasks)
		}
	}
	return tasks
}

// alwaysChanged is whether a task is a command whose changed result nothing decides.
func alwaysChanged(t uxTask) bool {
	if !unseenChangeModules[t.module] {
		return false
	}
	if cw := mappingValue(t.node, "changed_when"); cw != nil && !constantTrue(cw) {
		return false
	}
	return !skipsWhenDone(t)
}

// templatedConstant is a template holding nothing but a constant: "{{ true }}".
var templatedConstant = regexp.MustCompile(`^\{\{\s*(\S+?)\s*\}\}$`)

// constantTrue is whether a changed_when is the constant true: the plain boolean in any
// spelling, a template of nothing else, or a list whose every condition is one.
func constantTrue(v *yaml.Node) bool {
	switch v.Kind {
	case yaml.ScalarNode:
		if m := templatedConstant.FindStringSubmatch(strings.TrimSpace(v.Value)); m != nil {
			return constantTrue(&yaml.Node{Kind: yaml.ScalarNode, Value: m[1]})
		}
		value, plain := boolOf(v)
		return plain && value
	case yaml.SequenceNode:
		for _, item := range v.Content {
			if !constantTrue(item) {
				return false
			}
		}
		return len(v.Content) > 0
	case yaml.DocumentNode, yaml.MappingNode, yaml.AliasNode:
	}
	return false
}

// skipsWhenDone is whether a command task has creates or removes: in its arguments, in args,
// or in its free-form line.
func skipsWhenDone(t uxTask) bool {
	for _, holder := range []*yaml.Node{t.moduleValue(), mappingValue(t.node, "args")} {
		if holder == nil {
			continue
		}
		switch holder.Kind {
		case yaml.MappingNode:
			if mappingValue(holder, "creates") != nil || mappingValue(holder, "removes") != nil {
				return true
			}
		case yaml.ScalarNode:
			if freeFormSkip.MatchString(holder.Value) {
				return true
			}
		case yaml.DocumentNode, yaml.SequenceNode, yaml.AliasNode:
		}
	}
	return false
}
