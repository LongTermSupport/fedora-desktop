// Package docs is the documentation integrity gate: every relative link of the core documents
// must point at something this repository tracks, every anchor at a real heading, every playbook
// the main play imports must be named in the two catalogue documents, every topic file in the
// index, and every gate qa-all runs in the QA document's tables. Markdown is read with a
// CommonMark parser, the playbook list as YAML, the gate table by the loader qa-all uses; the
// specification is the plan's SPEC-qa-docs.md.
package docs

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/discovery"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootpath"
)

// ExitCode is the status qa-docs exits with.
type ExitCode int

// The statuses callers read: 0 no finding, 1 at least one, 2 the run could not complete.
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// defaultReportPath is where the machine report goes unless QA_JSON_OUT says otherwise.
const defaultReportPath = "/tmp/qa-docs-results.json"

// The command-line flags that take a value.
const (
	flagRoot    = "--root"
	flagScripts = "--scripts"
)

// Env is everything the command touches outside the repository tree: the environment, the two
// streams, the working directory, the process seam, and the file reads and writes.
type Env struct {
	Getenv         func(string) string
	Stdout, Stderr io.Writer
	// Cwd is the default root when neither --root nor --scripts is given.
	Cwd string
	// Runner starts git, for the shared file lists.
	Runner proc.Runner
	// ReadFile reads a document, the playbook list and the other inputs.
	ReadFile func(path string) ([]byte, error)
	// WriteFile writes the report.
	WriteFile func(path string, data []byte) error
}

const usageText = `usage: qa-docs [--root DIR] [--scripts DIR]

Checks the core documents: every relative link target exists and is tracked, every #anchor matches
a heading, every playbook playbooks/playbook-main.yml imports is named in docs/playbooks.md and
docs/architecture.md, every topic file directly under CLAUDE/ is in CLAUDE.md, and every gate in
scripts/qa-gates.tsv has a row in CLAUDE/QA.md (and the reverse). CLAUDE/Plan is out of scope.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory, which holds qa-trees.tsv; the root is its parent
  -h, --help      this text

QA_JSON_OUT       where the machine report goes (default /tmp/qa-docs-results.json).

Exit codes: 0 no finding, 1 at least one finding, 2 bad usage, an empty scope, a file that cannot
be read or parsed, or the run could not complete. stdout is the findings and the verdict;
failure to run goes to stderr.
`

// options is the parsed command line.
type options struct {
	help    bool
	root    string
	scripts string
}

// parseArgs reads the command line; anything unknown, a positional word included, is a usage error.
func parseArgs(args []string) (options, error) {
	var opts options
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; arg {
		case "-h", "--help":
			opts.help = true
		case flagRoot, flagScripts:
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s needs a value", arg)
			}
			i++
			if arg == flagRoot {
				opts.root = args[i]
			} else {
				opts.scripts = args[i]
			}
		default:
			return opts, fmt.Errorf("unknown argument '%s'", arg)
		}
	}
	return opts, nil
}

// gate is one run.
type gate struct {
	env     Env
	root    string
	scripts string
	report  string
	stdout  bytes.Buffer
	// parsed caches each document read, by repository-relative path.
	parsed map[string]document
}

// Main runs the gate and returns the exit status.
func Main(args []string, env Env) ExitCode {
	opts, err := parseArgs(args)
	if err != nil {
		return fail(env, "", err, true)
	}
	if opts.help {
		return write(env, usageText)
	}
	root, err := rootpath.Root(env.Cwd, opts.root, opts.scripts)
	if err != nil {
		return fail(env, "", err, false)
	}
	g := &gate{env: env, root: root, parsed: map[string]document{}}
	g.scripts = opts.scripts
	if g.scripts == "" {
		g.scripts = filepath.Join(root, "scripts")
	} else if !filepath.IsAbs(g.scripts) {
		g.scripts = filepath.Join(env.Cwd, g.scripts)
	}
	g.report = env.Getenv("QA_JSON_OUT")
	if g.report == "" {
		g.report = defaultReportPath
	}
	verdict, err := g.run()
	if err != nil {
		return fail(env, root, err, false)
	}
	if _, err := env.Stdout.Write(g.stdout.Bytes()); err != nil {
		return fail(env, root, fmt.Errorf("cannot write to stdout: %w", err), false)
	}
	return verdict
}

