package changed

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// Main is the qa-changed command: it returns the exit status. Diagnostics go to env.Stderr,
// the plan and the report to env.Stdout.
func Main(ctx context.Context, args []string, env Env) ExitCode {
	bare := &session{env: env}
	opts, err := parseArgs(args)
	if err != nil {
		bare.errf("ERROR: %v\n", err)
		if usage, listed := errors.AsType[*usageError](err); listed && usage.listUsage {
			bare.errf("%s", usageText)
		}
		return ExitCannotRun
	}
	if opts.help {
		bare.outf("%s", usageText)
		return bare.settle(ExitOK)
	}
	s, err := newSession(env, opts)
	if err != nil {
		bare.errf("ERROR: %v\n", err)
		return ExitCannotRun
	}
	defer s.cleanup()
	return s.settle(s.run(ctx))
}

// outf and errf write to the two streams, remembering the first write that failed.
func (s *session) outf(format string, args ...any) {
	s.noteWrite(fmt.Fprintf(s.env.Stdout, format, args...))
}

func (s *session) errf(format string, args ...any) {
	s.noteWrite(fmt.Fprintf(s.env.Stderr, format, args...))
}

func (s *session) noteWrite(_ int, err error) {
	if s.writeErr == nil {
		s.writeErr = err
	}
}

// settle turns a pass into "could not run" when the report could not be written.
func (s *session) settle(code ExitCode) ExitCode {
	if code == ExitOK && s.writeErr != nil {
		return ExitCannotRun
	}
	return code
}

// resolvePaths settles the repository root and the scripts directory: --root, then
// QA_CHANGED_ROOT, then the parent of the scripts directory, then the working directory; the
// scripts directory is --scripts or <root>/scripts.
func resolvePaths(opts options, getenv func(string) string, cwd string) (string, string) {
	root := opts.root
	if root == "" {
		root = getenv("QA_CHANGED_ROOT")
	}
	if root == "" && opts.scripts != "" {
		root = filepath.Dir(opts.scripts)
	}
	if root == "" {
		root = cwd
	}
	scripts := opts.scripts
	if scripts == "" {
		scripts = filepath.Join(root, "scripts")
	}
	return root, scripts
}

func newSession(env Env, opts options) (*session, error) {
	root, scripts := resolvePaths(opts, env.Getenv, env.Cwd)
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("root %s is not a directory", root)
	}
	// Absolute, because commands run in other directories (the semgrep mirror, extensions/) and
	// are handed paths built from these.
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve root %s: %w", root, err)
	}
	scripts, err = filepath.Abs(scripts)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve scripts directory %s: %w", scripts, err)
	}
	timeout, err := parseTimeout(env.Getenv("QA_CHANGED_TIMEOUT"))
	if err != nil {
		return nil, err
	}
	s := &session{env: env, opts: opts, root: root, scripts: scripts, timeout: timeout}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// run is everything after the command line: collect, classify, plan, preflight, execute.
func (s *session) run(ctx context.Context) ExitCode {
	files, err := s.collect(ctx)
	if err != nil {
		s.errf("ERROR: %v\n", err)
		return ExitCannotRun
	}
	if len(files.existing)+len(files.deleted) == 0 {
		s.outf("qa-changed: no changed files (%s) — nothing to check.\n", s.describeSource())
		return ExitOK
	}
	if err := s.classify(ctx, files); err != nil {
		s.errf("ERROR: %v\n", err)
		return ExitCannotRun
	}
	if err := s.selectSuites(ctx); err != nil {
		s.errf("ERROR: %v\n", err)
		return ExitCannotRun
	}
	s.printPlan()
	if s.opts.planOnly {
		return ExitOK
	}
	if s.preflight() != nil {
		return ExitCannotRun
	}
	return s.execute(ctx)
}

func (s *session) collect(ctx context.Context) (collected, error) {
	if s.opts.explicit {
		return s.collectExplicit(s.opts.files)
	}
	return s.collectGit(ctx, s.opts.base)
}

// describeSource says where the changed files came from; an explicit list never reaches the
// nothing-to-check line, because a list naming no existing file is an error.
func (s *session) describeSource() string {
	return "vs " + s.opts.base + ", plus untracked"
}

