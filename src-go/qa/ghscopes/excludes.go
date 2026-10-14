package ghscopes

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// excludes are the rule's own paths.exclude globs: files semgrep drops without saying so.
type excludes []*regexp.Regexp

// matches is whether a path is dropped by some glob.
func (e excludes) matches(path string) bool {
	for _, re := range e {
		if re.MatchString(path) {
			return true
		}
	}
	return false
}

// readRule checks the rule and fixture are regular files and reads the rule's excludes.
func (g *gate) readRule() (excludes, error) {
	for _, rel := range []string{ruleFile, fixtureFile} {
		info, err := os.Stat(g.path(rel))
		if err != nil {
			return nil, fmt.Errorf("cannot read %s: %w", rel, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s is not a regular file", rel)
		}
	}
	data, err := os.ReadFile(g.path(ruleFile))
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", ruleFile, err)
	}
	return parseExcludes(string(data))
}

// ruleFileDoc is the part of a semgrep rule file this gate reads.
type ruleFileDoc struct {
	Rules []struct {
		Paths struct {
			Exclude []string `yaml:"exclude"`
		} `yaml:"paths"`
	} `yaml:"rules"`
}

// parseExcludes decodes the rule file and returns the globs of every rule's paths.exclude. A
// file that does not decode, or declares none, is an error, so an emptied list cannot silently
// narrow what the coverage check demands. Unknown keys are the rule's own business (semgrep
// reads them), so the decode is not strict about them.
func parseExcludes(rule string) (excludes, error) {
	var doc ruleFileDoc
	if err := yaml.Unmarshal([]byte(rule), &doc); err != nil {
		return nil, fmt.Errorf("cannot decode %s: %w", ruleFile, err)
	}
	var out excludes
	for _, r := range doc.Rules {
		for _, glob := range r.Paths.Exclude {
			re, err := regexp.Compile(globRegexp(glob))
			if err != nil {
				return nil, fmt.Errorf("cannot read the exclude %q in %s: %w", glob, ruleFile, err)
			}
			out = append(out, re)
		}
	}
	if len(out) == 0 {
		return nil, errors.New(ruleFile + " declares no paths.exclude list; the coverage check is built around the rule's own excludes")
	}
	return out, nil
}

// globRegexp turns a semgrep path glob into an anchored expression: `*` stays within a path
// segment, `**` crosses segments and `**/` may match no directory at all.
func globRegexp(glob string) string {
	var b strings.Builder
	b.WriteString("^")
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
