// Package discovery is the one definition of which files the QA gates read: the repo-owned shell
// scripts, Python files and Python templates git would publish, and the tracked shell scripts,
// Python files, playbooks and helper tests a gate measures its own coverage against. The
// specification is the plan's SPEC-qa-discovery.md.
package discovery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// ExitCode is the status qa-discovery exits with.
type ExitCode int

// The statuses callers read: 0 the list is on stdout, 2 bad usage or the list could not be made.
const (
	ExitOK        ExitCode = 0
	ExitCannotRun ExitCode = 2
)

// treesFile names the trees the gates must not gate on, relative to the scripts directory.
const treesFile = "qa-trees.tsv"

// Env is everything the command touches outside the repository tree.
type Env struct {
	Stdout, Stderr io.Writer
	// Cwd is the default root when neither --root nor --scripts is given.
	Cwd string
	// Runner starts git.
	Runner proc.Runner
}

const usageText = `usage: qa-discovery [--root DIR] [--scripts DIR] LIST

Prints one list of files, each ended by NUL, sorted. LIST is one of:

  shell               scripts for the shell that git would publish (tracked, or untracked and not
                      ignored); absolute
  python              Python files, likewise; absolute
  python-templates    .j2 templates with a Python shebang, likewise; absolute; may be empty
  tracked-shell       tracked shell scripts; repository-relative
  tracked-python      tracked Python files; repository-relative
  tracked-playbooks   tracked .yml/.yaml files under playbooks/; repository-relative
  tracked-helper-tests  tracked tests/helpers/**/test_*.py; repository-relative

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory, which holds qa-trees.tsv; the root is its parent
  -h, --help      this text

Exit codes: 0 the list, 2 bad usage or the list could not be made (including an empty one, except
python-templates). stdout is the list; diagnostics go to stderr.
`

// options is the parsed command line.
type options struct {
	help    bool
	root    string
	scripts string
	list    string
}

// parseArgs reads the command line; anything unknown is a usage error.
func parseArgs(args []string) (options, error) {
	var opts options
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "-h" || arg == "--help":
			opts.help = true
		case arg == "--root" || arg == "--scripts":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s needs a value", arg)
			}
			i++
			if arg == "--root" {
				opts.root = args[i]
			} else {
				opts.scripts = args[i]
			}
		case strings.HasPrefix(arg, "-"):
			return opts, fmt.Errorf("unknown argument '%s'", arg)
		case opts.list != "":
			return opts, fmt.Errorf("one list at a time: got '%s' and '%s'", opts.list, arg)
		default:
			opts.list = arg
		}
	}
	if opts.help {
		return opts, nil
	}
	if opts.list == "" {
		return opts, errors.New("a list is needed")
	}
	if _, known := listings[opts.list]; !known {
		return opts, fmt.Errorf("unknown list '%s'", opts.list)
	}
	return opts, nil
}

// rootDir resolves the repository root.
func (o options) rootDir(cwd string) string {
	switch {
	case o.root != "":
		return strings.TrimRight(o.root, "/")
	case o.scripts != "":
		return filepath.Dir(filepath.Clean(o.scripts))
	}
	return cwd
}

// scriptsDir resolves the scripts directory under a root.
func (o options) scriptsDir(root string) string {
	if o.scripts != "" {
		return o.scripts
	}
	return filepath.Join(root, "scripts")
}

// listing is one list the command can print.
type listing struct {
	// what the list is, for the message when it is empty.
	what string
	// published is that the source is the files git would publish, else the tracked ones.
	published bool
	// absolute is that paths are printed under the root, else relative to it.
	absolute bool
	// mayBeEmpty is that an empty list is a list.
	mayBeEmpty bool
	// dirs picks the excluded trees; nil excludes nothing.
	dirs func(trees) []string
	// keep decides one path.
	keep func(f finder, rel string) (bool, error)
}

func shellDirs(t trees) []string  { return t.shell }
func pythonDirs(t trees) []string { return t.python }

func ofKind(lang language) func(finder, string) (bool, error) {
	return func(f finder, rel string) (bool, error) { return f.isOfKind(rel, lang) }
}

