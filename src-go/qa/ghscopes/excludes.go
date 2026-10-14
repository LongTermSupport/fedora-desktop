package ghscopes

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
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

// parseExcludes reads the list under the rule's `exclude:` key: the `- glob` lines that follow
// it, comments and blank lines between them allowed. A rule with none is an error, so an emptied
// or unparseable list cannot silently narrow what the coverage check demands.
func parseExcludes(rule string) (excludes, error) {
	var globs []string
	inList := false
	for line := range strings.SplitSeq(rule, "\n") {
		text := strings.TrimSpace(line)
		switch {
		case !inList:
			inList = text == "exclude:"
		case text == "" || strings.HasPrefix(text, "#"):
		case strings.HasPrefix(text, "- "):
			globs = append(globs, strings.Trim(strings.TrimSpace(text[2:]), `'"`))
		default:
			inList = false
		}
	}
	if len(globs) == 0 {
		return nil, errors.New(ruleFile + " declares no paths.exclude list; the coverage check is built around the rule's own excludes")
	}
	out := make(excludes, 0, len(globs))
	for _, glob := range globs {
		re, err := regexp.Compile(globRegexp(glob))
		if err != nil {
			return nil, fmt.Errorf("cannot read the exclude %q: %w", glob, err)
		}
		out = append(out, re)
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
