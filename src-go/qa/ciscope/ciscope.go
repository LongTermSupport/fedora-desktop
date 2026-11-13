// Package ciscope decides how much QA a CI run of .github/workflows/qa.yml does (issue #245,
// "diff gate, full after"). A protected branch moves only to a commit qa.yml passed on a push
// (rule push-without-qa-verdict, src-go/qa/pushgate), so the run on a work branch is the gate,
// and the full suite runs again on the protected branch after the move.
//
// The answer is three lines on stdout, in the form a workflow appends to $GITHUB_OUTPUT:
// mode=full|diff, line=<the protected line judged against, or empty> and base=<that line's tip
// on origin, the diff base, or empty>. The reason is one line on stderr.
//
//   - C1 a tag push is mode full; git is not asked.
//   - C2 a push to a protected branch (exempt in scripts/branch-policy.toml) is mode full, line
//     that branch: qa-all runs after the move, and a red run is fixed forward.
//   - C3 a push to a release-<version> branch, which the release workflow cuts and whose run its
//     tag job reads, is mode full.
//   - C4 a push to any other branch is mode diff against the tip of the protected line it is
//     built on: the default branch when the branch contains its tip on origin,
//   - C5 else the one other protected line whose tip it contains (F43, F44-arm; a line absent
//     from origin is no candidate).
//   - C6 a branch that contains no line's tip is refused (exit 1): its base is stale, and a diff
//     against it would judge changes that line already has. The refusal names the lines.
//   - C7 a branch that contains two or more lines' tips, none the default, is refused: no one
//     line is the base.
//   - C8 a pull request is mode diff against its base branch's tip on origin;
//   - C9 one whose base is not on origin cannot be judged (exit 2).
//   - C10 git failing, and C11 an unreadable policy, cannot be judged (exit 2); nothing is
//     printed on stdout, so no scope reaches the workflow.
//   - C12 bad usage is exit 2 with the usage on stderr; --help prints it on stdout, exit 0.
package ciscope

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/branchhygiene"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/emit"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// ExitCode is the command's status.
type ExitCode int

// 0 a scope was decided, 1 the branch is refused, 2 the command could not judge.
const (
	ExitOK        ExitCode = 0
	ExitRefused   ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// gitTimeout bounds one git question.
const gitTimeout = time.Minute

// ReleasePrefix starts the branch the release workflow pushes for a release
// (.github/workflows/release.yml); the toolchain tests hold the two equal.
const ReleasePrefix = "release-"

const (
	eventPush        = "push"
	eventPullRequest = "pull_request"
	modeFull         = "full"
	modeDiff         = "diff"
)

const usageText = `usage: qa-ci-scope --event push|pull_request --ref REF --default BRANCH [--pr-base BRANCH]

How much QA a CI run of .github/workflows/qa.yml does (CLAUDE/QA.md#push-without-qa-verdict).
Prints mode=full|diff, line=<protected line> and base=<its tip on origin> for $GITHUB_OUTPUT.

  a tag, a protected branch (exempt in scripts/branch-policy.toml) or a release-<version> branch:
      mode=full (qa-all.bash)
  any other branch: mode=diff (qa-changed.bash --base <base>) against the tip of the protected
      line it contains: the default branch, else the one other line; none or several is refused
  a pull request (--pr-base its base branch): mode=diff against that branch's tip

Exit codes: 0 decided, 1 the branch contains no single protected line's tip, 2 could not judge.
`

// Env is what the command touches: the streams, git, the checkout and its policy file.
type Env struct {
	Stdout, Stderr io.Writer
	Runner         proc.Runner
	Cwd            string
	ReadFile       func(path string) ([]byte, error)
}

type options struct {
	event, ref, defaultBranch, prBase string
	help                              bool
}

// scope is the decision.
type scope struct {
	mode, line, base, reason string
}

// refusal is a branch refused; any other error is a run that cannot judge.
type refusal struct{ msg string }

func (r *refusal) Error() string { return r.msg }

// Main decides the scope of one CI run.
func Main(ctx context.Context, args []string, env Env) ExitCode {
	out, errOut := emit.New(env.Stdout), emit.New(env.Stderr)
	code := decideAndPrint(ctx, args, env, out, errOut)
	return emit.Finish(out, errOut, code, ExitCannotRun)
}

func decideAndPrint(ctx context.Context, args []string, env Env, out, errOut *emit.Emitter) ExitCode {
	opts, err := parseArgs(args)
	if err != nil {
		errOut.Line("ERROR: qa-ci-scope: " + err.Error())
		errOut.Block(usageText)
		return ExitCannotRun
	}
	if opts.help {
		out.Block(usageText)
		return ExitOK
	}
	s, err := decide(ctx, opts, env)
	if refused, ok := errors.AsType[*refusal](err); ok {
		errOut.Line("✗ qa-ci-scope: " + refused.msg)
		return ExitRefused
	}
	if err != nil {
		errOut.Line("ERROR: qa-ci-scope: " + err.Error())
		return ExitCannotRun
	}
	errOut.Line("qa-ci-scope: " + s.reason)
	out.Line("mode=" + s.mode)
	out.Line("line=" + s.line)
	out.Line("base=" + s.base)
	return ExitOK
}

func parseArgs(args []string) (options, error) {
	var o options
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		return options{help: true}, nil
	}
	values := map[string]*string{"--event": &o.event, "--ref": &o.ref, "--default": &o.defaultBranch, "--pr-base": &o.prBase}
	seen := map[string]bool{}
	for i := 0; i < len(args); i += 2 {
		target, ok := values[args[i]]
		switch {
		case !ok:
			return o, fmt.Errorf("unknown argument %q", args[i])
		case seen[args[i]]:
			return o, fmt.Errorf("%s given twice", args[i])
		case i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(args[i+1], "-"):
			return o, fmt.Errorf("%s needs a value", args[i])
		}
		seen[args[i]] = true
		*target = args[i+1]
	}
	return o, o.validate()
}

