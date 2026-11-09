package sttrules

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/semgrepreport"
)

// language is a language the rules are written for, named as semgrep names it.
type language string

const (
	langJS language = "javascript"
	langPy language = "python"
)

// nounOf is a language as a person names it.
var nounOf = map[language]string{langJS: "JavaScript", langPy: "Python"}

// ruleSet is the rule file's rules, kept as the YAML nodes they were read as so a per-language
// rule file is the decoded rules written out again, not text assembled by hand.
type ruleSet struct {
	nodes map[language][]*yaml.Node
	ids   map[language][]string
}

// requireSemgrep is that semgrep runs at all; the reason it does not is part of the error.
func (g *gate) requireSemgrep(ctx context.Context) error {
	const hint = "semgrep not found: run ./playbooks/imports/play-python.yml (it installs the pinned semgrep; do not install it by hand)"
	res, err := g.runTool(ctx, versionTimeout, "semgrep", "--version")
	switch {
	case err != nil:
		return fmt.Errorf("%s (%w)", hint, err)
	case res.Code != 0:
		return fmt.Errorf("%s (semgrep --version exited %d: %s)", hint, res.Code, strings.TrimSpace(res.Stderr))
	}
	return nil
}

// loadRules checks the rule file and every fixture are regular files and decodes the rules.
func (g *gate) loadRules() (ruleSet, error) {
	files := make([]string, 0, 1+len(g.spec.languages))
	files = append(files, g.spec.ruleFile)
	for _, lang := range g.spec.languages {
		files = append(files, g.spec.fixtures[lang])
	}
	for _, rel := range files {
		info, err := os.Stat(g.path(rel))
		if err != nil {
			return ruleSet{}, fmt.Errorf("cannot read %s: %s", rel, reason(err))
		}
		if !info.Mode().IsRegular() {
			return ruleSet{}, fmt.Errorf("%s is not a regular file", rel)
		}
	}
	data, err := os.ReadFile(g.path(g.spec.ruleFile))
	if err != nil {
		return ruleSet{}, fmt.Errorf("cannot read %s: %s", g.spec.ruleFile, reason(err))
	}
	return g.spec.parseRules(data)
}

// languageNames is the spec's languages as semgrep names them, joined with "or".
func (s Spec) languageNames() string {
	names := make([]string, 0, len(s.languages))
	for _, lang := range s.languages {
		names = append(names, string(lang))
	}
	return strings.Join(names, " or ")
}

// reason is the cause of a file error without the checkout path the error also names.
func reason(err error) string {
	if pe, ok := errors.AsType[*os.PathError](err); ok {
		return pe.Err.Error()
	}
	return err.Error()
}

// parseRules decodes the rule file into its rules by language. Every rule needs an id and exactly
// one of the spec's languages, and each of them needs a rule, so a rule file cut down to one
// language's rules cannot stop judging another.
func (s Spec) parseRules(data []byte) (ruleSet, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return ruleSet{}, fmt.Errorf("cannot decode %s: %w", s.ruleFile, err)
	}
	list, err := s.rulesList(&doc)
	if err != nil {
		return ruleSet{}, err
	}
	set := ruleSet{nodes: map[language][]*yaml.Node{}, ids: map[language][]string{}}
	for i, rule := range list.Content {
		id, lang, err := s.identify(rule, i+1)
		if err != nil {
			return ruleSet{}, err
		}
		set.nodes[lang] = append(set.nodes[lang], rule)
		set.ids[lang] = append(set.ids[lang], id)
	}
	for _, lang := range s.languages {
		if len(set.nodes[lang]) == 0 {
			return ruleSet{}, fmt.Errorf("%s has no %s rules", s.ruleFile, lang)
		}
	}
	return set, nil
}