// write sends text to stdout; the status is that of the write.
func write(env Env, text string) ExitCode {
	if _, err := io.WriteString(env.Stdout, text); err != nil {
		return fail(env, "", fmt.Errorf("cannot write to stdout: %w", err), false)
	}
	return ExitOK
}

// fail prints an ERROR line (and the usage, for a usage error) on stderr, with the checkout's
// path taken out of it. A stderr that cannot be written to has nowhere left to say so; the
// status is the same either way.
func fail(env Env, root string, err error, usage bool) ExitCode {
	text := "ERROR: " + scrub(root, err.Error()) + "\n"
	if usage {
		text += usageText
	}
	return statusAfter(io.WriteString(env.Stderr, text))
}

// scrub removes the checkout's path from a message, so that a run through a symlinked or
// relocated root never names it.
func scrub(root, text string) string {
	if root == "" {
		return text
	}
	return strings.ReplaceAll(strings.ReplaceAll(text, root+string(filepath.Separator), ""), root, "the repository root")
}

// statusAfter is the status of a run that could not complete, whether or not its explanation
// could be written.
func statusAfter(int, error) ExitCode { return ExitCannotRun }

// run is the whole check; every error is a run that could not complete.
func (g *gate) run() (ExitCode, error) {
	population, err := g.population()
	if err != nil {
		return ExitCannotRun, err
	}
	tracked, err := g.trackedSet()
	if err != nil {
		return ExitCannotRun, err
	}
	findings, vendored, err := g.checkLinks(population, tracked)
	if err != nil {
		return ExitCannotRun, err
	}
	catalogue, err := g.checkCatalogue()
	if err != nil {
		return ExitCannotRun, err
	}
	topics, err := g.checkTopicIndex(population)
	if err != nil {
		return ExitCannotRun, err
	}
	inventory, err := g.checkInventory()
	if err != nil {
		return ExitCannotRun, err
	}
	findings = append(append(append(findings, catalogue...), topics...), inventory...)
	return g.finish(population, tracked, findings, vendored)
}

// list runs one shared discovery list and returns its paths relative to the root.
func (g *gate) list(name string, absolute bool) ([]string, error) {
	var stdout, stderr bytes.Buffer
	env := discovery.Env{Stdout: &stdout, Stderr: &stderr, Cwd: g.root, Runner: g.env.Runner}
	args := []string{flagRoot, g.root, flagScripts, g.scripts, name}
	if code := discovery.Main(args, env); code != discovery.ExitOK {
		return nil, fmt.Errorf("discovery of %s failed: %s", name, strings.TrimSpace(strings.TrimPrefix(stderr.String(), "ERROR: ")))
	}
	var rels []string
	for p := range strings.SplitSeq(stdout.String(), "\x00") {
		if p == "" {
			continue
		}
		if absolute {
			p = strings.TrimPrefix(p, g.root+"/")
		}
		rels = append(rels, p)
	}
	return rels, nil
}

// read reads a file under the root; its error names the path relative to the root and says why
// without the checkout's path.
func (g *gate) read(rel string) ([]byte, error) {
	data, err := g.env.ReadFile(filepath.Join(g.root, filepath.FromSlash(rel)))
	if err != nil {
		if pathErr, ok := errors.AsType[*fs.PathError](err); ok {
			err = pathErr.Err
		}
		return nil, fmt.Errorf("cannot read %s: %w", rel, err)
	}
	return data, nil
}

// document is the parsed Markdown file at the path, read once.
func (g *gate) document(rel string) (document, error) {
	if doc, ok := g.parsed[rel]; ok {
		return doc, nil
	}
	data, err := g.read(rel)
	if err != nil {
		return document{}, err
	}
	doc := parseDocument(data)
	g.parsed[rel] = doc
	return doc, nil
}
