// Package checkoutgate is the checkout-gate command (Plan 00265 Task 4.1, register R78, the
// owner's Q18): run.bash runs it before `--changed` and `--rerun` run plays, and refuses the
// run when the checkout holds work no commit does, or is not level with its upstream after a
// fetch, because a play deployed from such a checkout deploys code that is not on the remote
// (the remote is this repository's backup). `--force` runs anyway. It is a gate, so it
// refuses and never prompts. run.bash is on the bash-limits ratchet, so the judgement lives
// here and run.bash only builds and runs this command.
//
// Expectations, each pinned by a test:
//
//   - G1 a clean checkout on a branch level with its upstream after a fetch passes: exit 0,
//     one line on stderr naming the upstream.
//   - G2 uncommitted work is refused: tracked changes, staged or not, and untracked files no
//     ignore rule covers. Untracked files count because a play can read a new file (a
//     template directory, a fileglob) and would deploy what no commit holds; .gitignore is
//     the line between work and scratch. The refusal counts the paths and names the first
//     ten. Fix: commit them, or stash them.
//   - G3 every cause found is reported in the one refusal, the tree's first: the tree needs no
//     network.
//   - G4 a detached HEAD (a RUN_BASH_GIT_REF pin) has no upstream, so only the tree is checked
//     and nothing is fetched; the pass names the commit.
//   - G5 a branch with no upstream is refused, as nothing shows its commits are on a remote;
//     nothing is fetched. Fix: git push -u origin <branch>.
//   - G6 the comparison follows a `git fetch` that never asks on the terminal. A fetch that
//     fails (offline, no key) is a refusal carrying git's words, never a pass, and nothing is
//     compared against tracking refs it did not refresh. `--force` is the way past it, as an
//     offline machine must still be able to run a play; it then says the comparison was not
//     made.
//   - G7 behind its upstream: refused with the count. Fix: git pull --ff-only.
//   - G8 ahead of it (commits no remote holds): refused with the count. Fix: git push.
//   - G9 diverged: refused with both counts. Fix: git pull --rebase, then git push.
//   - G10 an upstream that is set but cannot be read after the fetch (its branch deleted on
//     the remote) is refused with git's words. Fix: push the branch again with -u.
//   - G11 a refusal is exit 1; its first line names the run.bash option refused, each cause is
//     a line with its fix under it, and the last line is the same command with --force.
//   - G12 `--force` reports each cause it overrode on stderr and exits 0.
//   - G13 a git that cannot answer (status, the branch, its config, the counts) is a fault:
//     exit 1 with git's words, --force or not, as the gate cannot say what it would override.
//   - G14 the command line is `[--force] <checkout> <run.bash option>`, the checkout an
//     absolute path and the option one starting with "--"; anything else is exit 64 and runs
//     no git.
//   - G15 the gate has no payload, so it is given no stdout: every line is a diagnostic, on
//     stderr, and a line that cannot be written is exit 1.
package checkoutgate

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const usage = "usage: checkout-gate [--force] <checkout> <run.bash option>"

// namedPaths is how many uncommitted paths a refusal names before it counts the rest.
const namedPaths = 10

// shortSHA is the length a commit is named by.
const shortSHA = 12

// App is one run of the gate over a checkout.
type App struct {
	Git    *gitx.Git
	Stderr io.Writer
}

// cause is one reason the checkout is refused, with the command that removes it.
type cause struct {
	what string
	fix  string
}

// Run judges the checkout the arguments name and returns the exit code.
func (a App) Run(ctx context.Context, args []string) term.ExitCode {
	force, dir, option, ok := parse(args)
	if !ok {
		return a.say(term.ExitUsage, usage)
	}
	causes, pass, err := a.judge(ctx, dir)
	if err != nil {
		return a.say(term.ExitFailure, "✗ checkout-gate: cannot judge the checkout at "+dir+": "+err.Error())
	}
	if len(causes) == 0 {
		return a.say(term.ExitOK, "✓ "+pass)
	}
	if force {
		lines := []string{"⚠ --force: running anyway, although:"}
		for _, c := range causes {
			lines = append(lines, "  • "+c.what)
		}
		return a.say(term.ExitOK, lines...)
	}
	lines := []string{"✗ run.bash " + option + " refused: the checkout at " + dir + " is not what the remote holds."}
	for _, c := range causes {
		lines = append(lines, "  • "+c.what, "    fix: "+c.fix)
	}
	lines = append(lines, "  To run anyway: ./run.bash "+option+" --force")
	return a.say(term.ExitFailure, lines...)
}

