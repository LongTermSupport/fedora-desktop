// Package ccyversionbase names the commit the pre-commit hook judges a ccy version bump against
// (issue #298). A ccy version names a release on a protected line, not a commit, so a merge is
// judged against its parent on that line, never against whichever parent happens to be HEAD.
//
// The command prints one line on stdout:
//
//	B1. No merge in progress: HEAD (the rule every other commit has always had).
//	B2. A merge with one parent on a protected line (a commit an origin/<line> tip of
//	    scripts/branch-policy.toml's exempt lines contains): that parent's commit, on either side.
//	B3. Both parents on a protected line, one holding the other: the later.
//	B4. No parent on a protected line (two work branches): an empty line. Nothing is owed: the
//	    bump is judged when the work is merged with the line it lands on.
//	B5. Both parents on protected lines, neither holding the other: refused (exit 2), never a guess.
//	B6. A git that fails, or a policy that cannot be read: exit 2 with nothing on stdout.
//	B7. A merge whose staged CCY_VERSION or container LABEL is below a parent's: refused (exit 1),
//	    every merge, a merge of two work branches included. With the hook's own check that the
//	    version differs from the base's when ccy files differ from it, the base's is exceeded.
//
//	B8. --protected-tip SHA: the line's tip is SHA, not origin's tips of the policy's lines (CI
//	    judging a merge commit already on F44 against the tip before the push, issue #311).
//
// A side without the file, or without the version line, sets no floor; a version that is not
// dotted numbers cannot be ordered (exit 2). The decision is issue #298's.
//
// Exit codes: 0 a base was named (or none is owed), 1 refused, 2 cannot judge, 64 usage.
package ccyversionbase

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/branchhygiene"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/emit"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// ExitCode is the command's status.
type ExitCode int

// The statuses the hook reads.
const (
	ExitOK        ExitCode = 0
	ExitRefused   ExitCode = 1
	ExitCannotRun ExitCode = 2
	ExitUsage     ExitCode = 64
)

// gitTimeout bounds each git question.
const gitTimeout = time.Minute

const usageText = `usage: qa-ccy-version-base [--protected-tip SHA]

The commit a ccy version bump is judged against: HEAD outside a merge; in a merge, the parent on a
protected line (scripts/branch-policy.toml's exempt lines, as origin has them), the later of two;
an empty line when no parent is on one. A merge whose staged CCY_VERSION or container LABEL is
below a parent's is refused. --protected-tip SHA judges "on a protected line" as "in SHA" instead.
Exit codes: 0 named, 1 refused, 2 cannot judge, 64 usage.
`

// Env is what the command touches: the streams, git, the checkout and its policy file.
type Env struct {
	Stdout, Stderr io.Writer
	Runner         proc.Runner
	Cwd            string
	ReadFile       func(path string) ([]byte, error)
}

// Main names the base and returns the exit status.
func Main(ctx context.Context, args []string, env Env) ExitCode {
	out, errOut := emit.New(env.Stdout), emit.New(env.Stderr)
	return emit.Finish(out, errOut, run(ctx, args, env, out, errOut), ExitCannotRun)
}

func run(ctx context.Context, args []string, env Env, out, errOut *emit.Emitter) ExitCode {
	tip := ""
	switch {
	case len(args) == 1 && (args[0] == "-h" || args[0] == "--help"):
		out.Block(usageText)
		return ExitOK
	case len(args) == 2 && args[0] == "--protected-tip" && args[1] != "":
		tip = args[1]
	case len(args) != 0:
		errOut.Block(usageText)
		return ExitUsage
	}
	base, parents, err := Base(ctx, env, tip)
	var behind []string
	if err == nil {
		behind, err = Behind(ctx, env, parents)
	}
	if err != nil {
		errOut.Line("ERROR: qa-ccy-version-base: " + err.Error())
		return ExitCannotRun
	}
	if len(behind) > 0 {
		errOut.Line("COMMIT REJECTED: a merge's version is never below a parent's (issue #298); raise it in the merge:")
		for _, b := range behind {
			errOut.Line("  " + b)
		}
		return ExitRefused
	}
	out.Line(base)
	return ExitOK
}

