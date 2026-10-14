package ansible

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
)

// ExitCode is the status qa-ansible exits with.
type ExitCode int

// The statuses callers read: 0 no violation, 1 at least one, 2 the run could not complete.
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// defaultReportPath is where the machine report goes unless QA_JSON_OUT says otherwise.
const defaultReportPath = "/tmp/qa-ansible-results.json"

// searchDirs are the Ansible source trees, in the order they are searched. roles/ is an
// ansible-galaxy install target and is legitimately absent on a clean checkout.
var searchDirs = []string{"playbooks", "tasks", "vars", "environment", "roles"}

// Env is everything the command touches outside the repository tree: the environment, the two
// streams, the working directory, how a root directory is opened and how the report is written.
type Env struct {
	Getenv func(string) string
	Stdout io.Writer
	Stderr io.Writer
	// Cwd is the default root when neither --root nor --scripts is given.
	Cwd string
	// Open returns the file system rooted at a directory.
	Open func(root string) fs.FS
	// WriteFile writes the report.
	WriteFile func(path string, data []byte) error
}

const usageText = `usage: qa-ansible [--root DIR] [--scripts DIR]

Static Ansible QA over playbooks/ tasks/ vars/ environment/ roles/: error-hiding directives
without FAIL-FAST-OK, playbook shebang and execute bit, self-default variables, vars.scope and
the canonical scope guard, deprecated top-level ansible_<fact> variables, and a guessed uid in
a /run/user path.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

QA_JSON_OUT       where the machine report goes (default /tmp/qa-ansible-results.json).

Exit codes: 0 no violation, 1 a violation, 2 bad usage or the run could not complete.
stdout is the violations and the summary; failure to run goes to stderr.
`

// options is the parsed command line.
type options struct {
	help    bool
	root    string
	scripts string
}

// parseArgs reads the command line. The bash ignored every argument, so a typo ran the scan;
// here anything unknown is a usage error.
func parseArgs(args []string) (options, error) {
	var opts options
	rest := args
	for len(rest) > 0 {
		arg := rest[0]
		rest = rest[1:]
		switch arg {
		case "-h", "--help":
			opts.help = true
		case "--root", "--scripts":
			if len(rest) == 0 || rest[0] == "" || rest[0][0] == '-' {
				return options{}, fmt.Errorf("%s needs a directory", arg)
			}
			if arg == "--root" {
				opts.root = rest[0]
			} else {
				opts.scripts = rest[0]
			}
			rest = rest[1:]
		default:
			return options{}, &unknownArgError{arg: arg}
		}
	}
	return opts, nil
}

// unknownArgError is an argument the tool does not know; the usage text follows it.
type unknownArgError struct{ arg string }

func (e *unknownArgError) Error() string { return fmt.Sprintf("unknown argument '%s'", e.arg) }

// reportPath is where the machine report is written.
func reportPath(getenv func(string) string) string {
	if path := getenv("QA_JSON_OUT"); path != "" {
		return path
	}
	return defaultReportPath
}

// emitter writes to a stream and remembers the first write that failed.
type emitter struct {
	w   io.Writer
	err error
}

func (e *emitter) line(text string) {
	if _, err := fmt.Fprintln(e.w, text); err != nil && e.err == nil {
		e.err = err
	}
}

func (e *emitter) text(text string) {
	if _, err := io.WriteString(e.w, text); err != nil && e.err == nil {
		e.err = err
	}
}

// Main runs the command and returns the exit status.
func Main(args []string, env Env) ExitCode {
	out, errOut := &emitter{w: env.Stdout}, &emitter{w: env.Stderr}
	code := run(args, env, out, errOut)
	if code != ExitCannotRun && (out.err != nil || errOut.err != nil) {
		return ExitCannotRun
	}
	return code
}

