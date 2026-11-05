package branchhygiene

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/emit"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootargs"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootpath"
)

// ExitCode is the status qa-branch-hygiene exits with.
type ExitCode int

// The statuses: 0 clean (warnings allowed), 1 findings, 2 the repository could not be judged.
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// PolicyFile is the policy's path under the repository root.
const PolicyFile = "scripts/branch-policy.toml"

const (
	defaultBranch = "F44"
	gitTimeout    = time.Minute
)

// Env is everything the command touches outside the arguments.
type Env struct {
	Stdout, Stderr io.Writer
	// Cwd is the default root when neither --root nor --scripts is given.
	Cwd string
	// Eval makes a root path free of symlinks (filepath.EvalSymlinks).
	Eval rootpath.Eval
	Now  func() time.Time
	// Git runs git in the repository root.
	Git      proc.Runner
	ReadFile func(path string) ([]byte, error)
	Getenv   func(key string) string
}

const usageText = `usage: qa-branch-hygiene [--root DIR] [--scripts DIR]

Judges the remote branches of origin against scripts/branch-policy.toml: a cap on open work
branches, no branch merged into the default branch left undeleted, no work branch dormant past
the limits unless parked with a reason and a review date. Run after "git fetch --prune origin".

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

Exit codes: 0 clean (warnings allowed), 1 findings, 2 bad usage or the repository could not be judged.
`

// Main runs the command and returns the exit status.
func Main(args []string, env Env) ExitCode {
	out, errOut := emit.New(env.Stdout), emit.New(env.Stderr)
	code := run(args, env, out, errOut)
	return emit.Finish(out, errOut, code, ExitCannotRun)
}

func run(args []string, env Env, out, errOut *emit.Emitter) ExitCode {
	opts, err := rootargs.Strict(args)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		errOut.Block(usageText)
		return ExitCannotRun
	}
	if opts.Help {
		out.Block(usageText)
		return ExitOK
	}
	root, err := rootpath.RootVia(env.Eval, env.Cwd, opts.Root, opts.Scripts)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	pol, err := ReadPolicy(env.ReadFile, root)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	branches, err := readBranches(env, root)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	now := env.Now()
	findings := Evaluate(pol, branches, now)
	report(out, pol, branches, findings, env.Getenv("GITHUB_ACTIONS") != "")
	if slices.ContainsFunc(findings, func(f Finding) bool { return f.Fail }) {
		return ExitFailed
	}
	return ExitOK
}

// ReadPolicy reads and validates PolicyFile under root; branch-delete-list reads it here too, so
// the two commands cannot disagree on what is exempt or parked.
func ReadPolicy(readFile func(path string) ([]byte, error), root string) (Policy, error) {
	data, err := readFile(filepath.Join(root, PolicyFile))
	if err != nil {
		return Policy{}, fmt.Errorf("policy: %w", err)
	}
	var pol Policy
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&pol); err != nil {
		return Policy{}, fmt.Errorf("policy %s: %w", PolicyFile, err)
	}
	if err := pol.Validate(); err != nil {
		return Policy{}, fmt.Errorf("policy %s: %w", PolicyFile, err)
	}
	return pol, nil
}

// git runs one git command; a start failure is an error, the exit code is the caller's to read.
func git(env Env, root string, args ...string) (proc.Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	res, err := env.Git.Run(ctx, proc.Cmd{Argv: append([]string{"git"}, args...), Dir: root})
	if err != nil {
		return res, fmt.Errorf("git %s: %w", args[0], err)
	}
	if res.TimedOut {
		return res, fmt.Errorf("git %s timed out", args[0])
	}
	return res, nil
}

// readBranches is every remote branch of origin with its last commit time and whether it is
// merged into origin/<default>.
func readBranches(env Env, root string) ([]Branch, error) {
	base := "refs/remotes/origin/" + defaultBranch
	res, err := git(env, root, "rev-parse", "--verify", "--quiet", base+"^{commit}")
	if err != nil {
		return nil, err
	}
	if res.Code != 0 {
		return nil, fmt.Errorf("origin/%s does not exist here; fetch with full history first", defaultBranch)
	}
	res, err = git(env, root, "for-each-ref", "--format=%(refname:lstrip=3)%09%(committerdate:unix)", "refs/remotes/origin")
	if err != nil {
		return nil, err
	}
	if res.Code != 0 {
		return nil, fmt.Errorf("git for-each-ref exited %d: %s", res.Code, strings.TrimSpace(res.Stderr))
	}
	var branches []Branch
	for line := range strings.SplitSeq(strings.TrimSpace(res.Stdout), "\n") {
		if line == "" {
			continue
		}
		b, err := parseRef(line)
		if err != nil {
			return nil, err
		}
		if b.Name == "HEAD" {
			continue
		}
		if b.Merged, err = isMerged(env, root, b.Name, base); err != nil {
			return nil, err
		}
		branches = append(branches, b)
	}
	return branches, nil
}

func parseRef(line string) (Branch, error) {
	name, stamp, ok := strings.Cut(line, "\t")
	if !ok || name == "" {
		return Branch{}, fmt.Errorf("unreadable ref line %q", line)
	}
	secs, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil {
		return Branch{}, fmt.Errorf("ref %s: commit time %q is not a unix time", name, stamp)
	}
	return Branch{Name: name, Commit: time.Unix(secs, 0).UTC()}, nil
}

// isMerged is whether the branch tip is an ancestor of base: git exits 0 for yes, 1 for no.
func isMerged(env Env, root, name, base string) (bool, error) {
	res, err := git(env, root, "merge-base", "--is-ancestor", "refs/remotes/origin/"+name, base)
	if err != nil {
		return false, err
	}
	switch res.Code {
	case 0:
		return true, nil
	case 1:
		return false, nil
	}
	return false, errors.New("git merge-base for " + name + " exited " + strconv.Itoa(res.Code) + ": " + strings.TrimSpace(res.Stderr))
}

// report prints the summary and each group, then the GitHub annotations when asked.
func report(out *emit.Emitter, pol Policy, branches []Branch, findings []Finding, annotate bool) {
	open := 0
	for _, b := range branches {
		if b.Name != "" && !slices.Contains(pol.Exempt, b.Name) && !b.Merged {
			open++
		}
	}
	out.Line(fmt.Sprintf("branch hygiene: %d open work branches, cap %d (%d remote branches)", open, pol.Cap, len(branches)))
	for _, g := range Groups {
		first := true
		for _, f := range findings {
			if f.Group != g {
				continue
			}
			if first {
				out.Line("== " + string(g) + " ==")
				first = false
			}
			out.Line(f.Text)
		}
	}
	if !annotate {
		return
	}
	for _, f := range findings {
		switch {
		case f.Fail && f.Branch == "":
			out.Line("::error::" + string(f.Group) + ": " + f.Text)
		case f.Fail:
			out.Line("::error title=" + string(f.Group) + "::" + strings.TrimSpace(f.Text))
		case f.Group == DormantWarn:
			out.Line("::warning title=" + string(f.Group) + "::" + strings.TrimSpace(f.Text))
		}
	}
}
