package semgrepreport

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Excludes are a rule file's own paths.exclude globs: files semgrep drops without saying so, so
// a gate that hands every file by name must not expect them back among the scanned paths.
type Excludes []*regexp.Regexp

// Matches is whether a path is dropped by some glob.
func (e Excludes) Matches(path string) bool {
	for _, re := range e {
		if re.MatchString(path) {
			return true
		}
	}
	return false
}

// ruleFileDoc is the part of a semgrep rule file the excludes are read from.
type ruleFileDoc struct {
	Rules []struct {
		Paths struct {
			Exclude []string `yaml:"exclude"`
		} `yaml:"paths"`
	} `yaml:"rules"`
}

// ParseExcludes decodes the rule file named name and returns the globs of every rule's
// paths.exclude. A file that does not decode, or declares none, is an error, so an emptied list
// cannot silently narrow what a coverage check demands. Unknown keys are the rule's own business
// (semgrep reads them), so the decode is not strict about them.
func ParseExcludes(name, rule string) (Excludes, error) {
	var doc ruleFileDoc
	if err := yaml.Unmarshal([]byte(rule), &doc); err != nil {
		return nil, fmt.Errorf("cannot decode %s: %w", name, err)
	}
	var out Excludes
	for _, r := range doc.Rules {
		for _, glob := range r.Paths.Exclude {
			out = append(out, regexp.MustCompile(globRegexp(glob)))
		}
	}
	if len(out) == 0 {
		return nil, errors.New(name + " declares no paths.exclude list; the coverage check is built around the rule's own excludes")
	}
	return out, nil
}

// globRegexp turns a semgrep path glob into an expression that matches the way a gitignore
// pattern does: at any depth, unless the glob starts with `/`, which anchors it to the root.
// `*` stays within a path segment, `**` crosses segments and `**/` may match no directory. Every
// other character is quoted, so the expression always compiles.
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
