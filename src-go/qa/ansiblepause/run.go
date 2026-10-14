// Package ansiblepause is the rule pause-without-register: an ansible.builtin.pause task under
// playbooks/ or tasks/ must register the answer the play uses or carry a "# PAUSE-OK: <reason>"
// comment, because a pause nobody knows is owed hangs an unattended batch. The scan is
// line-based, not a YAML load, because the annotation is a comment. The specification is the
// plan's SPEC-qa-ansible-pause.md.
package ansiblepause

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootpath"
)

// ExitCode is the status qa-ansible-pause exits with.
type ExitCode int

// The statuses callers read: 0 clean, 1 a pause task without register or reason, 2 the run could
// not complete.
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// scanDirs are the trees whose YAML is judged.
var scanDirs = []string{"playbooks/", "tasks/"}

// Env is everything the command touches outside the repository tree.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer
	// Cwd is the default root when neither --root nor --scripts is given.
	Cwd string
	// Resolve makes a root free of symlinks (filepath.EvalSymlinks; a test on a fake tree gives
	// its own).
	Resolve rootpath.Eval
	// Open returns the file system rooted at a directory.
	Open func(root string) fs.FS
	// Yaml lists the YAML files of the shared discovery, as slash paths relative to the root.
	Yaml func(root string) ([]string, error)
}

const usageText = `usage: qa-ansible-pause [--root DIR] [--scripts DIR] [PATH...]

Rule pause-without-register: an ansible.builtin.pause task under playbooks/ or tasks/ must
register: its answer or carry a '# PAUSE-OK: <reason>' comment. With PATH arguments only those
files (relative to the root) are scanned.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

Exit codes: 0 clean, 1 a pause without register or reason, 2 bad usage or the run could not
complete. stdout is the count line 'passed: N failed: M'; findings go to stderr.
`

// options is the parsed command line.
type options struct {
	help    bool
	root    string
	scripts string
	paths   []string
}

// unknownArgError is an argument the tool does not know; the usage text follows it.
type unknownArgError struct{ arg string }

func (e *unknownArgError) Error() string { return fmt.Sprintf("unknown argument '%s'", e.arg) }

func parseArgs(args []string) (options, error) {
	var opts options
	rest := args
	for len(rest) > 0 {
		arg := rest[0]
		rest = rest[1:]
		switch {
		case arg == "-h" || arg == "--help":
			opts.help = true
		case arg == "--root" || arg == "--scripts":
			if len(rest) == 0 || rest[0] == "" || rest[0][0] == '-' {
				return options{}, fmt.Errorf("%s needs a directory", arg)
			}
			if arg == "--root" {
				opts.root = rest[0]
			} else {
				opts.scripts = rest[0]
			}
			rest = rest[1:]
		case strings.HasPrefix(arg, "-"):
			return options{}, &unknownArgError{arg: arg}
		default:
			opts.paths = append(opts.paths, arg)
		}
	}
	return opts, nil
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

// Main runs the command and returns the exit status.
func Main(args []string, env Env) ExitCode {
	out, errOut := &emitter{w: env.Stdout}, &emitter{w: env.Stderr}
	code := run(args, env, out, errOut)
	if out.err != nil {
		errOut.line("ERROR: cannot write to stdout: " + out.err.Error())
		return ExitCannotRun
	}
	if code != ExitCannotRun && errOut.err != nil {
		return ExitCannotRun
	}
	return code
}

func run(args []string, env Env, out, errOut *emitter) ExitCode {
	opts, err := parseArgs(args)
	if err != nil {
		errOut.line("ERROR: " + err.Error())
		if unknown, isUnknown := errors.AsType[*unknownArgError](err); isUnknown && unknown != nil {
			for line := range strings.SplitSeq(strings.TrimSuffix(usageText, "\n"), "\n") {
				errOut.line(line)
			}
		}
		return ExitCannotRun
	}
	if opts.help {
		out.line(strings.TrimSuffix(usageText, "\n"))
		return ExitOK
	}
	root, err := rootpath.RootVia(env.Resolve, env.Cwd, opts.root, opts.scripts)
	if err != nil {
		errOut.line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	targets, err := population(env, root, opts.paths)
	if err != nil {
		errOut.line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	return judge(env.Open(root), targets, out, errOut)
}

// population is the files to scan: the PATH arguments, else the discovered YAML under the scanned
// trees. Sorted, no duplicates; an empty population is an error.
func population(env Env, root string, paths []string) ([]string, error) {
	targets, err := candidates(env, root, paths)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no YAML found under %s; nothing was checked", strings.Join(scanDirs, " or "))
	}
	slices.Sort(targets)
	return slices.Compact(targets), nil
}

// candidates are the PATH arguments made relative to the root, else the discovered YAML files
// under the scanned trees.
func candidates(env Env, root string, paths []string) ([]string, error) {
	var targets []string
	for _, p := range paths {
		rel, err := relative(root, p)
		if err != nil {
			return nil, err
		}
		targets = append(targets, rel)
	}
	if len(paths) > 0 {
		return targets, nil
	}
	listed, err := env.Yaml(root)
	if err != nil {
		return nil, err
	}
	for _, p := range listed {
		underDir := slices.ContainsFunc(scanDirs, func(dir string) bool { return strings.HasPrefix(p, dir) })
		if ext := path.Ext(p); underDir && (ext == ".yml" || ext == ".yaml") {
			targets = append(targets, p)
		}
	}
	return targets, nil
}

// relative turns a PATH argument into a slash path under the root; one outside it is an error.
func relative(root, p string) (string, error) {
	rel := p
	if filepath.IsAbs(p) {
		var err error
		if rel, err = filepath.Rel(root, p); err != nil {
			return "", fmt.Errorf("path %s is outside the repository root", p)
		}
	}
	rel = path.Clean(filepath.ToSlash(rel))
	if rel == ".." || strings.HasPrefix(rel, "../") || path.IsAbs(rel) {
		return "", fmt.Errorf("path %s is outside the repository root", p)
	}
	return rel, nil
}

// judge scans every target and reports: findings and the summary on stderr, the count line on
// stdout.
func judge(fsys fs.FS, targets []string, out, errOut *emitter) ExitCode {
	total, files := 0, 0
	unreadable := false
	for _, rel := range targets {
		data, err := fs.ReadFile(fsys, rel)
		if err != nil {
			errOut.line(fmt.Sprintf("ERROR: cannot read %s: %v", rel, err))
			unreadable = true
			continue
		}
		findings := Scan(string(data))
		for _, f := range findings {
			errOut.line(fmt.Sprintf("%s:%d: pause-without-register", rel, f.Line))
		}
		total += len(findings)
		if len(findings) > 0 {
			files++
		}
	}
	if unreadable {
		return ExitCannotRun
	}
	if total > 0 {
		errOut.line(fmt.Sprintf("pause-without-register: %d pause task(s) in %d file(s) wait with no register and no '# PAUSE-OK: <reason>'. "+
			"Use ansible.builtin.debug for status. See CLAUDE/QA.md \"pause-without-register\".", total, files))
	}
	out.line(fmt.Sprintf("passed: %d failed: %d", len(targets)-files, files))
	if total > 0 {
		return ExitFailed
	}
	return ExitOK
}
