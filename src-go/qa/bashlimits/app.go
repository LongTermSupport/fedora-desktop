package bashlimits

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// Exit codes. ExitUsage matches the main module's ExitUsage, so the gate speaks as the other tools do.
const (
	ExitOK      = 0
	ExitFailure = 1
	ExitUsage   = 64
)

// ExitCannotRun is the exit code for a gate that could not reach a verdict (git failed, a
// file could not be read, the allowlist is missing, malformed or from another measurer), as
// against findings (1).
const ExitCannotRun = 2

// DefaultAllowlist is the allowlist's path, relative to the repository root.
const DefaultAllowlist = "scripts/bash-limits.allowlist"

// allowlistMode is the mode of a written allowlist: a tracked file, world-readable like its
// siblings.
const allowlistMode = 0o644

var usage = fmt.Sprintf(`usage: qa-bash-limits [--base REF] [--update | --remeasure | --seed] [--root DIR] [--allowlist PATH]

Checks every bash file git knows against the limits in src-go/CLAUDE.md (%d code lines, %d
statements, %d functions, no associative arrays, no interactive UI, no data parsing past one
jq call), with the exemptions and the ratchet allowlist (%s).

  --base REF    compare the allowlist with REF's copy and fail on any entry added or record
                raised (default: the merge-base of HEAD and origin's default branch, so
                commits that are not pushed yet are compared too)
  --update      lower records to the current measurements and drop entries that are within
                limits or whose file is gone; never raises a record, adds an entry, or drops
                one whose file still exists
  --remeasure   rewrite every record at the current measure; only when the measurer version
                in the code is newer than the allowlist's, in the commit that changes it
  --seed        write the first allowlist from the current tree; refuses if it exists
  --root DIR    the repository root (default: the git top level of the working directory)
  --allowlist   the allowlist path relative to the root (default %s)
`, MaxCodeLines, MaxStmts, MaxFuncs, DefaultAllowlist, DefaultAllowlist)

// options are the parsed command line.
type options struct {
	base, root, allowlist   string
	update, remeasure, seed bool
}

// sink is a writer that remembers its first failed write, so the output can be written
// plainly and the failure reported once.
type sink struct {
	w   io.Writer
	err error
}

func (s *sink) printf(format string, args ...any) {
	if s.err == nil {
		_, s.err = fmt.Fprintf(s.w, format, args...)
	}
}

// App is the qa-bash-limits command: git through Git, the verdict on Out, diagnostics on Err.
type App struct {
	Git Git
	Out io.Writer
	Err io.Writer
}

// session is one run of the command.
type session struct {
	git  Git
	out  sink
	err  sink
	repo *os.Root
	opts options
}

// Run executes the command in cwd and returns its exit code.
func (a *App) Run(ctx context.Context, cwd string, args []string) int {
	s := &session{git: a.Git, out: sink{w: a.Out}, err: sink{w: a.Err}}
	code := s.run(ctx, cwd, args)
	if s.out.err != nil || s.err.err != nil {
		return ExitCannotRun
	}
	return code
}

func (s *session) run(ctx context.Context, cwd string, args []string) int {
	opts, code, done := s.parse(args)
	if done {
		return code
	}
	s.opts = opts
	root := opts.root
	if root == "" {
		top, err := gitRepo{s.git, cwd}.topLevel(ctx)
		if err != nil {
			return s.cannotRun(err)
		}
		root = top
	}
	repo, err := os.OpenRoot(root)
	if err != nil {
		return s.cannotRun(err)
	}
	s.repo = repo
	code, err = s.check(ctx, root)
	if closeErr := repo.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return s.cannotRun(err)
	}
	return code
}

func (s *session) cannotRun(err error) int {
	s.err.printf("✗ bash-limits: %v\n", err)
	return ExitCannotRun
}