func run(args []string, env Env, out, errOut *emitter) ExitCode {
	opts, err := parseArgs(args)
	if err != nil {
		errOut.line("ERROR: " + err.Error())
		if unknown, isUnknown := errors.AsType[*unknownArgError](err); isUnknown && unknown != nil {
			errOut.text(usageText)
		}
		return ExitCannotRun
	}
	if opts.help {
		out.text(usageText)
		return ExitOK
	}
	root, err := rootDir(opts, env.Cwd)
	if err != nil {
		errOut.line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	findings, err := Scan(env.Open(root), root)
	if err != nil {
		errOut.line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	return finish(findings, reportPath(env.Getenv), env.WriteFile, out, errOut)
}

// finish prints the violations, writes the report and prints the summary.
func finish(findings Findings, path string, write func(string, []byte) error, out, errOut *emitter) ExitCode {
	for _, line := range findings.Out {
		out.line(line)
	}
	body, err := findings.JSON()
	if err == nil {
		err = write(path, body)
	}
	if err != nil {
		errOut.line(fmt.Sprintf("ERROR: cannot write the report %s: %v", path, err))
		return ExitCannotRun
	}
	for _, line := range findings.Summary(path) {
		out.line(line)
	}
	if findings.Errors() > 0 {
		return ExitFailed
	}
	return ExitOK
}

// rootDir is the repository root: --root, else the parent of --scripts, else the working
// directory, made absolute.
func rootDir(opts options, cwd string) (string, error) {
	root := cwd
	switch {
	case opts.root != "":
		root = opts.root
	case opts.scripts != "":
		root = filepath.Join(opts.scripts, "..")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("cannot resolve the root %s: %w", root, err)
	}
	return abs, nil
}

// Scan runs the six checks over the tree and returns what they found. An error is a run that
// could not complete: no search directory, no playbooks directory, a file that cannot be read.
func Scan(fsys fs.FS, root string) (Findings, error) {
	var dirs []string
	for _, dir := range searchDirs {
		if dirExists(fsys, dir) {
			dirs = append(dirs, dir)
		}
	}
	if len(dirs) == 0 {
		return Findings{}, fmt.Errorf("none of the expected Ansible dirs (playbooks/ tasks/ vars/ environment/ roles/) exist under %s", root)
	}
	if !dirExists(fsys, "playbooks") {
		return Findings{}, fmt.Errorf("%s/playbooks does not exist; the hygiene and scope checks have nothing to read", root)
	}
	var f Findings
	if err := scanPatterns(fsys, root, dirs, &f); err != nil {
		return Findings{}, err
	}
	return f, nil
}

// scanPatterns runs the checks in the order the bash did, appending each finding's stdout line.
func scanPatterns(fsys fs.FS, root string, dirs []string, f *Findings) error {
	hits, err := grepLines(fsys, dirs, failFastPattern.MatchString)
	if err != nil {
		return err
	}
	for _, v := range failFastViolations(hits) {
		f.Out = append(f.Out, "  ERROR (fail-fast): "+v)
		f.FailFast = append(f.FailFast, v)
	}
	playbooks, err := loadPlaybooks(fsys)
	if err != nil {
		return err
	}
	if len(playbooks) == 0 {
		return errors.New("no playbook (a file with a '- hosts:' line) found under playbooks/; the hygiene and scope checks have nothing to read")
	}
	f.Playbooks = len(playbooks)
	hy := hygiene(playbooks)
	f.Out, f.Hygiene = append(f.Out, hy.out...), hy.entries
	selfHits, err := grepLines(fsys, dirs, selfDefaultMatch)
	if err != nil {
		return err
	}
	for _, h := range selfHits {
		v := h.String()
		f.Out = append(f.Out, "  ERROR (self-ref var): "+v)
		f.SelfRef = append(f.SelfRef, v)
	}
	sc := scope(playbooks, root)
	f.Out, f.Scope = append(f.Out, sc.out...), sc.entries
	return scanFacts(fsys, dirs, f)
}

// scanFacts runs checks 5 and 6: deprecated fact variables, then guessed uids in runtime paths.
func scanFacts(fsys fs.FS, dirs []string, f *Findings) error {
	checks := []struct {
		match  func(string) bool
		label  string
		advice string
	}{
		{factVarMatch, "deprecated fact var", "    Use ansible_facts['<name>'] — top-level injection is removed in ansible-core 2.24."},
		{runtimeDirMatch, "guessed uid in a runtime path", "    Resolve it: ansible.builtin.getent + ansible_facts['getent_passwd'][user_login][1]."},
	}
	for _, check := range checks {
		hits, err := grepLines(fsys, dirs, check.match)
		if err != nil {
			return err
		}
		for _, h := range hits {
			if commentOnly(h.text) {
				continue
			}
			v := h.String()
			f.Out = append(f.Out, "  ERROR ("+check.label+"): "+v, check.advice)
			f.FactVars = append(f.FactVars, v)
		}
	}
	return nil
}
