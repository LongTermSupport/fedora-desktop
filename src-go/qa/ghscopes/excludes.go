package ghscopes

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/qatrees"
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

// scope is what the gate leaves unread on purpose: every file scripts/qa-trees.tsv keeps out of
// all gates (the daemon-deployed files, vendored trees, the semgrep fixtures), which is never
// handed to semgrep, and the files the rule's own excludes drop, which semgrep drops itself. The
// table is the one list of trees that are not ours; a rule's excludes name only what is
// particular to that rule (qa-trees-drift, issue #217). The rows the spec still scans
// (Spec.stillScans) are taken out of the table first.
type scope struct {
	repo  fs.FS
	trees qatrees.Table
	rule  excludes
}

// loadScope reads the trees table and the rule's excludes.
func (g *gate) loadScope() (scope, error) {
	repo := os.DirFS(g.root)
	table, err := qatrees.Load(repo)
	if err != nil {
		return scope{}, err
	}
	trees, err := table.Without(g.spec.stillScans...)
	if err != nil {
		return scope{}, err
	}
	rule, err := g.readRule()
	if err != nil {
		return scope{}, err
	}
	return scope{repo: repo, trees: trees, rule: rule}, nil
}

// notOurs is whether the trees table keeps the published file rel out of every gate.
func (s scope) notOurs(rel string) (bool, error) {
	return s.trees.ExcludesFile(s.repo, rel)
}

// readRule checks the rule and fixture are regular files and reads the rule's excludes.
func (g *gate) readRule() (excludes, error) {
	for _, rel := range []string{g.spec.ruleFile, g.spec.fixtureFile} {
		info, err := os.Stat(g.path(rel))
		if err != nil {
			return nil, fmt.Errorf("cannot read %s: %w", rel, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s is not a regular file", rel)
		}
	}
	data, err := os.ReadFile(g.path(g.spec.ruleFile))
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", g.spec.ruleFile, err)
	}
	return parseExcludes(g.spec.ruleFile, string(data))
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
func parseExcludes(name, rule string) (excludes, error) {
	var doc ruleFileDoc
	if err := yaml.Unmarshal([]byte(rule), &doc); err != nil {
		return nil, fmt.Errorf("cannot decode %s: %w", name, err)
	}
	var out excludes
	for _, r := range doc.Rules {
		for _, glob := range r.Paths.Exclude {
			re, err := regexp.Compile(globRegexp(glob))
			if err != nil {
				return nil, fmt.Errorf("cannot read the exclude %q in %s: %w", glob, name, err)
			}
			out = append(out, re)
		}
	}
	if len(out) == 0 {
		return nil, errors.New(name + " declares no paths.exclude list; the coverage check is built around the rule's own excludes")
	}
	return out, nil
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