// parse reads the command line; done means the exit code is final (help or a usage error).
func (s *session) parse(args []string) (options, int, bool) {
	var o options
	flags := flag.NewFlagSet("qa-bash-limits", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&o.base, "base", "", "")
	flags.StringVar(&o.root, "root", "", "")
	flags.StringVar(&o.allowlist, "allowlist", DefaultAllowlist, "")
	flags.BoolVar(&o.update, "update", false, "")
	flags.BoolVar(&o.remeasure, "remeasure", false, "")
	flags.BoolVar(&o.seed, "seed", false, "")
	err := flags.Parse(args)
	modes := 0
	for _, on := range []bool{o.update, o.remeasure, o.seed} {
		if on {
			modes++
		}
	}
	switch {
	case errors.Is(err, flag.ErrHelp):
		s.err.printf("%s", usage)
		return o, ExitOK, true
	case err != nil:
		return o, s.usageError(err.Error()), true
	case flags.NArg() > 0:
		return o, s.usageError("unexpected argument " + flags.Arg(0)), true
	case modes > 1:
		return o, s.usageError("--update, --remeasure and --seed are different modes"), true
	case !filepath.IsLocal(o.allowlist):
		return o, s.usageError("--allowlist is a path inside the repository"), true
	}
	return o, ExitOK, false
}

func (s *session) usageError(msg string) int {
	s.err.printf("✗ bash-limits: %s\n\n%s", msg, usage)
	return ExitUsage
}

// check measures the tree and applies the mode; an error means no verdict could be reached.
func (s *session) check(ctx context.Context, root string) (int, error) {
	files, err := gitRepo{s.git, root}.listFiles(ctx)
	if err != nil {
		return 0, err
	}
	scan, err := ScanTree(s.repo.FS(), files)
	if err != nil {
		return 0, err
	}
	if s.opts.seed {
		return s.seedList(scan)
	}
	unshipped, err := s.verifyDaemonOwned(ctx, root, &scan)
	if err != nil {
		return 0, err
	}
	list, err := s.loadAllowlist()
	if err != nil {
		return 0, err
	}
	list, refused, err := s.apply(list, scan)
	if err != nil || len(refused) > 0 {
		s.report(append(unshipped, refused...))
		return ExitFailure, err
	}
	findings := slices.Concat(unshipped, Evaluate(list.Records, scan))
	more, err := s.againstBase(ctx, root, list, scan)
	if err != nil {
		return 0, err
	}
	return s.verdict(append(findings, more...), list, scan), nil
}

// verifyDaemonOwned holds each daemon-owned file in the scan to an upgrade commit that shipped
// it; git is asked only when there is one to prove.
func (s *session) verifyDaemonOwned(ctx context.Context, root string, scan *Scan) ([]Finding, error) {
	if len(scan.DaemonOwned) == 0 {
		return nil, nil
	}
	shipped, err := gitRepo{s.git, root}.upgradeShippedFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading hooks-daemon upgrade commits to prove the daemon-owned files: %w", err)
	}
	return VerifyDaemonOwned(scan, shipped), nil
}

// apply runs the rewriting mode, if any, and returns the allowlist to judge. Findings mean
// the rewrite was refused and nothing was written.
func (s *session) apply(list File, scan Scan) (File, []Finding, error) {
	switch {
	case s.opts.remeasure:
		if MeasurerVersion <= list.Version {
			return list, nil, fmt.Errorf("--remeasure: %s is already at measurer %d and this gate is measurer %d; a remeasure is for the commit that raises MeasurerVersion", s.opts.allowlist, list.Version, MeasurerVersion)
		}
		records, refused := Remeasure(list.Records, scan)
		return s.write(list, File{Version: MeasurerVersion, Records: records}, refused, "--remeasure")
	case list.Version != MeasurerVersion:
		return list, nil, fmt.Errorf("%s was written by measurer %d and this gate is measurer %d: records from another measurer cannot be compared (run scripts/qa-bash-limits.bash --remeasure in the commit that changes the measurer)", s.opts.allowlist, list.Version, MeasurerVersion)
	case s.opts.update:
		records, refused := Tighten(list.Records, scan)
		return s.write(list, File{Version: MeasurerVersion, Records: records}, refused, "--update")
	}
	return list, nil, nil
}

func (s *session) write(old, next File, refused []Finding, mode string) (File, []Finding, error) {
	if len(refused) > 0 {
		return old, refused, nil
	}
	if old.Version == next.Version && next.Records.Format() == old.Records.Format() {
		return old, nil, nil
	}
	if err := s.writeAllowlist(next.Records); err != nil {
		return old, nil, err
	}
	s.err.printf("bash-limits: %s rewrote the allowlist: %d -> %d entries\n", mode, len(old.Records), len(next.Records))
	return next, nil, nil
}