// Base is the commit to judge a bump against, "HEAD", or "" when none is owed, and a merge's
// two parents (none outside a merge). A non-empty tip is the protected line's tip (B8).
func Base(ctx context.Context, env Env, tip string) (string, []string, error) {
	incoming, merging, err := revParse(ctx, env, "--verify", "--quiet", "MERGE_HEAD^{commit}")
	if err != nil || !merging {
		return "HEAD", nil, err
	}
	head, found, err := revParse(ctx, env, "--verify", "HEAD^{commit}")
	switch {
	case err != nil:
		return "", nil, err
	case !found:
		return "", nil, errors.New("a merge in progress has no HEAD commit")
	}
	tips, err := lineTips(ctx, env, tip)
	if err != nil {
		return "", nil, err
	}
	base, err := protectedParent(ctx, env, tips, head, incoming)
	return base, []string{head, incoming}, err
}

// lineTips are the protected lines' tips: the given one, or origin's of the policy's lines.
func lineTips(ctx context.Context, env Env, given string) ([]string, error) {
	if given != "" {
		sha, found, err := revParse(ctx, env, "--verify", "--quiet", given+"^{commit}")
		if err == nil && !found {
			err = fmt.Errorf("--protected-tip %s names no commit", given)
		}
		return []string{sha}, err
	}
	pol, err := branchhygiene.ReadPolicy(env.ReadFile, env.Cwd)
	if err != nil {
		return nil, err
	}
	var tips []string
	for _, line := range pol.Exempt {
		tip, exists, err := revParse(ctx, env, "--verify", "--quiet", "refs/remotes/origin/"+line+"^{commit}")
		if err != nil {
			return nil, err
		}
		if exists {
			tips = append(tips, tip)
		}
	}
	return tips, nil
}

// protectedParent is the parent some tip contains, the later of two, or "" for neither.
func protectedParent(ctx context.Context, env Env, tips []string, head, incoming string) (string, error) {
	headOn, incomingOn := false, false
	for _, tip := range tips {
		var err error
		if headOn, err = orContains(ctx, env, headOn, head, tip); err != nil {
			return "", err
		}
		if incomingOn, err = orContains(ctx, env, incomingOn, incoming, tip); err != nil {
			return "", err
		}
	}
	switch {
	case headOn && incomingOn:
		return later(ctx, env, head, incoming)
	case headOn:
		return head, nil
	case incomingOn:
		return incoming, nil
	}
	return "", nil
}

// orContains is on, or whether tip contains commit.
func orContains(ctx context.Context, env Env, on bool, commit, tip string) (bool, error) {
	if on {
		return true, nil
	}
	return isAncestor(ctx, env, commit, tip)
}

// later is whichever of a and b holds the other.
func later(ctx context.Context, env Env, a, b string) (string, error) {
	if in, err := isAncestor(ctx, env, a, b); err != nil || in {
		return b, err
	}
	if in, err := isAncestor(ctx, env, b, a); err != nil || in {
		return a, err
	}
	return "", fmt.Errorf("both parents are on protected lines and neither holds the other (%s, %s): no one release to judge the bump against", a, b)
}

// revParse is git rev-parse's one commit, and whether it named one (exit 1 with --quiet: none).
func revParse(ctx context.Context, env Env, args ...string) (string, bool, error) {
	res, err := git(ctx, env, append([]string{"rev-parse"}, args...)...)
	switch {
	case err != nil:
		return "", false, err
	case res.Code == 1 && strings.TrimSpace(res.Stdout) == "":
		return "", false, nil
	case res.Code != 0:
		return "", false, fmt.Errorf("git rev-parse %s: exit %d: %s", strings.Join(args, " "), res.Code, strings.TrimSpace(res.Stderr))
	}
	sha := strings.TrimSpace(res.Stdout)
	if sha == "" {
		return "", false, fmt.Errorf("git rev-parse %s printed no commit", strings.Join(args, " "))
	}
	return sha, true, nil
}

// isAncestor is whether descendant contains commit.
func isAncestor(ctx context.Context, env Env, commit, descendant string) (bool, error) {
	res, err := git(ctx, env, "merge-base", "--is-ancestor", commit, descendant)
	switch {
	case err != nil:
		return false, err
	case res.Code == 0:
		return true, nil
	case res.Code == 1:
		return false, nil
	}
	return false, fmt.Errorf("git merge-base --is-ancestor %s %s: exit %d: %s", commit, descendant, res.Code, strings.TrimSpace(res.Stderr))
}

func git(ctx context.Context, env Env, args ...string) (proc.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	res, err := env.Runner.Run(ctx, proc.Cmd{Argv: append([]string{"git"}, args...), Dir: env.Cwd})
	if err != nil {
		return proc.Result{}, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return res, nil
}
