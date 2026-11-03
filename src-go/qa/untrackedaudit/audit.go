// Package untrackedaudit is the gate for the defect class "an unrecorded asset" (Plan 00245): work
// someone would want later, kept only where git does not carry it to a remote. It makes two
// checks and fails on either.
//
// Files: every file under untracked/ older than the policy's age floor is judged, except the
// registered stores (data in scripts/untracked-policy.yml). A work-product extension (.md, .patch,
// .diff) is a finding whatever the name; any other file is a finding unless its name is a scratch
// shape (message and body files, captures). The hooks daemon's own stores are counted as warnings.
//
// Unpushed work: a local branch with commits no remote holds, a linked work tree with uncommitted
// changes or (detached) unpushed commits older than the floor, and any stash.
//
// Each finding names the tracked home it belongs in. Ages come from file modification times and
// commit times against an injected clock. Exit 0 clean, 1 findings, 2 the audit could not judge.
// It cannot run in CI: a clean checkout has no untracked/ and no work in progress.
package untrackedaudit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/emit"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootargs"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootpath"
)

// ExitCode is the status qa-untracked-audit exits with.
type ExitCode int

// The statuses callers read.
const (
	ExitOK        ExitCode = 0
	ExitFindings  ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// The kinds of finding, in report order.
const (
	KindAsset           = "untracked-asset"
	KindDecision        = "untracked-decision"
	KindPatch           = "untracked-patch"
	KindUnshaped        = "untracked-unshaped"
	KindBranch          = "unpushed-branch"
	KindDirty           = "dirty-worktree"
	KindWorktreeCommits = "unpushed-worktree"
	KindStash           = "stash"
)

var kindOrder = []string{KindAsset, KindDecision, KindPatch, KindUnshaped, KindBranch, KindDirty, KindWorktreeCommits, KindStash}

// Finding is one thing the audit objects to.
type Finding struct {
	Kind    string
	Subject string
	Age     time.Duration
	Detail  string
	Home    string
}

// Warning is a daemon-owned store holding old files: reported, never a failure.
type Warning struct {
	Store string
	Count int
}

// Env is everything the command touches outside the repository tree.
type Env struct {
	Stdout, Stderr io.Writer
	// Cwd is the default root when --root is not given.
	Cwd string
	// Resolve makes a root free of symlinks.
	Resolve rootpath.Eval
	// Open returns the file system rooted at a directory.
	Open func(root string) fs.FS
	// Git runs git.
	Git proc.Runner
	// Now is the clock.
	Now func() time.Time
	// Mtime is the modification time of an absolute path (fs.ErrNotExist when it is gone).
	Mtime func(path string) (time.Time, error)
}

const usageText = `usage: qa-untracked-audit [--root DIR] [--policy FILE]

Fails on work kept where git does not carry it (Plan 00245): files in untracked/ that are not
scratch and are older than the policy's age floor, local branches with commits no remote holds,
linked work trees with old uncommitted changes, and any stash. The data is
scripts/untracked-policy.yml.

  --root DIR      the repository root (default: the current directory)
  --policy FILE   read the policy from FILE instead of <root>/scripts/untracked-policy.yml
  -h, --help      this text

Exit codes: 0 clean, 1 findings, 2 bad usage or the audit could not judge.
`

// Main runs the command and returns the exit status.
func Main(args []string, env Env) ExitCode {
	out, errOut := emit.New(env.Stdout), emit.New(env.Stderr)
	return emit.Finish(out, errOut, run(args, env, out, errOut), ExitCannotRun)
}

// takePolicyFlag removes --policy FILE from args: the policy to read instead of the root's own
// (a checkout that does not carry the file yet).
func takePolicyFlag(args []string) ([]string, string, error) {
	var rest []string
	policy := ""
	for i := 0; i < len(args); i++ {
		if args[i] != "--policy" {
			rest = append(rest, args[i])
			continue
		}
		if i+1 >= len(args) || args[i+1] == "" || args[i+1][0] == '-' {
			return nil, "", errors.New("--policy needs a file")
		}
		policy = args[i+1]
		i++
	}
	return rest, policy, nil
}

func run(args []string, env Env, out, errOut *emit.Emitter) ExitCode {
	args, policyFile, err := takePolicyFlag(args)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	parsed, err := rootargs.Strict(args)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		if unknown, isUnknown := errors.AsType[*rootargs.UnknownArgError](err); isUnknown && unknown != nil {
			errOut.Block(usageText)
		}
		return ExitCannotRun
	}
	if parsed.Help {
		out.Block(usageText)
		return ExitOK
	}
	root, err := rootpath.RootVia(env.Resolve, env.Cwd, parsed.Root, parsed.Scripts)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	findings, warnings, err := audit(root, policyFile, env)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	return report(findings, warnings, out)
}

// gitTimeout bounds the whole git side of the audit.
const gitTimeout = 5 * time.Minute

func audit(root, policyFile string, env Env) ([]Finding, []Warning, error) {
	fsys := env.Open(root)
	pol, err := loadPolicy(fsys, PolicyPath)
	if policyFile != "" {
		pol, err = loadPolicy(env.Open(filepath.Dir(policyFile)), filepath.Base(policyFile))
	}
	if err != nil {
		return nil, nil, err
	}
	now := env.Now()
	findings, warnings, err := auditFiles(fsys, pol, now)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	git := gitAsker{run: env.Git, root: root}
	branches, err := git.auditBranches(ctx, pol, now)
	if err != nil {
		return nil, nil, err
	}
	trees, err := git.auditWorktrees(ctx, pol, now, env.Mtime)
	if err != nil {
		return nil, nil, err
	}
	stashes, err := git.auditStashes(ctx, now)
	if err != nil {
		return nil, nil, err
	}
	return slices.Concat(findings, branches, trees, stashes), warnings, nil
}

// report prints the findings grouped by kind and returns the exit status.
func report(findings []Finding, warnings []Warning, out *emit.Emitter) ExitCode {
	for _, kind := range kindOrder {
		var group []Finding
		for _, f := range findings {
			if f.Kind == kind {
				group = append(group, f)
			}
		}
		if len(group) == 0 {
			continue
		}
		slices.SortFunc(group, func(a, b Finding) int { return strings.Compare(a.Subject, b.Subject) })
		out.Line(fmt.Sprintf("%s (%d):", kind, len(group)))
		for _, f := range group {
			detail := ""
			if f.Detail != "" {
				detail = "  " + f.Detail
			}
			out.Line(fmt.Sprintf("  %s  (%dh old)%s  -> belongs in: %s", f.Subject, int(f.Age.Hours()), detail, f.Home))
		}
	}
	for _, w := range warnings {
		out.Line(fmt.Sprintf("WARNING daemon-owned store %s: %d file(s) older than the age floor (hooks daemon issue 155 covers these)", w.Store, w.Count))
	}
	if len(findings) == 0 {
		out.Line(fmt.Sprintf("UNTRACKED-AUDIT-OK nothing kept only where git does not carry it (%d warning(s))", len(warnings)))
		return ExitOK
	}
	out.Line(fmt.Sprintf("summary: %d finding(s), %d warning(s)", len(findings), len(warnings)))
	return ExitFindings
}
