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

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// ExitCannotRun is the exit code for a gate that could not reach a verdict (git failed, a
// file could not be read, the allowlist is missing or malformed), as against findings (1).
const ExitCannotRun = 2

// DefaultAllowlist is the allowlist's path, relative to the repository root.
const DefaultAllowlist = "scripts/bash-limits.allowlist"

// allowlistMode is the mode of a written allowlist.
const allowlistMode = 0o600

const usage = `usage: qa-bash-limits [--base REF] [--update | --seed] [--root DIR] [--allowlist PATH]

Checks every bash file git knows against the limits in src-go/CLAUDE.md (60 code lines, 4
functions, no associative arrays, no interactive UI), with the exemptions and the ratchet
allowlist (` + DefaultAllowlist + `).

  --base REF   fail on any allowlist entry added or record raised relative to REF's copy
  --update     lower records to the current measurements and drop entries that are within
               limits or gone; never raises a record or adds an entry
  --seed       write the first allowlist from the current tree; refuses if it exists
  --root DIR   the repository root (default: the git top level of the working directory)
  --allowlist  the allowlist path relative to the root (default ` + DefaultAllowlist + `)
`

// options are the parsed command line.
type options struct {
	base, root, allowlist string
	update, seed          bool
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

// App is the qa-bash-limits command: git through Runner, the verdict on Out, diagnostics on Err.
type App struct {
	Runner execx.Runner
	Out    io.Writer
	Err    io.Writer
}

// session is one run of the command.
type session struct {
	runner execx.Runner
	out    sink
	err    sink
	repo   *os.Root
	opts   options
}

// Run executes the command in cwd and returns its exit code.
func (a *App) Run(ctx context.Context, cwd string, args []string) int {
	s := &session{runner: a.Runner, out: sink{w: a.Out}, err: sink{w: a.Err}}
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
		top, err := gitx.New(s.runner).TopLevel(ctx, cwd)
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
	flags.BoolVar(&o.seed, "seed", false, "")
	err := flags.Parse(args)
	switch {
	case errors.Is(err, flag.ErrHelp):
		s.err.printf("%s", usage)
		return o, term.ExitOK, true
	case err != nil:
		return o, s.usageError(err.Error()), true
	case flags.NArg() > 0:
		return o, s.usageError("unexpected argument " + flags.Arg(0)), true
	case o.update && o.seed:
		return o, s.usageError("--update and --seed are different modes"), true
	case !filepath.IsLocal(o.allowlist):
		return o, s.usageError("--allowlist is a path inside the repository"), true
	}
	return o, term.ExitOK, false
}

func (s *session) usageError(msg string) int {
	s.err.printf("✗ bash-limits: %s\n\n%s", msg, usage)
	return term.ExitUsage
}

// check measures the tree and applies the mode; an error means no verdict could be reached.
func (s *session) check(ctx context.Context, root string) (int, error) {
	files, err := gitx.New(s.runner).ListFiles(ctx, root)
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
	list, err := s.loadAllowlist()
	if err != nil {
		return 0, err
	}
	if s.opts.update {
		if list, err = s.update(list, scan); err != nil {
			return 0, err
		}
	}
	findings := Evaluate(list, scan.Measured)
	if s.opts.base != "" {
		more, err := s.againstBase(ctx, root, list)
		if err != nil {
			return 0, err
		}
		findings = append(findings, more...)
	}
	return s.verdict(findings, list, scan), nil
}

func (s *session) loadAllowlist() (Allowlist, error) {
	rel := s.opts.allowlist
	text, err := s.repo.ReadFile(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%s does not exist: create it once with --seed", rel)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}
	list, err := ParseAllowlist(string(text))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}
	return list, nil
}

func (s *session) seedList(scan Scan) (int, error) {
	rel := s.opts.allowlist
	_, err := s.repo.Stat(rel)
	if err == nil {
		s.err.printf("✗ bash-limits: refusing to --seed: %s already exists (the ratchet is tightened with --update, never reseeded)\n", rel)
		return term.ExitFailure, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return 0, fmt.Errorf("stat %s: %w", rel, err)
	}
	list := Seed(scan.Measured)
	if err := s.repo.WriteFile(rel, []byte(list.Format()), allowlistMode); err != nil {
		return 0, fmt.Errorf("write %s: %w", rel, err)
	}
	s.out.printf("BASH-LIMITS-SEEDED %d allowlisted file(s) written to %s\n", len(list), rel)
	return term.ExitOK, nil
}

func (s *session) update(list Allowlist, scan Scan) (Allowlist, error) {
	tightened := Tighten(list, scan.Measured)
	if tightened.Format() == list.Format() {
		return list, nil
	}
	if err := s.repo.WriteFile(s.opts.allowlist, []byte(tightened.Format()), allowlistMode); err != nil {
		return nil, fmt.Errorf("write %s: %w", s.opts.allowlist, err)
	}
	s.err.printf("bash-limits: --update rewrote the allowlist: %d -> %d entries\n", len(list), len(tightened))
	return tightened, nil
}

// againstBase compares the allowlist with the one committed at the base ref.
func (s *session) againstBase(ctx context.Context, root string, list Allowlist) ([]Finding, error) {
	ref, rel := s.opts.base, filepath.ToSlash(s.opts.allowlist)
	text, found, err := gitx.New(s.runner).FileAtRef(ctx, root, ref, rel)
	if err != nil {
		return nil, err
	}
	if !found {
		s.err.printf("bash-limits: %s has no allowlist (it is being introduced): nothing to compare\n", ref)
		return nil, nil
	}
	base, err := ParseAllowlist(text)
	if err != nil {
		return nil, fmt.Errorf("%s at %s: %w", rel, ref, err)
	}
	return CompareBase(base, list), nil
}

func (s *session) verdict(findings []Finding, list Allowlist, scan Scan) int {
	if len(findings) > 0 {
		for _, f := range findings {
			s.err.printf("✗ bash-limits: %s\n", f.Msg)
		}
		s.err.printf("✗ bash-limits: %d finding(s). Tooling is Go; bash is for very small helpers (src-go/CLAUDE.md).\n", len(findings))
		return term.ExitFailure
	}
	s.out.printf("BASH-LIMITS-OK %d bash file(s) checked: %d exempt, %d allowlisted, %d within limits\n",
		len(scan.Measured), scan.Exempt, len(list), len(scan.Measured)-len(list))
	return term.ExitOK
}