// rulesList is the sequence under the document's rules key.
func (s Spec) rulesList(doc *yaml.Node) (*yaml.Node, error) {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s has no rules", s.ruleFile)
	}
	list := mapValue(doc.Content[0], "rules")
	switch {
	case list == nil:
		return nil, fmt.Errorf("%s has no rules", s.ruleFile)
	case list.Kind != yaml.SequenceNode:
		return nil, fmt.Errorf("%s: rules must be a list", s.ruleFile)
	case len(list.Content) == 0:
		return nil, fmt.Errorf("%s has no rules", s.ruleFile)
	}
	return list, nil
}

// mapValue is the value of a key in a mapping node, or nil.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// identify is a rule's id and its one language.
func (s Spec) identify(rule *yaml.Node, n int) (string, language, error) {
	if rule.Kind != yaml.MappingNode {
		return "", "", fmt.Errorf("%s: rule %d is not a mapping", s.ruleFile, n)
	}
	idNode := mapValue(rule, "id")
	if idNode == nil || idNode.Kind != yaml.ScalarNode || idNode.Value == "" {
		return "", "", fmt.Errorf("%s: rule %d has no id", s.ruleFile, n)
	}
	id := idNode.Value
	langs := mapValue(rule, "languages")
	if langs == nil || langs.Kind != yaml.SequenceNode || len(langs.Content) != 1 || langs.Content[0].Kind != yaml.ScalarNode {
		return "", "", fmt.Errorf("%s: rule %s: languages must be exactly one language", s.ruleFile, id)
	}
	lang := language(langs.Content[0].Value)
	if !slices.Contains(s.languages, lang) {
		return "", "", fmt.Errorf("%s: rule %s: language %q is not %s", s.ruleFile, id, lang, s.languageNames())
	}
	return id, lang, nil
}

// rulesFor writes the rules of one language as a rule file of its own and returns its path.
func (set ruleSet) rulesFor(lang language, dir string) (string, error) {
	doc := yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "rules"},
			{Kind: yaml.SequenceNode, Tag: "!!seq", Content: set.nodes[lang]},
		},
	}}}
	data, err := yaml.Marshal(&doc)
	if err != nil {
		return "", fmt.Errorf("cannot encode the %s rules: %w", lang, err)
	}
	path := filepath.Join(dir, string(lang)+".yml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("cannot write the %s rules: %s", lang, reason(err))
	}
	return path, nil
}

// proveRules shows every rule is still matched by its fixture. A rule that silently stops
// matching is worse than a missing tool, so a regression here is the run failing and never
// "0 findings". semgrep --test exits 0 on a fixture with no annotations, so the annotations are
// required first.
func (g *gate) proveRules(ctx context.Context, set ruleSet) error {
	for _, lang := range g.spec.languages {
		fixture := g.spec.fixtures[lang]
		data, err := os.ReadFile(g.path(fixture))
		if err != nil {
			return fmt.Errorf("cannot read %s: %s", fixture, reason(err))
		}
		marked := semgrepreport.Annotated(string(data))
		for _, id := range set.ids[lang] {
			if !marked[id] {
				return fmt.Errorf("%s marks no ruleid line for %s; the self-test proves nothing for it", fixture, id)
			}
		}
	}
	for _, lang := range g.spec.languages {
		fixture := g.spec.fixtures[lang]
		res, err := g.runTool(ctx, semgrepTimeout, "semgrep", "--test", "--metrics=off", semgrepreport.TimeoutFlag, "--disable-version-check",
			"--config", g.path(g.spec.ruleFile), g.path(fixture))
		if err != nil {
			return err
		}
		if res.Code >= 2 && !strings.Contains(res.Stdout+res.Stderr, "rule id mismatch") {
			return fmt.Errorf("semgrep could not run the self-test (exit %d):\n%s%s", res.Code, g.scrub(res.Stdout), g.scrub(res.Stderr))
		}
		if res.Code != 0 {
			return failed("%s failed its fixture %s:\n%s%s", g.spec.ruleFile, fixture, g.scrub(res.Stdout), g.scrub(res.Stderr))
		}
	}
	return nil
}