func (o options) validate() error {
	switch {
	case o.event == "" || o.ref == "" || o.defaultBranch == "":
		return errors.New("--event, --ref and --default are required")
	case o.event == eventPullRequest && o.prBase == "":
		return errors.New("a pull_request needs --pr-base")
	case o.event == eventPush && o.prBase != "":
		return errors.New("--pr-base is for a pull_request only")
	case o.event != eventPush && o.event != eventPullRequest:
		return fmt.Errorf("--event %q is neither push nor pull_request", o.event)
	case !strings.HasPrefix(o.ref, "refs/"):
		return fmt.Errorf("--ref %q is not a full ref (refs/...)", o.ref)
	}
	return nil
}

func decide(ctx context.Context, o options, env Env) (scope, error) {
	if o.event == eventPullRequest {
		tip, found, err := lineTip(ctx, env, o.prBase)
		switch {
		case err != nil:
			return scope{}, err
		case !found:
			return scope{}, fmt.Errorf("the pull request's base %s is not on origin", o.prBase)
		}
		return scope{mode: modeDiff, line: o.prBase, base: tip, reason: "a pull request into " + o.prBase + ": diff mode against its tip " + tip}, nil
	}
	if strings.HasPrefix(o.ref, "refs/tags/") {
		return scope{mode: modeFull, reason: "a tag: the full suite"}, nil
	}
	branch, ok := strings.CutPrefix(o.ref, "refs/heads/")
	if !ok {
		return scope{}, fmt.Errorf("--ref %q is neither a branch nor a tag", o.ref)
	}
	pol, err := branchhygiene.ReadPolicy(env.ReadFile, env.Cwd)
	if err != nil {
		return scope{}, err
	}
	switch {
	case slices.Contains(pol.Exempt, branch):
		return scope{mode: modeFull, line: branch, reason: branch + " is protected: the full suite, after the move"}, nil
	case strings.HasPrefix(branch, ReleasePrefix):
		return scope{mode: modeFull, reason: branch + " is a release branch: the full suite"}, nil
	}
	return workBranch(ctx, env, branch, o.defaultBranch, pol.Exempt)
}

// workBranch is the diff scope of a work branch: the protected line whose tip it contains.
func workBranch(ctx context.Context, env Env, branch, defaultBranch string, lines []string) (scope, error) {
	ordered := append([]string{defaultBranch}, slices.DeleteFunc(slices.Clone(lines), func(l string) bool { return l == defaultBranch })...)
	var contained []string
	tips := map[string]string{}
	for _, line := range ordered {
		tip, found, err := lineTip(ctx, env, line)
		if err != nil {
			return scope{}, err
		}
		if !found {
			continue
		}
		in, err := containsCommit(ctx, env, tip)
		if err != nil {
			return scope{}, err
		}
		if !in {
			continue
		}
		if line == defaultBranch {
			return diffScope(branch, line, tip), nil
		}
		contained = append(contained, line)
		tips[line] = tip
	}
	switch len(contained) {
	case 1:
		return diffScope(branch, contained[0], tips[contained[0]]), nil
	case 0:
		return scope{}, &refusal{msg: branch + " contains the tip of no protected line on origin (" + strings.Join(ordered, " ") +
			"): merge " + defaultBranch + " (or the line it targets) into it and push again; a diff against a stale base judges changes the line already has"}
	}
	return scope{}, &refusal{msg: branch + " contains the tips of " + strings.Join(contained, " ") +
		", none of them " + defaultBranch + ", so no one line is its base: build it on one line"}
}

func diffScope(branch, line, tip string) scope {
	return scope{mode: modeDiff, line: line, base: tip, reason: branch + " is built on " + line + ": diff mode against its tip " + tip}
}

// lineTip is the commit origin/<line> names, and whether it exists.
func lineTip(ctx context.Context, env Env, line string) (string, bool, error) {
	res, err := git(ctx, env, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+line+"^{commit}")
	switch {
	case err != nil:
		return "", false, err
	case res.Code == 1 && strings.TrimSpace(res.Stdout) == "":
		return "", false, nil
	case res.Code != 0:
		return "", false, fmt.Errorf("git rev-parse origin/%s: exit %d: %s", line, res.Code, strings.TrimSpace(res.Stderr))
	}
	tip := strings.TrimSpace(res.Stdout)
	if tip == "" {
		return "", false, fmt.Errorf("git rev-parse origin/%s printed no commit", line)
	}
	return tip, true, nil
}

// containsCommit is whether HEAD contains sha.
func containsCommit(ctx context.Context, env Env, sha string) (bool, error) {
	res, err := git(ctx, env, "merge-base", "--is-ancestor", sha, "HEAD")
	switch {
	case err != nil:
		return false, err
	case res.Code == 0:
		return true, nil
	case res.Code == 1:
		return false, nil
	}
	return false, fmt.Errorf("git merge-base --is-ancestor %s HEAD: exit %d: %s", sha, res.Code, strings.TrimSpace(res.Stderr))
}

func git(ctx context.Context, env Env, args ...string) (proc.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	res, err := env.Runner.Run(ctx, proc.Cmd{Argv: append([]string{"git"}, args...), Dir: env.Cwd})
	switch {
	case err != nil:
		return res, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	case res.TimedOut:
		return res, fmt.Errorf("git %s: no answer within %s", strings.Join(args, " "), gitTimeout)
	case res.Leftover:
		return res, fmt.Errorf("git %s: left a child running", strings.Join(args, " "))
	}
	return res, nil
}
