package ansible

import (
	"bytes"
	"errors"
	"io"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The changed-when-on-success rule (CLAUDE/QA.md, "changed-when-on-success"). A changed_when
// that is true on every run in which its task succeeded reads as a decision but is the constant
// true: the task reports changed whether it changed anything or not, a second run of a play with
// nothing to do is never clean, and the recap hides the run that did change something. The
// claude-devtools image build said git_clone.changed or image_build.rc == 0 (issue 282).
//
// The rule reports a task of any module, in a play's pre_tasks, tasks and post_tasks (blocks,
// rescues and always opened) or in a task list outside a play, whose changed_when is true on
// every successful run: the plain constant; its own result's rc == 0, not failed, is not
// failed, is succeeded (success, successful) or failed == false, while failure is left to the
// default (no failed_when, or one that restates rc != 0 or failed); its own rc is defined
// (a command module's rc is set on every run); is not skipped (changed_when is read only on a
// run); a template of one; an or with one as a side; an and, or a list, of nothing else. Its
// own result is the task's register. Not reported: a command task whose changed_when is the
// plain constant (command-always-changed reports it); a command task with creates or removes,
// which runs only when there is work; a handler, which runs only after a task reported a change;
// and any other expression, including one keyed on another task's result.

// ruleChangedOnSuccess is the rule ID, printed with every finding and the anchor of its section.
const ruleChangedOnSuccess = "changed-when-on-success"

// changedWhenOnSuccess returns the rule's findings of one YAML file, "<rel>:<line>: <rule>:
// <why>", in document order. A file the YAML library cannot parse is a finding.
func changedWhenOnSuccess(rel string, data []byte) []string {
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
			return append(findings, rel+":"+line+": cannot be parsed as YAML, so its changed_when cannot be judged: "+err.Error())
		}
		for _, top := range doc.Content {
			for _, t := range runTasks(top) {
				if changedOnSuccess(t) {
					findings = append(findings, rel+":"+t.line()+": "+ruleChangedOnSuccess+": this changed_when is true on "+
						"every run the task succeeds, so the task reports changed whether it changed anything or not; decide "+
						"changed from what the task observed (its output, or a check made before it), or changed_when: false "+
						"if it changes nothing")
				}
			}
		}
	}
}

// changedOnSuccess is whether a task's changed_when is true on every run in which it succeeded.
func changedOnSuccess(t uxTask) bool {
	cw := mappingValue(t.node, "changed_when")
	if cw == nil || t.module == "" {
		return false
	}
	command := unseenChangeModules[t.module]
	if command && (constantTrue(cw) || skipsWhenDone(t)) {
		return false
	}
	reg := scalarValue(t.node, "register")
	defaultFailure := failsByDefault(mappingValue(t.node, "failed_when"), reg)
	conditions := []*yaml.Node{cw}
	if cw.Kind == yaml.SequenceNode {
		conditions = cw.Content
	}
	for _, c := range conditions {
		if c.Kind != yaml.ScalarNode {
			return false
		}
		if value, plain := boolOf(c); plain {
			if !value {
				return false
			}
			continue
		}
		if !trueOnSuccess(c.Value, reg, command, defaultFailure) {
			return false
		}
	}
	return len(conditions) > 0
}

// failsByDefault is whether a task fails exactly when Ansible's default says so: no
// failed_when, or one that restates the default for its own result.
func failsByDefault(fw *yaml.Node, reg string) bool {
	if fw == nil {
		return true
	}
	if fw.Kind == yaml.SequenceNode && len(fw.Content) == 1 {
		fw = fw.Content[0]
	}
	if fw.Kind != yaml.ScalarNode || reg == "" {
		return false
	}
	r := regexp.QuoteMeta(reg)
	restated := regexp.MustCompile(`^(?:` + r + rcRef + `\s*(?:!=|>)\s*0|` + r + `\s+is\s+failed|` + r + `\.failed)$`)
	return restated.MatchString(normaliseCondition(fw.Value))
}

// jinjaTrue is Jinja's literal true; yes and on are names there, not booleans.
var jinjaTrue = regexp.MustCompile(`^[Tt]rue$`)

// rcRef is a result's rc read by attribute or by subscript.
const rcRef = `(?:\.rc|\[\s*['"]rc['"]\s*\])`

// normaliseCondition is a condition with its template braces removed, its whitespace collapsed
// and its outer parentheses stripped.
func normaliseCondition(expr string) string {
	s := strings.Join(strings.Fields(expr), " ")
	if m := templatedConstant.FindStringSubmatch(s); m != nil {
		s = m[1]
	} else if strings.HasPrefix(s, "{{") && strings.HasSuffix(s, "}}") {
		s = strings.TrimSpace(s[2 : len(s)-2])
	}
	for strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") && closesAtEnd(s) {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}

// closesAtEnd is whether the parenthesis opening s is the one closing it.
func closesAtEnd(s string) bool {
	depth := 0
	var quote rune
	for i, c := range s {
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				return i == len(s)-1
			}
		}
	}
	return false
}

// splitTop splits a condition on a Jinja keyword (or, and) where it stands outside brackets
// and quotes.
func splitTop(s, keyword string) []string {
	var parts []string
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			depth--
		case depth == 0 && c == ' ' && strings.HasPrefix(s[i+1:], keyword+" "):
			parts = append(parts, s[start:i])
			start = i + len(keyword) + 2
			i = start - 1
		}
	}
	return append(parts, s[start:])
}

// trueOnSuccess is whether a changed_when condition is true on every run in which the task
// succeeded. reg is the task's register, rcModule whether its module sets rc on every run,
// and defaultFailure whether it fails exactly when its module failed.
func trueOnSuccess(expr, reg string, rcModule, defaultFailure bool) bool {
	s := normaliseCondition(expr)
	if ors := splitTop(s, "or"); len(ors) > 1 {
		for _, side := range ors {
			if trueOnSuccess(side, reg, rcModule, defaultFailure) {
				return true
			}
		}
		return false
	}
	if ands := splitTop(s, "and"); len(ands) > 1 {
		for _, side := range ands {
			if !trueOnSuccess(side, reg, rcModule, defaultFailure) {
				return false
			}
		}
		return true
	}
	return atomTrueOnSuccess(s, reg, rcModule, defaultFailure)
}

// atomTrueOnSuccess is trueOnSuccess of a normalised condition with no top-level "or" nor "and".
func atomTrueOnSuccess(s, reg string, rcModule, defaultFailure bool) bool {
	if jinjaTrue.MatchString(s) {
		return true
	}
	if reg == "" {
		return false
	}
	r := regexp.QuoteMeta(reg)
	always := `^(?:` + r + `\s+is\s+not\s+skipped|not\s+` + r + `\.skipped)$`
	if regexp.MustCompile(always).MatchString(s) {
		return true
	}
	if rcModule && regexp.MustCompile(`^`+r+rcRef+`\s+is\s+defined$`).MatchString(s) {
		return true
	}
	if !defaultFailure {
		return false
	}
	succeeded := `^(?:not\s+` + r + `\.failed|not\s+\(?\s*` + r + `\s+is\s+failed\s*\)?|` + r + `\s+is\s+not\s+failed|` +
		r + `\s+is\s+(?:succeeded|success|successful)|` + r + `\.failed\s*==\s*(?:false|False))$`
	if regexp.MustCompile(succeeded).MatchString(s) {
		return true
	}
	return rcModule && regexp.MustCompile(`^(?:`+r+rcRef+`\s*==\s*0|0\s*==\s*`+r+rcRef+`)$`).MatchString(s)
}