func (s *session) printPlan() {
	p := s.plan
	total := len(p.existing) + len(p.deleted)
	if s.opts.explicit {
		s.outf("plan: %d changed file(s) (explicit)\n", total)
	} else {
		s.outf("plan: %d changed file(s) vs %s\n", total, s.opts.base)
	}
	for _, d := range checkDefs {
		cp := p.checks[d.id]
		switch {
		case cp == nil:
		case d.listed:
			s.outf("check %s: %s\n", d.id, strings.Join(cp.items, " "))
		default:
			s.outf("check %s\n", d.id)
		}
	}
	for _, suite := range p.suites {
		s.outf("suite %s: %s\n", suite.name, strings.Join(suite.why, "; "))
	}
	for _, f := range p.skipped {
		s.outf("skipped (excluded tree): %s\n", f)
	}
	for _, note := range p.notes {
		s.outf("note: %s\n", note)
	}
	for _, f := range p.uncovered {
		s.outf("⚠ not covered by qa-changed: %s\n", f)
	}
}

// pythonPlay is the fix for the tools the Python QA toolchain play installs.
const pythonPlay = "Deploy it: ansible-playbook playbooks/imports/play-python.yml"

// toolFix says how a missing tool is put right: always through the repository's IaC.
var toolFix = map[string]string{
	toolShellcheck: "Deploy it: ansible-playbook playbooks/imports/play-devtools.yml",
	toolSemgrep:    pythonPlay,
	toolRuff:       pythonPlay,
	toolPython:     pythonPlay,
	toolGolangci:   pythonPlay,
	toolGo:         "Deploy it: ansible-playbook playbooks/imports/play-golang.yml",
	toolAnsible:    "Install ansible (sudo dnf install ansible) via the repo's IaC.",
	toolNode:       "Deploy it: ansible-playbook playbooks/imports/play-nvm-install.yml",
	toolBash:       "bash is part of the base system; this host is broken.",
}

// preflight checks, once and before any check runs, every tool the planned checks need; the
// first one missing is reported with how to get it and the run does not start.
func (s *session) preflight() error {
	for _, d := range checkDefs {
		cp := s.plan.checks[d.id]
		if cp == nil {
			continue
		}
		for _, tool := range d.tools {
			if tool == eslintTool {
				if err := s.preflightEslint(cp); err != nil {
					return err
				}
				continue
			}
			if err := s.env.LookPath(tool); err != nil {
				s.errf("ERROR: %s not found — a planned check needs it.\n  %s\n  Do NOT install it by hand.\n", tool, toolFix[tool])
				return fmt.Errorf("%s not found: %w", tool, err)
			}
		}
	}
	return nil
}

// executableBits are the permission bits that make a file runnable by anyone.
const executableBits = 0o111

func (s *session) preflightEslint(cp *checkPlan) error {
	bin := filepath.Join(s.root, cp.rel, "node_modules", ".bin", "eslint")
	if info, err := os.Stat(bin); err != nil || info.Mode()&executableBits == 0 {
		s.errf("ERROR: eslint for %s/ is not set up (cd %s && npm ci).\n", cp.rel, cp.rel)
		return errors.New("eslint is not set up")
	}
	return nil
}

// outcome is how one check ended, for the report and the verdict.
type outcome int

const (
	passed outcome = iota
	failed
	cannotRun
	timedOut
	interrupted
)

// timed runs one check under the per-check timeout. The error from the check function is a
// command that could not start, which is a failed check carrying the reason.
func (s *session) timed(ctx context.Context, gate bool, fn func(context.Context) (checkResult, error)) (checkResult, outcome) {
	cctx := ctx
	if s.timeout > 0 {
		var cancel context.CancelFunc
		cctx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}
	res, err := fn(cctx)
	switch {
	case ctx.Err() != nil:
		return res, interrupted
	case errors.Is(cctx.Err(), context.DeadlineExceeded):
		return res, timedOut
	case err != nil:
		return checkResult{output: err.Error(), code: 1}, failed
	case gate && res.code == int(ExitCannotRun):
		return res, cannotRun
	case res.code != 0:
		return res, failed
	}
	return res, passed
}

// report prints one check's line, and a failure's captured output on stderr.
func (s *session) report(name, summary string, res checkResult, o outcome) {
	if o == passed {
		s.outf("✓ %s: %s\n", name, summary)
		return
	}
	suffix := ""
	switch o {
	case cannotRun:
		suffix = fmt.Sprintf(" — could not run (exit %d)", ExitCannotRun)
	case timedOut:
		suffix = " — timed out after " + s.timeout.String()
	case passed, failed, interrupted:
	}
	s.outf("✗ %s: %s%s\n", name, summary, suffix)
	s.errf("%s\n", strings.TrimRight(res.output, "\n"))
}

