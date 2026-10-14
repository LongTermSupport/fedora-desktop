package sttrules

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// language is a language the rules are written for, named as semgrep names it.
type language string

const (
	langJS language = "javascript"
	langPy language = "python"
)

// languages is every language the rules must cover, in the order they are proven and scanned.
var languages = []language{langJS, langPy}

// fixtureOf is the annotated fixture each language's rules are proven against.
var fixtureOf = map[language]string{langJS: jsFixtureFile, langPy: pyFixtureFile}

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
	const hint = "semgrep not found. Install with: pipx install semgrep"
	res, err := g.runTool(ctx, versionTimeout, "semgrep", "--version")
	switch {
	case err != nil:
		return fmt.Errorf("%s (%w)", hint, err)
	case res.Code != 0:
		return fmt.Errorf("%s (semgrep --version exited %d: %s)", hint, res.Code, strings.TrimSpace(res.Stderr))
	}
	return nil
}

// loadRules checks the rule file and both fixtures are regular files and decodes the rules.
func (g *gate) loadRules() (ruleSet, error) {
	for _, rel := range []string{ruleFile, jsFixtureFile, pyFixtureFile} {
		info, err := os.Stat(g.path(rel))
		if err != nil {
			return ruleSet{}, fmt.Errorf("cannot read %s: %s", rel, reason(err))
		}
		if !info.Mode().IsRegular() {
			return ruleSet{}, fmt.Errorf("%s is not a regular file", rel)
		}
	}
	data, err := os.ReadFile(g.path(ruleFile))
	if err != nil {
		return ruleSet{}, fmt.Errorf("cannot read %s: %s", ruleFile, reason(err))
	}
	return parseRules(data)
}

// reason is the cause of a file error without the checkout path the error also names.
func reason(err error) string {
	if pe, ok := errors.AsType[*os.PathError](err); ok {
		return pe.Err.Error()
	}
	return err.Error()
}

// parseRules decodes the rule file into its rules by language. Every rule needs an id and exactly
// one language, and both languages need a rule, so a rule file cut down to one language's rules
// cannot stop judging the other.
func parseRules(data []byte) (ruleSet, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return ruleSet{}, fmt.Errorf("cannot decode %s: %w", ruleFile, err)
	}
	list, err := rulesList(&doc)
	if err != nil {
		return ruleSet{}, err
	}
	set := ruleSet{nodes: map[language][]*yaml.Node{}, ids: map[language][]string{}}
	for i, rule := range list.Content {
		id, lang, err := identify(rule, i+1)
		if err != nil {
			return ruleSet{}, err
		}
		set.nodes[lang] = append(set.nodes[lang], rule)
		set.ids[lang] = append(set.ids[lang], id)
	}
	for _, lang := range languages {
		if len(set.nodes[lang]) == 0 {
			return ruleSet{}, fmt.Errorf("%s has no %s rules", ruleFile, lang)
		}
	}
	return set, nil
}

// rulesList is the sequence under the document's rules key.
func rulesList(doc *yaml.Node) (*yaml.Node, error) {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s has no rules", ruleFile)
	}
	list := mapValue(doc.Content[0], "rules")
	switch {
	case list == nil:
		return nil, fmt.Errorf("%s has no rules", ruleFile)
	case list.Kind != yaml.SequenceNode:
		return nil, fmt.Errorf("%s: rules must be a list", ruleFile)
	case len(list.Content) == 0:
		return nil, fmt.Errorf("%s has no rules", ruleFile)
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
func identify(rule *yaml.Node, n int) (string, language, error) {
	if rule.Kind != yaml.MappingNode {
		return "", "", fmt.Errorf("%s: rule %d is not a mapping", ruleFile, n)
	}
	idNode := mapValue(rule, "id")
	if idNode == nil || idNode.Kind != yaml.ScalarNode || idNode.Value == "" {
		return "", "", fmt.Errorf("%s: rule %d has no id", ruleFile, n)
	}
	id := idNode.Value
	langs := mapValue(rule, "languages")
	if langs == nil || langs.Kind != yaml.SequenceNode || len(langs.Content) != 1 || langs.Content[0].Kind != yaml.ScalarNode {
		return "", "", fmt.Errorf("%s: rule %s: languages must be exactly one language", ruleFile, id)
	}
	lang := language(langs.Content[0].Value)
	if !slices.Contains(languages, lang) {
		return "", "", fmt.Errorf("%s: rule %s: language %q is not javascript or python", ruleFile, id, lang)
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

// annotationRe is a fixture annotation: a comment naming the rules the next line must match.
var annotationRe = regexp.MustCompile(`ruleid:\s*([\w.-]+(?:\s*,\s*[\w.-]+)*)`)

// annotated is the rule ids a fixture marks with a ruleid annotation.
func annotated(fixture string) map[string]bool {
	ids := map[string]bool{}
	for _, m := range annotationRe.FindAllStringSubmatch(fixture, -1) {
		for id := range strings.SplitSeq(m[1], ",") {
			ids[strings.TrimSpace(id)] = true
		}
	}
	return ids
}

// proveRules shows every rule is still matched by its fixture. A rule that silently stops
// matching is worse than a missing tool, so a regression here is the run failing and never
// "0 findings". semgrep --test exits 0 on a fixture with no annotations, so the annotations are
// required first.
func (g *gate) proveRules(ctx context.Context, set ruleSet) error {
	for _, lang := range languages {
		fixture := fixtureOf[lang]
		data, err := os.ReadFile(g.path(fixture))
		if err != nil {
			return fmt.Errorf("cannot read %s: %s", fixture, reason(err))
		}
		marked := annotated(string(data))
		for _, id := range set.ids[lang] {
			if !marked[id] {
				return fmt.Errorf("%s marks no ruleid line for %s; the self-test proves nothing for it", fixture, id)
			}
		}
	}
	for _, lang := range languages {
		fixture := fixtureOf[lang]
		res, err := g.runTool(ctx, semgrepTimeout, "semgrep", "--test", "--metrics=off", "--disable-version-check",
			"--config", g.path(ruleFile), g.path(fixture))
		if err != nil {
			return err
		}
		if res.Code != 0 {
			return failed("%s failed its fixture %s:\n%s%s", ruleFile, fixture, g.scrub(res.Stdout), g.scrub(res.Stderr))
		}
	}
	return nil
}
