package all

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/gates"
)

// session is one run: the gates to run, what has failed so far, and the files it owns.
type session struct {
	env     Env
	opts    options
	root    string
	scripts string
	timeout time.Duration
	// results is where the machine report goes.
	results string
	gates   []gates.Gate
	// failedGates and failedStages are the names that failed, in run order. A hard gate's
	// verdict lives in its stage line and here; a stage's in its report and here.
	failedGates  []string
	failedStages []string
	reports      []namedReport
	temps        []string
	writeErr     error
}

// Main is the qa-all command: it returns the exit status. Diagnostics go to env.Stderr, the
// stage lines and the verdict to env.Stdout.
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

// settle turns a pass into "could not run" when the verdict could not be written.
func (s *session) settle(code ExitCode) ExitCode {
	if code == ExitOK && s.writeErr != nil {
		return ExitCannotRun
	}
	return code
}

// resolvePaths settles the repository root and the scripts directory: --root, then the parent
// of --scripts, then the working directory; the scripts directory is --scripts or
// <root>/scripts.
func resolvePaths(opts options, cwd string) (string, string) {
	root := opts.root
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

// reportPath is where the machine report goes: QA_RESULTS_JSON, else the path CI uploads.
func reportPath(getenv func(string) string) string {
	if path := getenv("QA_RESULTS_JSON"); path != "" {
		return path
	}
	return defaultReportPath
}

func newSession(env Env, opts options) (*session, error) {
	root, scripts := resolvePaths(opts, env.Cwd)
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("root %s is not a directory", root)
	}
	// Absolute, because the gates run in the root and are handed paths built from it.
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve root %s: %w", root, err)
	}
	timeout, err := parseTimeout(env.Getenv("QA_ALL_TIMEOUT"))
	if err != nil {
		return nil, err
	}
	table, err := gates.Load(filepath.Join(scripts, "qa-gates.tsv"))
	if err != nil {
		return nil, err
	}
	if err := gates.CheckScripts(root, table); err != nil {
		return nil, err
	}
	return &session{env: env, opts: opts, root: root, scripts: scripts, timeout: timeout, results: reportPath(env.Getenv), gates: table}, nil
}

// run is everything after the command line: clear the last report, run every gate in table
// order, write the report, give the verdict.
func (s *session) run(ctx context.Context) ExitCode {
	// A run that ends before the report is written must not leave the last run's report to be
	// read as its own.
	if err := os.Remove(s.results); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.errf("ERROR: cannot clear the previous report %s: %v\n", s.results, err)
		return ExitCannotRun
	}
	for _, g := range s.gates {
		if code, stop := s.runGate(ctx, g); stop {
			return code
		}
	}
	return s.finish()
}

// runGate runs one gate by its kind. It reports whether the run must stop here: a stage that
// cannot run its tools, or an interrupt.
func (s *session) runGate(ctx context.Context, g gates.Gate) (ExitCode, bool) {
	switch g.Kind {
	case gates.KindStage:
		if code, stop := s.runStage(ctx, g); stop {
			return code, true
		}
	case gates.KindHelpers:
		s.runHelpers(ctx, g)
	case gates.KindDrift:
		s.runDrift(ctx, g)
	case gates.KindGate:
		s.runHard(ctx, g)
	}
	if ctx.Err() != nil {
		s.outf("✗ qa-all interrupted\n")
		return ExitInterrupted, true
	}
	return ExitOK, false
}

// finish writes the machine report and gives the verdict.
func (s *session) finish() ExitCode {
	failed := len(s.failedGates)+len(s.failedStages) > 0
	status := "pass"
	if failed {
		status = "fail"
	}
	body, totals, err := mergeReports(status, s.reports)
	if err == nil {
		err = writeReport(s.results, body)
	}
	if err != nil {
		s.errf("ERROR: cannot write the report: %v\n", err)
		return ExitCannotRun
	}
	if !failed {
		s.outf("✓ QA passed: %d files checked\n", totals.Total)
		return ExitOK
	}
	s.outf("✗ QA FAILED: %d errors in %d files\n", totals.Failed, totals.Total)
	// Every gate ran, so these lists are the whole of what is broken, not the first thing that
	// happened to break.
	if n := len(s.failedGates); n > 0 {
		s.outf("  %d gate(s) failed: %s\n", n, strings.Join(s.failedGates, " "))
	}
	if n := len(s.failedStages); n > 0 {
		s.outf("  %d stage(s) failed: %s\n", n, strings.Join(s.failedStages, " "))
	}
	s.outf("  Details: jq '.failures[]' %s\n", s.results)
	return ExitFailed
}

// tempFile makes an empty scratch file that cleanup removes on every exit path.
func (s *session) tempFile() (string, error) {
	f, err := os.CreateTemp("", "qa-all-")
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
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.errf("qa-all: cannot remove %s: %v\n", path, err)
		}
	}
	s.temps = nil
}