// execute runs every planned check in order, never stopping at a failure, then the suites,
// and prints the verdict. A failed check outranks one that could not run.
func (s *session) execute(ctx context.Context) ExitCode {
	var failures, cannot []string
	record := func(name string, o outcome) {
		switch o {
		case failed, timedOut:
			failures = append(failures, name)
		case cannotRun:
			cannot = append(cannot, name)
		case passed, interrupted:
		}
	}
	for _, d := range checkDefs {
		cp := s.plan.checks[d.id]
		if cp == nil {
			continue
		}
		res, o := s.timed(ctx, d.gate, func(c context.Context) (checkResult, error) { return d.run(c, s, cp) })
		if o == interrupted {
			return s.interrupted()
		}
		s.report(d.id, d.summaryFor(cp), res, o)
		record(d.id, o)
	}
	for _, suite := range s.plan.suites {
		res, o := s.timed(ctx, true, func(c context.Context) (checkResult, error) {
			return s.script(c, nil, strings.TrimPrefix(suite.name, "scripts/"))
		})
		if o == interrupted {
			return s.interrupted()
		}
		name, reasons := "suite "+strings.TrimPrefix(suite.name, "scripts/"), "("+strings.Join(suite.why, "; ")+")"
		if o == passed {
			count, counted := gateCaseCount(res.output)
			if counted {
				reasons = count + " " + reasons
			} else {
				o = cannotRun
				res.output = "the suite exited 0 but printed no 'passed: <n>' line, so it proved nothing\n" + res.output
			}
		}
		s.report(name, reasons, res, o)
		record(name, o)
	}
	return s.verdict(failures, cannot)
}

func (s *session) interrupted() ExitCode {
	s.outf("✗ qa-changed interrupted\n")
	return ExitInterrupted
}

func (s *session) verdict(failures, cannot []string) ExitCode {
	if n := len(s.plan.uncovered); n > 0 {
		s.outf("⚠ %d changed file(s) not covered by qa-changed — qa-all.bash is the only check\n", n)
	}
	if len(failures)+len(cannot) == 0 {
		s.outf("✓ qa-changed passed: %d changed file(s). Run ./scripts/qa-all.bash before pushing.\n", len(s.plan.existing)+len(s.plan.deleted))
		return ExitOK
	}
	s.outf("✗ qa-changed FAILED: %s\n", strings.Join(orderedNames(s.plan, failures, cannot), " "))
	if len(failures) > 0 {
		return ExitFailed
	}
	return ExitCannotRun
}

// orderedNames lists the failed and could-not-run checks in the order they ran.
func orderedNames(p *plan, failures, cannot []string) []string {
	var names []string
	for _, d := range checkDefs {
		if p.checks[d.id] != nil && (slices.Contains(failures, d.id) || slices.Contains(cannot, d.id)) {
			names = append(names, d.id)
		}
	}
	for _, suite := range p.suites {
		name := "suite " + strings.TrimPrefix(suite.name, "scripts/")
		if slices.Contains(failures, name) || slices.Contains(cannot, name) {
			names = append(names, name)
		}
	}
	return names
}

// tempDir and tempFile make scratch paths that cleanup removes on every exit path.
func (s *session) tempDir() (string, error) {
	dir, err := os.MkdirTemp("", "qa-changed-")
	if err != nil {
		return "", fmt.Errorf("cannot make a scratch directory: %w", err)
	}
	s.temps = append(s.temps, dir)
	return dir, nil
}

func (s *session) tempFile() (string, error) {
	f, err := os.CreateTemp("", "qa-changed-")
	if err != nil {
		return "", fmt.Errorf("cannot make a scratch file: %w", err)
	}
	name := f.Name()
	s.temps = append(s.temps, name)
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("cannot close %s: %w", name, err)
	}
	return name, nil
}

func (s *session) cleanup() {
	for _, path := range s.temps {
		if err := os.RemoveAll(path); err != nil {
			s.errf("qa-changed: cannot remove %s: %v\n", path, err)
		}
	}
	s.temps = nil
}

// runCmd starts a command through the process seam, naming the program when it cannot start.
func (s *session) runCmd(ctx context.Context, cmd proc.Cmd) (proc.Result, error) {
	res, err := s.env.Runner.Run(ctx, cmd)
	if err != nil {
		return proc.Result{}, fmt.Errorf("cannot run %s: %w", cmd.Argv[0], err)
	}
	return res, nil
}
