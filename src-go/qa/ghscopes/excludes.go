package ghscopes

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"go.yaml.in/yaml/v3"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/qatrees"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/semgrepreport"
)

// excludes are the rule's own paths.exclude globs: files semgrep drops without saying so.
type excludes = semgrepreport.Excludes

// scope is what the gate leaves unread on purpose: every file scripts/qa-trees.tsv keeps out of
// all gates (the daemon-deployed files, vendored trees, the semgrep fixtures), which is never
// handed to semgrep, and the files the rule's own excludes drop, which semgrep drops itself. The
// table is the one list of trees that are not ours; a rule's excludes name only what is
// particular to that rule (qa-trees-drift, issue #217). The rows the spec still scans
// (Spec.stillScans) are taken out of the table first.
//
// reads is what each rule of the file reads (its paths.include and paths.exclude, rule by rule):
// a named run hands semgrep only the files some rule reads.
type scope struct {
	repo  fs.FS
	trees qatrees.Table
	rule  excludes
	reads semgrepreport.Scope
}

// loadScope reads the trees table and the rule's excludes.
func (g *gate) loadScope() (scope, error) {
	rule, err := g.readRule()
	if err != nil {
		return scope{}, err
	}
	s, err := g.spec.readScope(g.root)
	if err != nil {
		return scope{}, err
	}
	s.rule = rule
	return s, nil
}

// readScope reads the trees table, less the rows the spec still scans, and the rule file's scope.
func (s Spec) readScope(root string) (scope, error) {
	repo := os.DirFS(root)
	table, err := qatrees.Load(repo)
	if err != nil {
		return scope{}, err
	}
	trees, err := table.Without(s.stillScans...)
	if err != nil {
		return scope{}, err
	}
	data, err := fs.ReadFile(repo, s.ruleFile)
	if err != nil {
		return scope{}, fmt.Errorf("cannot read %s: %w", s.ruleFile, err)
	}
	reads, err := semgrepreport.ParseScope(s.ruleFile, data)
	if err != nil {
		return scope{}, err
	}
	return scope{repo: repo, trees: trees, reads: reads}, nil
}

// notOurs is whether the trees table keeps the published file rel out of every gate.
func (s scope) notOurs(rel string) (bool, error) {
	return s.trees.ExcludesFile(s.repo, rel)
}

// readsFile is whether the gate reads the file rel: the trees table leaves it in and some rule of
// the rule file reads it.
func (s scope) readsFile(rel string) (bool, error) {
	notOurs, err := s.notOurs(rel)
	if err != nil || notOurs {
		return false, err
	}
	return s.reads.Reads(rel), nil
}

// Reader answers, for a caller that plans a run of the gate over changed files (qa-changed),
// which of them the gate would read: a run named only files it does not read has nothing to
// scan, so it need not be planned (issue #403).
type Reader struct {
	spec  Spec
	scope scope
}

// Reader reads the trees table and the rule file under the repository root.
func (s Spec) Reader(root string) (Reader, error) {
	sc, err := s.readScope(root)
	if err != nil {
		return Reader{}, err
	}
	return Reader{spec: s, scope: sc}, nil
}

// Reads is whether the gate reads the existing repository-relative file rel.
func (r Reader) Reads(rel string) (bool, error) { return r.scope.readsFile(rel) }

// RuleFile is the gate's rule file, relative to the repository root.
func (r Reader) RuleFile() string { return r.spec.ruleFile }

// readRule checks the rule and fixture are regular files and reads the rule's excludes.
func (g *gate) readRule() (excludes, error) {
	for _, rel := range append([]string{g.spec.ruleFile}, g.spec.fixtures()...) {
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
	var globs []string
	for _, r := range doc.Rules {
		globs = append(globs, r.Paths.Exclude...)
	}
	if len(globs) == 0 {
		return nil, errors.New(name + " declares no paths.exclude list; the coverage check is built around the rule's own excludes")
	}
	out, err := semgrepreport.CompileExcludes(globs)
	if err != nil {
		return nil, fmt.Errorf("in %s: %w", name, err)
	}
	return out, nil
}
