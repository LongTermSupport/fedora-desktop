package pushgate

import (
	"context"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/prepush"
)

// CIBranchRuleID is the identifier of a refused ci/ branch start; CLAUDE/QA.md documents it.
const CIBranchRuleID = "ci-branch-not-at-line-tip"

const ciPrefix = "refs/heads/ci/"

// lineTip is a protected line's tip as the checkout knows it on the remote.
type lineTip struct{ line, sha string }

// ciBranchStart refuses a push that creates a ci/ branch anywhere but a protected line's tip.
// GitHub diffs a new branch against the parent of the deepest commit the push carries, so a
// ci/ branch created at a commit the remote already holds (the work branch was pushed first)
// has no changed file, and qa.yml's push filter starts no run. Started at a line's tip, the
// branch then moves to the commit as a fast-forward, which GitHub diffs against that tip.
func (g *gate) ciBranchStart(ctx context.Context, ref prepush.Ref) error {
	if ref.RemoteID != prepush.ZeroID || ref.Deletes() || !strings.HasPrefix(ref.RemoteRef, ciPrefix) {
		return nil
	}
	tips, err := g.lineTips(ctx)
	if err != nil {
		return err
	}
	for _, t := range tips {
		if t.sha == ref.LocalID {
			return nil
		}
	}
	return &ciStartRefusal{ref: ref, tips: tips}
}

// lineTips is the tip of each protected line the checkout's remote-tracking refs know.
func (g *gate) lineTips(ctx context.Context) ([]lineTip, error) {
	pol, err := g.policy()
	if err != nil {
		return nil, err
	}
	args := []string{"for-each-ref", "--format=%(refname) %(objectname)"}
	for _, line := range pol.Exempt {
		args = append(args, "refs/remotes/"+g.remote+"/"+line)
	}
	listed, err := g.git(ctx, args...)
	if err != nil {
		return nil, err
	}
	var tips []lineTip
	for row := range strings.Lines(listed) {
		name, sha, ok := strings.Cut(strings.TrimSpace(row), " ")
		if ok {
			tips = append(tips, lineTip{line: strings.TrimPrefix(name, "refs/remotes/"+g.remote+"/"), sha: sha})
		}
	}
	return tips, nil
}

type ciStartRefusal struct {
	ref  prepush.Ref
	tips []lineTip
}

func (c *ciStartRefusal) Error() string {
	return "a new ci/ branch must start at a protected line's tip"
}

func (g *gate) ciStartRefused(c *ciStartRefusal) {
	ref := c.ref
	g.errOut.Line("✗ qa-push-gate [" + CIBranchRuleID + "]: " + ref.RemoteRef + " at " + ref.LocalID[:short] + ": " + c.Error() + ".")
	g.errOut.Line("  GitHub diffs a new branch against the parent of the deepest commit the push carries; a")
	g.errOut.Line("  commit the remote already holds carries none, so " + QAWorkflow + "'s push filter sees no")
	g.errOut.Line("  changed file and there would be no QA run. Start the branch at the tip of the line the")
	g.errOut.Line("  commit is built on, then push the commit to it:")
	if len(c.tips) == 0 {
		g.errOut.Line("    (no protected line's tip is known of " + g.remote + ": git fetch " + g.remote + ", then push again)")
	}
	for _, t := range c.tips {
		g.errOut.Line("    git push " + g.remote + " " + t.sha + ":" + ref.RemoteRef + "    (" + t.line + "'s tip)")
	}
	g.errOut.Line("    git push " + g.remote + " " + ref.LocalID + ":" + ref.RemoteRef)
	g.errOut.Line("  See CLAUDE/QA.md#" + CIBranchRuleID)
}
