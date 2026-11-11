package ciscope

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/branchhygiene"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const (
	cwd    = "/repo"
	f44Tip = "4444444444444444444444444444444444444444"
	f43Tip = "3333333333333333333333333333333333333333"
	armTip = "5555555555555555555555555555555555555555"
	policy = "cap = 10\nwarn_days = 3\nfail_days = 7\nexempt = [\"F44\", \"F42\", \"F43\", \"F44-arm\"]\n"
)

// lineTips is the remote's protected lines; F42 is absent from origin.
var lineTips = map[string]string{"F44": f44Tip, "F43": f43Tip, "F44-arm": armTip}

// fake is git over a remote with lineTips and a HEAD containing the tips in contains; a command
// it was not told about fails the test.
type fake struct {
	t        *testing.T
	tips     map[string]string
	contains map[string]bool
	broken   string
	ran      []string
}

func newFake(t *testing.T, contains ...string) *fake {
	t.Helper()
	f := &fake{t: t, tips: maps.Clone(lineTips), contains: map[string]bool{}}
	for _, c := range contains {
		f.contains[f.tips[c]] = true
	}
	return f
}

func (f *fake) Run(_ context.Context, c proc.Cmd) (proc.Result, error) {
	key := strings.Join(c.Argv, " ")
	f.ran = append(f.ran, key)
	if c.Dir != cwd {
		f.t.Errorf("%q run in %q, not the checkout", key, c.Dir)
	}
	if f.broken != "" && strings.Contains(key, f.broken) {
		return proc.Result{Code: 128, Stderr: "fatal: broken"}, nil
	}
	if ref, ok := strings.CutPrefix(key, "git rev-parse --verify --quiet refs/remotes/origin/"); ok {
		name := strings.TrimSuffix(ref, "^{commit}")
		if sha, found := f.tips[name]; found {
			return proc.Result{Stdout: sha + "\n"}, nil
		}
		return proc.Result{Code: 1}, nil
	}
	if sha, ok := strings.CutPrefix(key, "git merge-base --is-ancestor "); ok {
		if f.contains[strings.TrimSuffix(sha, " HEAD")] {
			return proc.Result{}, nil
		}
		return proc.Result{Code: 1}, nil
	}
	f.t.Errorf("unexpected command %q", key)
	return proc.Result{Code: 2, Stderr: "unexpected"}, nil
}

type rig struct {
	git      *fake
	out, err bytes.Buffer
	policy   string
}

func (r *rig) readFile(path string) ([]byte, error) {
	if path != filepath.Join(cwd, branchhygiene.PolicyFile) {
		return nil, errors.New("no such file " + path)
	}
	if r.policy == "" {
		return nil, os.ErrNotExist
	}
	return []byte(r.policy), nil
}

func (r *rig) run(args ...string) ExitCode {
	return Main(context.Background(), args, Env{Stdout: &r.out, Stderr: &r.err, Runner: r.git, Cwd: cwd, ReadFile: r.readFile})
}

func newRig(t *testing.T, contains ...string) *rig {
	t.Helper()
	return &rig{git: newFake(t, contains...), policy: policy}
}

func push(ref string) []string { return []string{"--event", "push", "--ref", ref, "--default", "F44"} }

func (r *rig) wantScope(t *testing.T, code ExitCode, mode, line, base string) {
	t.Helper()
	if code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, r.err.String())
	}
	want := "mode=" + mode + "\nline=" + line + "\nbase=" + base + "\n"
	if r.out.String() != want {
		t.Fatalf("stdout %q, want %q", r.out.String(), want)
	}
	if !strings.Contains(r.err.String(), "qa-ci-scope: ") {
		t.Errorf("no reason on stderr: %q", r.err.String())
	}
}

// C1: a release tag runs the full suite, and git is not asked.
func TestATagRunsTheFullSuite(t *testing.T) {
	r := newRig(t)
	r.wantScope(t, r.run(push("refs/tags/44.1.0")...), "full", "", "")
	if len(r.git.ran) != 0 {
		t.Errorf("git asked %v", r.git.ran)
	}
}

// C2: a push to a protected branch runs the full suite after the move, for every line the policy
// protects.
func TestAProtectedBranchRunsTheFullSuite(t *testing.T) {
	for _, branch := range []string{"F44", "F43", "F44-arm", "F42"} {
		r := newRig(t)
		r.wantScope(t, r.run(push("refs/heads/"+branch)...), "full", branch, "")
	}
}

// C3: a release-<version> branch, which the release workflow cuts and whose run its tag job
// reads, runs the full suite.
func TestAReleaseBranchRunsTheFullSuite(t *testing.T) {
	r := newRig(t)
	r.wantScope(t, r.run(push("refs/heads/release-44.2.0")...), "full", "", "")
}

