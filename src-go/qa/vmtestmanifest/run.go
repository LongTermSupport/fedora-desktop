// Package vmtestmanifest is the Plan 00110 gate for the VM-test scenario manifest,
// vars/vm-test-scenarios.yml, which is the source of the acceptance bridge's scenario allowlist.
// The manifest is read as YAML, handed as JSON to the validator the host itself runs
// (helpers/vmtest/validate_manifest.py, so there is one definition of a valid manifest), and
// cross-checked against the guest scripts: every runnable scenario's planned count equals the
// PLANNED its guest checker declares, and every fixture runs before a declared reboot. Exit 1
// says the repository is wrong; exit 2 says the gate could not judge. The specification is the
// plan's SPEC-qa-vmtest-manifest.md.
package vmtestmanifest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/emit"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootargs"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootpath"
)

// ExitCode is the status qa-vmtest-manifest exits with.
type ExitCode int

// The statuses callers read: 0 valid and in agreement, 1 the manifest or a guest script is wrong,
// 2 the gate could not judge.
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// The inputs, relative to the repository root.
const (
	manifestFile = "vars/vm-test-scenarios.yml"
	versionFile  = "vars/fedora-version.yml"
	guestDir     = "files/home/.local/share/vmtest/"
)

// The options that take a value.
const (
	flagRoot    = "--root"
	flagScripts = "--scripts"
)

// defaultTimeout bounds the validator run when Env.Timeout is zero.
const defaultTimeout = time.Minute

// Env is everything the command touches outside the repository tree.
type Env struct {
	Stdout, Stderr io.Writer
	// Cwd is the default root when neither --root nor --scripts is given.
	Cwd string
	// Runner starts python3 (the validator) and, through qa/discovery, git.
	Runner proc.Runner
	// Timeout bounds the validator run; zero is defaultTimeout.
	Timeout time.Duration
}

const usageText = `usage: qa-vmtest-manifest [--root DIR] [--scripts DIR]

Plan 00110: vars/vm-test-scenarios.yml is valid (the host's own validator, helpers/vmtest), every
runnable scenario's planned count equals the PLANNED of its guest checker, and every
guest-prepare-<scenario>.bash fixture runs before a declared reboot.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

Exit codes: 0 valid and in agreement, 1 the manifest or a guest script is wrong, 2 bad usage or
the run could not complete (an input unread or unparsed, python3 missing).
`

// options is the parsed command line.
type options struct {
	help    bool
	root    string
	scripts string
}

func parseArgs(args []string) (options, error) {
	parsed, err := rootargs.Strict(args)
	return options{help: parsed.Help, root: parsed.Root, scripts: parsed.Scripts}, err
}

// Main runs the command and returns the exit status.
func Main(args []string, env Env) ExitCode {
	out, errOut := emit.New(env.Stdout), emit.New(env.Stderr)
	return emit.Finish(out, errOut, run(args, env, out, errOut), ExitCannotRun)
}

func run(args []string, env Env, out, errOut *emit.Emitter) ExitCode {
	opts, err := parseArgs(args)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		if unknown, isUnknown := errors.AsType[*rootargs.UnknownArgError](err); isUnknown && unknown != nil {
			errOut.Block(usageText)
		}
		return ExitCannotRun
	}
	if opts.help {
		out.Block(usageText)
		return ExitOK
	}
	root, err := rootpath.Root(env.Cwd, opts.root, opts.scripts)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	g := &gate{env: env, root: root, scripts: filepath.Join(root, "scripts")}
	if opts.scripts != "" {
		g.scripts = opts.scripts
	}
	rep, err := g.judge()
	switch {
	case err != nil:
		for line := range strings.SplitSeq(g.scrub(err.Error()), "\n") {
			errOut.Line("ERROR: " + line)
		}
		return ExitCannotRun
	case len(rep.findings) > 0:
		for _, finding := range rep.findings {
			errOut.Line("ERROR: " + g.scrub(finding))
		}
		return ExitFailed
	}
	for _, line := range rep.lines {
		out.Line(line)
	}
	return ExitOK
}

// gate is one run.
type gate struct {
	env     Env
	root    string
	scripts string
}

// scrub is text with the checkout path taken out: no checkout path may reach the output.
func (g *gate) scrub(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, g.root+"/", ""), g.root, ".")
}

// judge makes the verdict: the stdout lines of a pass, or the findings of a repository that is
// wrong, or an error for a gate that could not judge.
func (g *gate) judge() (report, error) {
	version, err := g.fedoraVersion()
	if err != nil {
		return report{}, err
	}
	doc, manifestJSON, err := g.loadManifest()
	if err != nil {
		return report{}, err
	}
	verdict, err := g.validate(context.Background(), version, manifestJSON)
	if err != nil {
		return report{}, err
	}
	if verdict.invalid != nil {
		return report{findings: verdict.invalid}, nil
	}
	m, err := readShape(doc)
	if err != nil {
		return report{}, err
	}
	found, err := g.guestScripts()
	if err != nil {
		return report{}, err
	}
	agree, planned, err := g.checkPlanned(m, found)
	if err != nil {
		return report{}, err
	}
	fixtures, fixtureFindings := checkFixtures(m, found)
	prefix := manifestFile + ": "
	return report{
		lines: []string{
			prefix + verdict.counts,
			fmt.Sprintf("%s%d scenario(s) agree with their guest checker", prefix, agree),
			fmt.Sprintf("%s%d scenario fixture(s) run before a declared reboot", prefix, fixtures),
		},
		findings: slices.Concat(planned, fixtureFindings, checkOrphans(m, found)),
	}, nil
}

// report is the outcome of a run that could judge: the stdout lines of a pass, and the findings
// of a repository that is wrong.
type report struct {
	lines    []string
	findings []string
}