// writeAllowlist writes the file and sets its mode: WriteFile keeps the mode of a file that exists.
func (s *session) writeAllowlist(list Allowlist) error {
	rel := s.opts.allowlist
	if err := s.repo.WriteFile(rel, []byte(list.Format()), allowlistMode); err != nil {
		return fmt.Errorf("write %s: %w", rel, err)
	}
	if err := s.repo.Chmod(rel, allowlistMode); err != nil {
		return fmt.Errorf("chmod %s: %w", rel, err)
	}
	return nil
}

func (s *session) loadAllowlist() (File, error) {
	return s.parseAt(func() (string, bool, error) {
		text, err := s.repo.ReadFile(s.opts.allowlist)
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		return string(text), err == nil, err
	}, fmt.Errorf("%s does not exist: create it once with --seed", s.opts.allowlist))
}

// parseAt reads an allowlist from where fetch finds it; missing is the error to return when
// there is none.
func (s *session) parseAt(fetch func() (string, bool, error), missing error) (File, error) {
	text, found, err := fetch()
	if err != nil {
		return File{}, fmt.Errorf("read %s: %w", s.opts.allowlist, err)
	}
	if !found {
		return File{}, missing
	}
	list, err := ParseAllowlist(text)
	if err != nil {
		return File{}, fmt.Errorf("%s: %w", s.opts.allowlist, err)
	}
	return list, nil
}

func (s *session) seedList(scan Scan) (int, error) {
	rel := s.opts.allowlist
	_, err := s.repo.Stat(rel)
	if err == nil {
		s.err.printf("✗ bash-limits: refusing to --seed: %s already exists (the ratchet is tightened with --update, never reseeded)\n", rel)
		return ExitFailure, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return 0, fmt.Errorf("stat %s: %w", rel, err)
	}
	list := Seed(scan.Measured)
	if err := s.writeAllowlist(list); err != nil {
		return 0, err
	}
	s.out.printf("BASH-LIMITS-SEEDED %d allowlisted file(s) written to %s\n", len(list), rel)
	return ExitOK, nil
}

// againstBase compares the allowlist with the one committed at the base ref: --base, else the
// merge-base with origin's default branch. A base without an allowlist is an error: it would
// let any entry in.
func (s *session) againstBase(ctx context.Context, root string, list File, scan Scan) ([]Finding, error) {
	repo := gitRepo{s.git, root}
	ref, rel := s.opts.base, filepath.ToSlash(s.opts.allowlist)
	if ref == "" {
		var err error
		if ref, err = repo.mergeBaseWithDefault(ctx); err != nil {
			return nil, fmt.Errorf("no base to compare the allowlist with (pass --base REF): %w", err)
		}
	}
	base, err := s.parseAt(func() (string, bool, error) {
		return repo.fileAtRef(ctx, ref, rel)
	}, fmt.Errorf("%s has no allowlist at %s: the base must be a commit that already has one, or no new entry could be told from an old one", ref, rel))
	if err != nil {
		return nil, err
	}
	return CompareBase(base, list, scan), nil
}

// report prints the findings that stopped a rewrite.
func (s *session) report(findings []Finding) {
	for _, f := range findings {
		s.err.printf("✗ bash-limits: %s: %s\n", f.Path, f.Msg)
	}
}

func (s *session) verdict(findings []Finding, list File, scan Scan) int {
	if len(findings) > 0 {
		for _, f := range findings {
			s.err.printf("✗ bash-limits: %s\n", f.Msg)
		}
		s.err.printf("✗ bash-limits: %d finding(s). Tooling is Go; bash is for very small helpers (src-go/CLAUDE.md).\n", len(findings))
		return ExitFailure
	}
	checked := len(scan.Measured)
	s.out.printf("BASH-LIMITS-OK %d bash files: %d exempt, %d checked (%d allowlisted, %d within limits)\n",
		checked+len(scan.Exempt), len(scan.Exempt), checked, len(list.Records), checked-len(list.Records))
	return ExitOK
}