func parse(args []string) (force bool, dir, option string, ok bool) {
	if len(args) > 0 && args[0] == "--force" {
		force = true
		args = args[1:]
	}
	const want = 2
	if len(args) != want || !filepath.IsAbs(args[0]) || !strings.HasPrefix(args[1], "--") {
		return false, "", "", false
	}
	return force, args[0], args[1], true
}

// judge is every cause found in the checkout at dir, or the line a pass prints.
func (a App) judge(ctx context.Context, dir string) ([]cause, string, error) {
	paths, err := a.Git.ChangedPaths(ctx, dir)
	if err != nil {
		return nil, "", err
	}
	var causes []cause
	if len(paths) > 0 {
		causes = append(causes, dirty(paths))
	}
	branch, onBranch, err := a.Git.Branch(ctx, dir)
	if err != nil {
		return nil, "", err
	}
	if !onBranch {
		head, err := a.Git.Head(ctx, dir)
		if err != nil {
			return nil, "", err
		}
		return causes, "Checkout clean (detached at " + head[:shortSHA] + ", so there is no upstream to compare with)", nil
	}
	has, err := a.Git.HasUpstream(ctx, dir, branch)
	if err != nil {
		return nil, "", err
	}
	if !has {
		return append(causes, cause{
			what: branch + " has no upstream, so nothing shows its commits are on a remote",
			fix:  "git push -u origin " + branch,
		}), "", nil
	}
	upstream, level, err := a.compare(ctx, dir, branch)
	if err != nil {
		return nil, "", err
	}
	return append(causes, level...), "Checkout clean and level with " + upstream + " (fetched just now)", nil
}

// compare fetches and is how far branch is from its upstream, as causes.
func (a App) compare(ctx context.Context, dir, branch string) (string, []cause, error) {
	if err := a.Git.FetchUpstream(ctx, dir); err != nil {
		return "", []cause{fetchFailed(branch, err)}, nil
	}
	upstream, err := a.Git.Upstream(ctx, dir)
	if err != nil {
		return "", []cause{upstreamGone(branch, err)}, nil
	}
	ahead, behind, err := a.Git.AheadBehind(ctx, dir)
	if err != nil {
		return "", nil, err
	}
	switch {
	case ahead > 0 && behind > 0:
		return upstream, []cause{{
			what: fmt.Sprintf("%s has diverged from %s: %d commit(s) of its own, %d it lacks", branch, upstream, ahead, behind),
			fix:  "git pull --rebase, then git push",
		}}, nil
	case behind > 0:
		return upstream, []cause{{
			what: fmt.Sprintf("%s is %d commit(s) behind %s, so a play would deploy older code than the remote holds", branch, behind, upstream),
			fix:  "git pull --ff-only",
		}}, nil
	case ahead > 0:
		return upstream, []cause{{
			what: fmt.Sprintf("%s is %d commit(s) ahead of %s: commits no remote holds", branch, ahead, upstream),
			fix:  "git push",
		}}, nil
	}
	return upstream, nil, nil
}

// fetchFailed is G6's cause: the tracking refs are as old as the last fetch that worked.
func fetchFailed(branch string, err error) cause {
	return cause{
		what: "the fetch failed, so whether " + branch + " is behind or ahead of its upstream cannot be told (" + err.Error() + ")",
		fix:  "reach the remote (the network, the ssh key in the agent), then run it again",
	}
}

// upstreamGone is G10's cause: the branch tracks one git cannot read.
func upstreamGone(branch string, err error) cause {
	return cause{
		what: branch + "'s upstream cannot be read after the fetch (" + err.Error() + ")",
		fix:  "git push -u origin " + branch + ", or git branch --set-upstream-to=<remote>/<branch>",
	}
}

func dirty(paths []string) cause {
	named := paths
	more := ""
	if len(paths) > namedPaths {
		named = paths[:namedPaths]
		more = fmt.Sprintf(", and %d more", len(paths)-namedPaths)
	}
	return cause{
		what: fmt.Sprintf("%d uncommitted path(s): %s%s", len(paths), strings.Join(named, ", "), more),
		fix:  "commit them (git add -A && git commit), or stash them (git stash push -u)",
	}
}

// say writes lines to stderr and returns code, or a failure when they cannot be written.
func (a App) say(code term.ExitCode, lines ...string) term.ExitCode {
	if _, err := io.WriteString(a.Stderr, strings.Join(lines, "\n")+"\n"); err != nil {
		return term.ExitFailure
	}
	return code
}