// fileUnder keeps the regular files under a directory that pass the test on their name.
func fileUnder(dir string, name func(base string) bool) func(finder, string) (bool, error) {
	return func(f finder, rel string) (bool, error) {
		if !strings.HasPrefix(rel, dir) || !name(path.Base(rel)) {
			return false, nil
		}
		return f.regular(rel, true)
	}
}

var listings = map[string]listing{
	"shell":             {what: "shell scripts", published: true, absolute: true, dirs: shellDirs, keep: ofKind(shellLang)},
	"python":            {what: "Python files", published: true, absolute: true, dirs: pythonDirs, keep: ofKind(pythonLang)},
	"python-templates":  {what: "Python templates", published: true, absolute: true, mayBeEmpty: true, dirs: pythonDirs, keep: ofKind(templateLang)},
	"tracked-shell":     {what: "tracked shell scripts", dirs: shellDirs, keep: ofKind(shellLang)},
	"tracked-python":    {what: "tracked Python files", dirs: pythonDirs, keep: ofKind(pythonLang)},
	"tracked-playbooks": {what: "tracked playbooks", keep: fileUnder("playbooks/", func(b string) bool { return path.Ext(b) == ".yml" || path.Ext(b) == ".yaml" })},
	"tracked-helper-tests": {what: "tracked helper test modules", keep: fileUnder("tests/helpers/", func(b string) bool {
		return strings.HasPrefix(b, "test_") && path.Ext(b) == ".py"
	})},
}

// Main runs the command and returns the exit status.
func Main(args []string, env Env) ExitCode {
	opts, err := parseArgs(args)
	if err != nil {
		return fail(env, err, true)
	}
	if opts.help {
		return write(env, usageText)
	}
	root := opts.rootDir(env.Cwd)
	paths, err := list(context.Background(), env, root, opts.scriptsDir(root), listings[opts.list])
	if err != nil {
		return fail(env, err, false)
	}
	var out strings.Builder
	for _, rel := range paths {
		if listings[opts.list].absolute {
			rel = root + "/" + rel
		}
		out.WriteString(rel + "\x00")
	}
	return write(env, out.String())
}

// list makes one list: the source, less the excluded trees, the kept paths; sorted.
func list(ctx context.Context, env Env, root, scripts string, l listing) ([]string, error) {
	t, err := loadTrees(filepath.Join(scripts, treesFile))
	if err != nil {
		return nil, err
	}
	f := finder{root: root, runner: env.Runner}
	files, err := f.files(ctx, l.published)
	if err != nil {
		return nil, err
	}
	if l.dirs != nil {
		dirs := l.dirs(t)
		files = slices.DeleteFunc(files, func(rel string) bool { return excluded(rel, dirs) })
	}
	paths, err := pick(files, func(rel string) (bool, error) { return l.keep(f, rel) })
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 && !l.mayBeEmpty {
		return nil, fmt.Errorf("found 0 %s under %s: this repository always has some, so discovery is broken, and a pass here would vouch for code nothing had read", l.what, root)
	}
	return paths, nil
}

// write sends text to stdout; the status is that of the write.
func write(env Env, text string) ExitCode {
	if _, err := io.WriteString(env.Stdout, text); err != nil {
		return fail(env, fmt.Errorf("cannot write to stdout: %w", err), false)
	}
	return ExitOK
}

// Fail reports an error the command could not run for, before Main could be called.
func Fail(stderr io.Writer, err error) ExitCode {
	return fail(Env{Stderr: stderr}, err, false)
}

// fail prints an ERROR line (and the usage, for a usage error) on stderr. A stderr that cannot
// be written to has nowhere left to say so; the status is the same either way.
func fail(env Env, err error, usage bool) ExitCode {
	text := "ERROR: " + err.Error() + "\n"
	if usage {
		text += usageText
	}
	return statusAfter(io.WriteString(env.Stderr, text))
}

// statusAfter is the status of a run that could not complete, whether or not its explanation
// could be written.
func statusAfter(int, error) ExitCode { return ExitCannotRun }