// C4: a work branch that contains the default branch's tip is judged in diff mode against it,
// even when it contains another line too.
func TestAWorkBranchOnTheDefaultLineRunsDiffModeAgainstItsTip(t *testing.T) {
	r := newRig(t, "F44", "F43")
	r.wantScope(t, r.run(push("refs/heads/ci/topic")...), "diff", "F44", f44Tip)
}

// C5: a work branch built on another protected line is judged against that line's tip.
func TestAWorkBranchOnAnotherLineRunsDiffModeAgainstThatLine(t *testing.T) {
	for _, line := range []string{"F43", "F44-arm"} {
		r := newRig(t, line)
		r.wantScope(t, r.run(push("refs/heads/ci/topic")...), "diff", line, lineTips[line])
	}
}

// C6: a work branch that contains no protected line's tip is refused: its base is stale, and a
// diff against it would judge changes the line already has.
func TestAWorkBranchOnNoLineIsRefused(t *testing.T) {
	r := newRig(t)
	if code := r.run(push("refs/heads/ci/topic")...); code != ExitRefused {
		t.Fatalf("exit %d, stdout %q", code, r.out.String())
	}
	if r.out.String() != "" || !strings.Contains(r.err.String(), "merge F44") || !strings.Contains(r.err.String(), "F43") {
		t.Fatalf("stdout %q stderr %q", r.out.String(), r.err.String())
	}
}

// C7: a work branch that contains two lines other than the default names no one line, so it is
// refused rather than judged against a guess.
func TestAWorkBranchOnTwoOtherLinesIsRefused(t *testing.T) {
	r := newRig(t, "F43", "F44-arm")
	if code := r.run(push("refs/heads/ci/topic")...); code != ExitRefused {
		t.Fatalf("exit %d, stdout %q", code, r.out.String())
	}
	if !strings.Contains(r.err.String(), "F43 F44-arm") {
		t.Fatalf("stderr %q", r.err.String())
	}
}

// C8: a pull request is judged in diff mode against its base branch's tip.
func TestAPullRequestRunsDiffModeAgainstItsBase(t *testing.T) {
	r := newRig(t)
	code := r.run("--event", "pull_request", "--ref", "refs/pull/7/merge", "--default", "F44", "--pr-base", "F43")
	r.wantScope(t, code, "diff", "F43", f43Tip)
}

// C9: a pull request whose base is not on origin cannot be judged.
func TestAPullRequestWithAnUnknownBaseCannotBeJudged(t *testing.T) {
	r := newRig(t)
	code := r.run("--event", "pull_request", "--ref", "refs/pull/7/merge", "--default", "F44", "--pr-base", "gone")
	if code != ExitCannotRun || r.out.String() != "" {
		t.Fatalf("exit %d, stdout %q", code, r.out.String())
	}
}

// C10: git failing is a run that cannot judge, never a scope.
func TestAGitFailureCannotJudge(t *testing.T) {
	for _, broken := range []string{"rev-parse", "merge-base"} {
		r := newRig(t, "F44")
		r.git.broken = broken
		if code := r.run(push("refs/heads/ci/topic")...); code != ExitCannotRun || r.out.String() != "" {
			t.Fatalf("%s: exit %d, stdout %q", broken, code, r.out.String())
		}
	}
}

// C11: a missing policy cannot be judged.
func TestAMissingPolicyCannotJudge(t *testing.T) {
	r := newRig(t, "F44")
	r.policy = ""
	if code := r.run(push("refs/heads/ci/topic")...); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
}

// C12: bad usage is exit 2 with the usage on stderr; --help prints it on stdout.
func TestUsage(t *testing.T) {
	bad := [][]string{
		{},
		{"--event", "push"},
		{"--event", "schedule", "--ref", "refs/heads/x", "--default", "F44"},
		{"--event", "push", "--ref", "refs/heads/x", "--default", "F44", "--pr-base", "F44"},
		{"--event", "pull_request", "--ref", "refs/pull/1/merge", "--default", "F44"},
		{"--event", "push", "--ref", "HEAD", "--default", "F44"},
		{"--event", "push", "--ref", "refs/heads/x", "--default", "F44", "--stray"},
		{"--event", "push", "--ref", "refs/heads/x", "--default"},
		{"--event", "push", "--ref", "--default", "F44"},
		{"--event", "push", "--ref", "refs/heads/x", "--default", "F44", "--event", "push"},
	}
	for _, args := range bad {
		r := newRig(t)
		if code := r.run(args...); code != ExitCannotRun || !strings.Contains(r.err.String(), "usage: qa-ci-scope") {
			t.Errorf("%q: exit %d, stderr %q", args, code, r.err.String())
		}
	}
	r := newRig(t)
	if code := r.run("--help"); code != ExitOK || !strings.HasPrefix(r.out.String(), "usage: qa-ci-scope") {
		t.Fatalf("--help: exit %d, stdout %q", code, r.out.String())
	}
}
