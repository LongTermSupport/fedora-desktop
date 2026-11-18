package pushgate

import (
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/prepush"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// filteredWorkflow is a QA workflow whose push leaves out documentation, as qa.yml's does.
const filteredWorkflow = `name: QA
on:
  push:
    branches: ["F[0-9]*"]
    paths:
      - "**"
      - "!**.md"
      - "!docs/**"
      - "**/CLAUDE.md"
  pull_request:
`

func catFile(sha string) string { return "git cat-file blob " + sha + ":" + QAWorkflow }

func diffOf(from, to string) string {
	return "git diff --name-only -z --no-renames " + from + " " + to + " --"
}

const (
	headKey   = "git rev-parse --verify HEAD^{commit}"
	statusKey = "git status --porcelain -z --untracked-files=all"
	docsCheck = cwd + "/scripts/qa-changed.bash --base " + old
)

// rangeOf makes git answer the push of sha over old: qa.yml at sha is filteredWorkflow, and the
// files changed between them are these.
func (r *rig) rangeOf(sha string, files ...string) {
	r.runner.answers[catFile(sha)] = proc.Result{Stdout: filteredWorkflow}
	listed := ""
	for _, f := range files {
		listed += f + "\x00"
	}
	r.runner.answers[diffOf(old, sha)] = proc.Result{Stdout: listed}
}

// checkout makes the checkout's HEAD sha, clean or not.
func (r *rig) checkout(sha, status string) {
	r.runner.answers[headKey] = proc.Result{Stdout: sha + "\n"}
	r.runner.answers[statusKey] = proc.Result{Stdout: status}
}

// docsOnly is a docs-only push of tip over old from a clean checkout at tip, whose local docs
// check exits with code.
func docsOnly(t *testing.T, code int) *rig {
	t.Helper()
	r := newRig(t)
	r.rangeOf(tip, "README.md", "docs/guide/install.md", "docs/x.png", "CLAUDE/Plan/00001-x/JOURNAL/a.md")
	r.checkout(tip, "")
	r.runner.answers[docsCheck] = proc.Result{Code: code}
	return r
}

func (r *rig) asked(prefix string) bool {
	for _, c := range r.runner.ran {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func TestADocsOnlyRangeIsJudgedLocallyAndGoesWithoutCI(t *testing.T) {
	r := docsOnly(t, 0)
	if code := r.run(pushF44); code != ExitOK {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	if r.asked("gh ") {
		t.Fatalf("asked GitHub for a docs-only range: %v", r.runner.ran)
	}
	if !r.asked(docsCheck) {
		t.Fatalf("the local docs check did not run: %v", r.runner.ran)
	}
	r.says("refs/heads/F44", tip[:12], "docs-only", "4 files", "local docs check", "stood in for CI")
}

func TestADocsOnlyRangeWhoseDocsCheckFailsIsRefused(t *testing.T) {
	for _, code := range []int{1, 2} {
		r := docsOnly(t, code)
		if got := r.run(pushF44); got != ExitRefused {
			t.Fatalf("docs check exit %d: gate exit %d: %s", code, got, r.text())
		}
		if r.asked("gh ") {
			t.Fatalf("asked GitHub: %v", r.runner.ran)
		}
		r.says(RuleID, "docs-only", "qa-changed.bash --base "+old, "failed")
	}
}

func TestADocsCheckThatCannotStartCannotJudge(t *testing.T) {
	r := docsOnly(t, 0)
	r.runner.errs[docsCheck] = errors.New("exec: no such file")
	if code := r.run(pushF44); code != ExitCannotRun {
		t.Fatalf("exit %d: %s", code, r.text())
	}
}

// A range with no changed file leaves nothing for CI to judge: the tree is the protected line's.
func TestAnEmptyRangeIsDocsOnly(t *testing.T) {
	r := docsOnly(t, 0)
	r.rangeOf(tip)
	if code := r.run(pushF44); code != ExitOK || r.asked("gh ") {
		t.Fatalf("exit %d, ran %v: %s", code, r.runner.ran, r.text())
	}
}

func TestAMixedRangeStillNeedsCI(t *testing.T) {
	for name, files := range map[string][]string{
		"code":                   {"README.md", "scripts/qa-all.bash"},
		"an instruction file":    {"docs/a.md", "src-go/CLAUDE.md"},
		"a deleted code file":    {"docs/a.md", "playbooks/gone.yml"},
		"a rename into the code": {"docs/old.md", "src-go/new.go"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.rangeOf(tip, files...)
			r.ci()
			if code := r.run(pushF44); code != ExitRefused {
				t.Fatalf("exit %d: %s", code, r.text())
			}
			if r.asked(docsCheck) {
				t.Fatalf("ran the docs check for a range qa.yml runs for")
			}
			r.says("no QA run")
		})
	}
}

// Whatever stops the gate knowing a range is docs-only, it asks CI as before, saying why.
func TestTheGateFailsClosedToCI(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stdin string
		set   func(*rig)
		why   string
	}{
		{"a new branch", "refs/heads/F44 " + tip + " refs/heads/F44 " + prepush.ZeroID + "\n", func(*rig) {}, "no tip"},
		{"qa.yml unreadable", pushF44, func(r *rig) {
			r.runner.answers[catFile(tip)] = proc.Result{Code: 128, Stderr: "fatal: path does not exist"}
		}, "cannot read " + QAWorkflow},
		{"git cannot run", pushF44, func(r *rig) {
			r.runner.errs[catFile(tip)] = errors.New("exec: git not found")
		}, "cannot read " + QAWorkflow},
		{"filter unparsed", pushF44, func(r *rig) {
			r.runner.answers[catFile(tip)] = proc.Result{Stdout: "on: [push\n"}
		}, "push filter"},
		{"a pattern outside the matcher", pushF44, func(r *rig) {
			r.runner.answers[catFile(tip)] = proc.Result{Stdout: strings.Replace(filteredWorkflow, `"!docs/**"`, `"!{docs,notes}/**"`, 1)}
		}, "outside what this gate reads"},
		{"no filter", pushF44, func(r *rig) {
			r.runner.answers[catFile(tip)] = proc.Result{Stdout: "on: push\n"}
		}, ""},
		{"the range cannot be computed", pushF44, func(r *rig) {
			r.runner.answers[diffOf(old, tip)] = proc.Result{Code: 128, Stderr: "fatal: bad object " + old}
		}, "the files changed"},
		{"the checkout is elsewhere", pushF44, func(r *rig) { r.checkout(old, "") }, "HEAD"},
		{"the checkout is dirty", pushF44, func(r *rig) { r.checkout(tip, " M README.md\x00") }, "uncommitted"},
		{"the checkout is unreadable", pushF44, func(r *rig) {
			r.runner.answers[statusKey] = proc.Result{Code: 128, Stderr: "fatal"}
		}, "checkout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := docsOnly(t, 0)
			tc.set(r)
			r.ci(run("completed", "success", "2026-11-11T10:00:00Z", "https://example.com/run/1"))
			if code := r.run(tc.stdin); code != ExitOK {
				t.Fatalf("exit %d: %s", code, r.text())
			}
			if r.asked(docsCheck) {
				t.Fatalf("ran the docs check: %v", r.runner.ran)
			}
			if !r.asked("gh api") {
				t.Fatalf("did not ask CI: %v", r.runner.ran)
			}
			if tc.why != "" && !strings.Contains(r.err.String(), tc.why) {
				t.Fatalf("stderr does not say why (%q): %s", tc.why, r.err.String())
			}
			r.says("https://example.com/run/1")
		})
	}
}
