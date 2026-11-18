package pushgate

import (
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/prepush"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const (
	f44Tip   = "4444444444444444444444444444444444444444"
	f43Tip   = "5555555555555555555555555555555555555555"
	tipsKey  = "git for-each-ref --format=%(refname) %(objectname) refs/remotes/origin/F44 refs/remotes/origin/F43 refs/remotes/origin/F44-arm"
	createCI = "refs/heads/topic " + tip + " refs/heads/ci/topic " + prepush.ZeroID + "\n"
)

// lineTips makes git answer the protected lines' tips the checkout knows of the remote.
func (r *rig) lineTips(lines string) {
	r.runner.answers[tipsKey] = proc.Result{Stdout: lines}
}

const knownTips = "refs/remotes/origin/F44 " + f44Tip + "\nrefs/remotes/origin/F43 " + f43Tip + "\n"

// A ci/ branch created at a commit the remote already holds carries no commit, so GitHub's
// push filter sees no changed file and qa.yml never runs: the branch must start at a protected
// line's tip, and the commit then reaches it as a fast-forward, diffed against that tip.
func TestACIBranchCreatedAtTheCommitIsRefused(t *testing.T) {
	r := newRig(t)
	r.lineTips(knownTips)
	if code := r.run(createCI); code != ExitRefused {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	if r.asked("gh ") {
		t.Fatalf("asked GitHub: %v", r.runner.ran)
	}
	r.says(CIBranchRuleID, "refs/heads/ci/topic", tip[:12], "no QA run",
		"git push origin "+f44Tip+":refs/heads/ci/topic", "(F44's tip)",
		"git push origin "+f43Tip+":refs/heads/ci/topic", "(F43's tip)",
		"git push origin "+tip+":refs/heads/ci/topic", "CLAUDE/QA.md#"+CIBranchRuleID)
}

func TestACIBranchCreatedAtALineTipGoes(t *testing.T) {
	for _, at := range []string{f44Tip, f43Tip} {
		r := newRig(t)
		r.lineTips(knownTips)
		if code := r.run("refs/heads/F44 " + at + " refs/heads/ci/topic " + prepush.ZeroID + "\n"); code != ExitOK {
			t.Fatalf("at %s: exit %d: %s", at[:4], code, r.text())
		}
		r.says("no protected branch moves")
	}
}

// Once it exists, the branch moves as any work branch: the fast-forward is what CI judges.
func TestACIBranchMovedOnIsNotJudged(t *testing.T) {
	r := newRig(t)
	if code := r.run("refs/heads/topic " + tip + " refs/heads/ci/topic " + f44Tip + "\n"); code != ExitOK {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	if len(r.runner.ran) != 0 {
		t.Fatalf("asked %v", r.runner.ran)
	}
}

// With no tip known (no remote-tracking ref, a push to a URL), no start is allowed, and the
// refusal says how to learn the tips.
func TestACIBranchWithNoLineTipKnownIsRefused(t *testing.T) {
	r := newRig(t)
	r.lineTips("")
	if code := r.run(createCI); code != ExitRefused {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	r.says(CIBranchRuleID, "no protected line's tip is known", "git fetch origin")
}

func TestACIBranchWhoseLineTipsCannotBeReadCannotBeJudged(t *testing.T) {
	r := newRig(t)
	r.runner.errs[tipsKey] = errors.New("exec: git not found")
	if code := r.run(createCI); code != ExitCannotRun {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	r.runner.errs = map[string]error{}
	r.runner.answers[tipsKey] = proc.Result{Code: 128, Stderr: "fatal: bad"}
	r.out.Reset()
	r.err.Reset()
	if code := r.run(createCI); code != ExitCannotRun {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	if !strings.Contains(r.err.String(), "fatal: bad") {
		t.Fatalf("stderr does not carry git's error: %s", r.err.String())
	}
}

func TestOnlyACIBranchIsHeldToALineTip(t *testing.T) {
	r := newRig(t)
	if code := r.run("refs/heads/topic " + tip + " refs/heads/cix/topic " + prepush.ZeroID + "\n" +
		"refs/heads/topic " + tip + " refs/heads/topic " + prepush.ZeroID + "\n"); code != ExitOK {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	if len(r.runner.ran) != 0 {
		t.Fatalf("asked %v", r.runner.ran)
	}
}
