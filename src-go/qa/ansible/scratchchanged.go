package ansible

import (
	"bytes"
	"errors"
	"io"
	"regexp"
	"slices"

	"go.yaml.in/yaml/v3"
)

// The scratch-always-changed rule (CLAUDE/QA.md, "scratch-always-changed"). A tempfile task
// reports changed on every run, as does a task that makes, fills or removes a path inside the
// directory one made: the directory is new each run. Its only effect is on scratch that the play
// removes again, so a second run of a converged play reports changes, and the recap that is never
// clean hides the run that did change something (issue 247: play-teamchat's controller build
// directory, three tasks changed on every pass).
//
// Scratch names are the register of each tempfile task, and then, to a fixed point over every
// file judged together, each vars entry (of a play, a block, a task, an import or include) and
// each set_fact key whose value names a scratch name, so a path handed to a task file is
// followed into it. The rule reports a tempfile task, and a task of a module that writes a path
// (writerModules) whose path or dest names a scratch name, when its changed_when is absent or
// the constant true. A when does not excuse it. Command modules are command-always-changed's.

// ruleScratchChanged is the rule ID, printed with every finding and the anchor of its section.
const ruleScratchChanged = "scratch-always-changed"

// writerModules are the modules that write the path their path or dest names, and report it.
var writerModules = map[string]bool{
	modFile: true, modCopy: true, modTemplate: true, "get_url": true, modUnarchive: true, modLineInFile: true,
	modBlockInFile: true, "replace": true, "uri": true, "git": true, "archive": true, "synchronize": true,
}

const (
	modTemplate  = "template"
	modUnarchive = "unarchive"
)

// scratchArgs are the arguments that say which path a writer writes.
var scratchArgs = []string{"path", "dest"}

// identWord is a Jinja identifier, as a reference in a template text.
var identWord = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// scratchAlwaysChanged returns the rule's findings over every file, judged together, each
// "<rel>:<line>: <rule>: <why>", in file then document order. A file the YAML library cannot
// parse is skipped: command-always-changed reports it.
func scratchAlwaysChanged(sources []yamlSource) []string {
	files := make([]scratchFile, 0, len(sources))
	scratch := map[string]bool{}
	for _, src := range sources {
		f := scratchFile{rel: src.rel, docs: parsedDocs(src.data)}
		for _, t := range f.tasks() {
			if reg := scalarValue(t.node, "register"); t.module == modTempfile && reg != "" {
				scratch[reg] = true
			}
		}
		files = append(files, f)
	}
	for spreadAll(files, scratch) {
	}
	var findings []string
	for _, f := range files {
		for _, t := range f.tasks() {
			if scratchTask(t, scratch) && !decidesChanged(t) {
				findings = append(findings, f.rel+":"+t.line()+": "+ruleScratchChanged+": this task only makes, "+
					"fills or removes a scratch directory and reports changed on every run it makes; set changed_when: false "+
					"(the task that installs from it reports the change)")
			}
		}
	}
	return findings
}

// spreadAll is one pass of spreadScratch over every file: whether it added a scratch name.
func spreadAll(files []scratchFile, scratch map[string]bool) bool {
	grew := false
	for _, f := range files {
		for _, doc := range f.docs {
			grew = spreadScratch(doc, scratch) || grew
		}
	}
	return grew
}

// scratchFile is one file's parsed documents.
type scratchFile struct {
	rel  string
	docs []*yaml.Node
}

// tasks is every task the file's documents run unasked, in document order.
func (f scratchFile) tasks() []uxTask {
	var out []uxTask
	for _, doc := range f.docs {
		for _, top := range doc.Content {
			out = append(out, runTasks(top)...)
		}
	}
	return out
}

// parsedDocs is every document of a file, or none when the file does not parse.
func parsedDocs(data []byte) []*yaml.Node {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs []*yaml.Node
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return docs
		}
		if err != nil {
			return nil
		}
		docs = append(docs, &doc)
	}
}

// spreadScratch adds to scratch every vars entry and set_fact key under n whose value names a
// scratch name, and says whether it added one.
func spreadScratch(n *yaml.Node, scratch map[string]bool) bool {
	grew := false
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i].Value
			if key == keyVars || shortName(key) == modSetFact {
				grew = spreadEntries(n.Content[i+1], scratch) || grew
			}
		}
	}
	for _, child := range n.Content {
		grew = spreadScratch(child, scratch) || grew
	}
	return grew
}

// spreadEntries adds each key of a vars or set_fact mapping whose value names a scratch name.
func spreadEntries(m *yaml.Node, scratch map[string]bool) bool {
	grew := false
	if m.Kind != yaml.MappingNode {
		return false
	}
	for j := 0; j+1 < len(m.Content); j += 2 {
		name := m.Content[j].Value
		if !scratch[name] && namesScratch(m.Content[j+1], scratch) {
			scratch[name], grew = true, true
		}
	}
	return grew
}

// namesScratch is whether a scalar names a scratch name as a whole identifier.
func namesScratch(v *yaml.Node, scratch map[string]bool) bool {
	if v == nil || v.Kind != yaml.ScalarNode {
		return false
	}
	return slices.ContainsFunc(identWord.FindAllString(v.Value, -1), func(w string) bool { return scratch[w] })
}

// scratchTask is whether a task makes scratch, or writes a path inside it.
func scratchTask(t uxTask, scratch map[string]bool) bool {
	if t.module == modTempfile {
		return true
	}
	args := t.moduleValue()
	if !writerModules[t.module] || args == nil || args.Kind != yaml.MappingNode {
		return false
	}
	return slices.ContainsFunc(scratchArgs, func(key string) bool { return namesScratch(mappingValue(args, key), scratch) })
}

// decidesChanged is whether a task's changed_when is anything but absent or the constant true.
func decidesChanged(t uxTask) bool {
	cw := mappingValue(t.node, "changed_when")
	return cw != nil && !constantTrue(cw)
}
