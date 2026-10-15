package docs

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/gates"
)

// The files the three catalogue checks read.
const (
	mainPlaybook    = "playbooks/playbook-main.yml"
	playbooksDoc    = "docs/playbooks.md"
	architectureDoc = "docs/architecture.md"
	indexDoc        = "CLAUDE.md"
	topicDir        = "CLAUDE/"
	qaDoc           = "CLAUDE/QA.md"
	gateTable       = "scripts/qa-gates.tsv"
)

// importedPlaybooks are the base names of the playbooks the main play imports, once each, in
// order. The file is YAML, so a commented import is no import and a play with hosts is no import.
func importedPlaybooks(body []byte) ([]string, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("does not parse as YAML: %w", err)
	}
	if len(root.Content) == 0 {
		return nil, errors.New("is empty")
	}
	list := root.Content[0]
	if list.Kind != yaml.SequenceNode {
		return nil, errors.New("is not a YAML list of plays")
	}
	var names []string
	for _, item := range list.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		for i := 0; i+1 < len(item.Content); i += 2 {
			if item.Content[i].Value != "import_playbook" {
				continue
			}
			value := item.Content[i+1]
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" || strings.TrimSpace(value.Value) == "" {
				return nil, fmt.Errorf("line %d: import_playbook is not a file name", value.Line)
			}
			if name := path.Base(value.Value); !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	if len(names) == 0 {
		return nil, errors.New("parsed 0 import_playbook entries: discovery is broken, not the tree")
	}
	return names, nil
}

// mentions is whether the text names the file as a whole token: not preceded by a letter, digit,
// underscore, dot or hyphen, nor followed by a letter, digit, underscore or hyphen. A longer name
// that merely ends or starts with it does not count.
func mentions(text, name string) bool { return mentionsAs(text, name, false) }

// mentionsAs is mentions; a path token (as) is not preceded by a slash either, so a longer path
// that ends in the same name does not count.
func mentionsAs(text, name string, path bool) bool {
	for from := 0; ; {
		at := strings.Index(text[from:], name)
		if at < 0 {
			return false
		}
		start, end := from+at, from+at+len(name)
		before := lastRune(text[:start])
		if !continuesName(before, true) && (!path || before != '/') && !continuesName(firstRune(text[end:]), false) {
			return true
		}
		from = start + 1
	}
}

// continuesName is whether a character next to a file name makes it part of a longer name; a
// dot only does so before the name.
func continuesName(r rune, before bool) bool {
	return r == '_' || r == '-' || (before && r == '.') || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// lastRune and firstRune are the character at an end of the text, 0 for none.
func lastRune(s string) rune {
	runes := []rune(s[max(0, len(s)-utf8.UTFMax):])
	if len(runes) == 0 {
		return 0
	}
	return runes[len(runes)-1]
}

func firstRune(s string) rune {
	runes := []rune(s[:min(len(s), utf8.UTFMax)])
	if len(runes) == 0 {
		return 0
	}
	return runes[0]
}

// checkCatalogue requires every playbook the main play imports to be named in both catalogue documents.
func (g *gate) checkCatalogue() ([]finding, error) {
	body, err := g.read(mainPlaybook)
	if err != nil {
		return nil, err
	}
	names, err := importedPlaybooks(body)
	if err != nil {
		return nil, fmt.Errorf("%s %w", mainPlaybook, err)
	}
	findings := []finding{}
	for _, doc := range []string{playbooksDoc, architectureDoc} {
		text, err := g.read(doc)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			if !mentions(string(text), name) {
				findings = append(findings, finding{File: doc, Target: name, Problem: "core playbook is absent from this document"})
			}
		}
	}
	return findings, nil
}

// topicFiles are the names of the topic files in the population: those directly under CLAUDE/.
func topicFiles(population []string) ([]string, error) {
	var names []string
	for _, rel := range population {
		if name, ok := strings.CutPrefix(rel, topicDir); ok && !strings.Contains(name, "/") {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, errors.New("found 0 topic files under " + topicDir + ": discovery is broken, not the tree")
	}
	slices.Sort(names)
	return names, nil
}

// checkTopicIndex requires every topic file to be named in the index document.
func (g *gate) checkTopicIndex(population []string) ([]finding, error) {
	index, err := g.read(indexDoc)
	if err != nil {
		return nil, err
	}
	names, err := topicFiles(population)
	if err != nil {
		return nil, err
	}
	findings := []finding{}
	for _, name := range names {
		if !mentionsAs(string(index), topicDir+name, true) {
			findings = append(findings, finding{File: indexDoc, Target: topicDir + name, Problem: "topic file has no row in the index"})
		}
	}
	return findings, nil
}

// moduleGate is a gate run as a module, which the QA document names by its dotted path.
var moduleGate = regexp.MustCompile(`-m\s+(helpers[A-Za-z0-9_.]+)`)

// gateNames are the names the QA document uses for the gates the table runs: a module by its
// dotted path, a script by its file name. A command that is neither is an error: a row the
// inventory cannot name is a gate it never looked at.
func gateNames(table []gates.Gate) ([]string, error) {
	var names []string
	for _, row := range table {
		command := strings.Join(row.Argv, " ")
		script, isScript := row.Script()
		switch module := moduleGate.FindStringSubmatch(command); {
		case module != nil:
			names = append(names, module[1])
		case isScript:
			names = append(names, script)
		default:
			return nil, fmt.Errorf("%s:%d: cannot name the gate run by %q", gateTable, row.Line, command)
		}
	}
	return names, nil
}

// checkInventory requires every gate the table runs to have a row in the QA document's tables,
// and every row there to be a gate that runs. Both directions: a one-way check lets a row stand
// for a gate that no longer runs.
func (g *gate) checkInventory() ([]finding, error) {
	table, err := gates.Load(filepath.Join(g.root, gateTable))
	if err != nil {
		return nil, err
	}
	names, err := gateNames(table)
	if err != nil {
		return nil, err
	}
	doc, err := g.document(qaDoc)
	if err != nil {
		return nil, err
	}
	if len(doc.rows) == 0 {
		return nil, fmt.Errorf("%s has no gate row in any table: it is not the inventory this check reads", qaDoc)
	}
	documented := map[string]int{}
	for _, row := range doc.rows {
		if line, seen := documented[row.name]; !seen || row.line < line {
			documented[row.name] = row.line
		}
	}
	findings := []finding{}
	for _, name := range sortedUnique(names) {
		if _, ok := documented[name]; !ok {
			findings = append(findings, finding{File: qaDoc, Target: name, Problem: "gate is run by qa-all.bash but has no row in this document"})
		}
	}
	for _, name := range sortedKeys(documented) {
		if !slices.Contains(names, name) {
			findings = append(findings, finding{File: qaDoc, Line: documented[name], Target: name, Problem: "this document lists the gate but qa-all.bash does not run it"})
		}
	}
	return findings, nil
}

func sortedUnique(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return slices.Compact(out)
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
