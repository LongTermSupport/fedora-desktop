package versionpins

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"
)

// target is a file a pin names, read once.
type target struct {
	// state is why the file cannot be searched for the var: empty when it can.
	state string
	text  string
	// docs is the file's YAML documents, parsed on first use; parseErr is why it did not parse.
	docs     []*yaml.Node
	parsed   bool
	parseErr error
}

// Reasons a target cannot be searched.
const (
	targetMissing    = "missing"
	targetNotRegular = "not regular"
	targetBroken     = "broken"
)

// resolve checks that every well-formed pin still resolves to a declared var.
func (r *report) resolve(fsys fs.FS, pins []pin) {
	targets := map[string]*target{}
	for _, p := range pins {
		r.reached++
		where := fmt.Sprintf("%s[%d]", topKey, p.index)
		if !fs.ValidPath(p.playbook) || p.playbook == "." {
			r.findf(p.line, "%s: playbook '%s' must be a repository-relative path (not absolute, no . or .. element, "+
				"no empty element)", where, p.playbook)
			continue
		}
		t, seen := targets[p.playbook]
		if !seen {
			t = r.load(fsys, p.playbook)
			targets[p.playbook] = t
		}
		r.judgePin(p, where, t)
	}
}

// load reads a target file once.
func (r *report) load(fsys fs.FS, name string) *target {
	info, err := fs.Lstat(fsys, name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return &target{state: targetMissing}
	case err != nil:
		r.problemf("cannot read %s: %v", name, err)
		return &target{state: targetBroken}
	case !info.Mode().IsRegular():
		return &target{state: targetNotRegular}
	}
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		r.problemf("cannot read %s: %v", name, err)
		return &target{state: targetBroken}
	}
	return &target{text: string(data)}
}

// judgePin holds one pin against its target.
func (r *report) judgePin(p pin, where string, t *target) {
	switch t.state {
	case targetMissing:
		r.findf(p.line, "%s: names a playbook that does not exist: %s", where, p.playbook)
		return
	case targetNotRegular:
		r.findf(p.line, "%s: %s is not a regular file (a directory or a symlink); a link's target can lie outside the "+
			"repository", where, p.playbook)
		return
	case targetBroken:
		return
	}
	value, declared := declaration(t.text, p.name)
	switch {
	case !declared:
		r.findf(p.line, "%s: names '%s', which %s no longer declares", where, p.name, p.playbook)
		return
	case value == "":
		r.findf(p.line, "%s: declares '%s' with no value in %s", where, p.name, p.playbook)
		return
	}
	if !strings.HasSuffix(p.playbook, ".yml") && !strings.HasSuffix(p.playbook, ".yaml") {
		return
	}
	if !t.parsed {
		t.docs, t.parseErr = decodeAll([]byte(t.text))
		t.parsed = true
		if t.parseErr != nil {
			r.problemf("%s is not valid YAML: %v", p.playbook, t.parseErr)
		}
	}
	if t.parseErr != nil {
		return
	}
	if !hasScalarKey(t.docs, p.name) {
		r.findf(p.line, "%s: %s is not a key with a scalar value in %s", where, p.name, p.playbook)
	}
}

// declaration is the value of the first line of text that declares name in the form the consumer
// reads (helpers/version_pins/check_pins.py pinned_value): blanks, the name taken literally, blanks,
// a colon, then the value, trimmed of blanks and quotes. It is false when no line declares name.
func declaration(text, name string) (string, bool) {
	for line := range strings.SplitSeq(text, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimLeftFunc(line, unicode.IsSpace), name)
		if !ok {
			continue
		}
		rest, ok = strings.CutPrefix(strings.TrimLeftFunc(rest, unicode.IsSpace), ":")
		if !ok {
			continue
		}
		return strings.TrimSpace(strings.Trim(strings.TrimSpace(rest), `"'`)), true
	}
	return "", false
}

// hasScalarKey is true when any document holds name as a mapping key, at any depth, with a scalar
// value. Aliases are not followed: the anchor they name is in the tree and is walked there.
func hasScalarKey(docs []*yaml.Node, name string) bool {
	for _, doc := range docs {
		if scalarKeyIn(doc, name) {
			return true
		}
	}
	return false
}

func scalarKeyIn(n *yaml.Node, name string) bool {
	if n == nil {
		return false
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], deref(n.Content[i+1])
			if k.Kind == yaml.ScalarNode && k.Value == name && v.Kind == yaml.ScalarNode {
				return true
			}
		}
	}
	for _, child := range n.Content {
		if scalarKeyIn(child, name) {
			return true
		}
	}
	return false
}
