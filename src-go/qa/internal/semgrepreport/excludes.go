package semgrepreport

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Excludes is a rule's paths.exclude globs, matched as semgrep matches a path against them.
// semgrep drops a file an exclude matches without saying so, so a gate that hands it such a file
// and then refuses a pass over every file it did not read must know the excludes too.
type Excludes []*regexp.Regexp

// CompileExcludes turns paths.exclude globs into the expressions Matches uses.
func CompileExcludes(globs []string) (Excludes, error) {
	out := make(Excludes, 0, len(globs))
	for _, glob := range globs {
		re, err := regexp.Compile(globRegexp(glob))
		if err != nil {
			return nil, fmt.Errorf("cannot read the exclude %q: %w", glob, err)
		}
		out = append(out, re)
	}
	return out, nil
}

// Matches is whether some glob drops the repository-relative path.
func (e Excludes) Matches(path string) bool {
	for _, re := range e {
		if re.MatchString(path) {
			return true
		}
	}
	return false
}

// Scope is which files a rule file reads: each rule's own paths.include and paths.exclude. A rule
// file of several rules reads a file when any one of them does, so a file one rule excludes is
// still read when a sibling rule does not exclude it.
type Scope struct {
	rules []ruleScope
}

// ruleScope is one rule's paths: an empty include reads every file the excludes leave.
type ruleScope struct {
	include, exclude Excludes
}

// ParseScope reads the paths of every rule of the rule file named name. A file that does not
// decode, or holds no rule, is an error: an empty scope would read nothing, so every file would
// look excluded.
func ParseScope(name string, data []byte) (Scope, error) {
	var doc struct {
		Rules []struct {
			Paths struct {
				Include []string `yaml:"include"`
				Exclude []string `yaml:"exclude"`
			} `yaml:"paths"`
		} `yaml:"rules"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return Scope{}, fmt.Errorf("cannot decode %s: %w", name, err)
	}
	if len(doc.Rules) == 0 {
		return Scope{}, fmt.Errorf("%s holds no rule", name)
	}
	var s Scope
	for _, r := range doc.Rules {
		include, err := CompileExcludes(r.Paths.Include)
		if err != nil {
			return Scope{}, fmt.Errorf("in %s: %w", name, err)
		}
		exclude, err := CompileExcludes(r.Paths.Exclude)
		if err != nil {
			return Scope{}, fmt.Errorf("in %s: %w", name, err)
		}
		s.rules = append(s.rules, ruleScope{include: include, exclude: exclude})
	}
	return s, nil
}

// Reads is whether some rule of the file reads the repository-relative path.
func (s Scope) Reads(path string) bool {
	for _, r := range s.rules {
		if (len(r.include) == 0 || r.include.Matches(path)) && !r.exclude.Matches(path) {
			return true
		}
	}
	return false
}

// RuleExcludes is the paths.exclude list of one rule, read from the rule's YAML mapping. A rule
// without one has none; a paths or exclude of any other shape is refused, so a list written wrong
// is never read as no excludes.
func RuleExcludes(rule *yaml.Node) ([]string, error) {
	if rule.Kind != yaml.MappingNode {
		return nil, errors.New("a rule is not a mapping")
	}
	paths := valueOf(rule, "paths")
	if paths == nil {
		return nil, nil
	}
	if paths.Kind != yaml.MappingNode {
		return nil, errors.New("paths is not a mapping")
	}
	list := valueOf(paths, "exclude")
	if list == nil {
		return nil, nil
	}
	if list.Kind != yaml.SequenceNode {
		return nil, errors.New("paths.exclude is not a list")
	}
	globs := make([]string, 0, len(list.Content))
	for _, item := range list.Content {
		if item.Kind != yaml.ScalarNode {
			return nil, errors.New("a paths.exclude entry is not a string")
		}
		globs = append(globs, item.Value)
	}
	return globs, nil
}

// valueOf is the value of a key in a mapping node, or nil.
func valueOf(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// globRegexp turns a semgrep path glob into an expression that matches the way a gitignore
// pattern does: at any depth, unless the glob starts with `/`, which anchors it to the root.
// `*` stays within a path segment, `**` crosses segments and `**/` may match no directory.
func globRegexp(glob string) string {
	var b strings.Builder
	if rooted, ok := strings.CutPrefix(glob, "/"); ok {
		glob = rooted
		b.WriteString("^")
	} else {
		b.WriteString("^(?:.*/)?")
	}
	for i := 0; i < len(glob); i++ {
		switch {
		case strings.HasPrefix(glob[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case strings.HasPrefix(glob[i:], "**"):
			b.WriteString(".*")
			i++
		case glob[i] == '*':
			b.WriteString("[^/]*")
		case glob[i] == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(glob[i : i+1]))
		}
	}
	b.WriteString("$")
	return b.String()
}
