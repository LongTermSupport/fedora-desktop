package pushgate

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/branchhygiene"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/prepush"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const (
	tip       = "2222222222222222222222222222222222222222"
	old       = "3333333333333333333333333333333333333333"
	cwd       = "/repo"
	remoteURL = "git@github.com:owner/repo.git"
	runs      = "gh api repos/owner/repo/actions/runs?head_sha=" + tip + "&event=push&per_page=100 --jq " + runsFilter
	qa        = `"path":".github/workflows/qa.yml","event":"push","headSha":"` + tip + `"`
)

const policy = `cap = 10
warn_days = 3
fail_days = 7
exempt = ["F44", "F43", "F44-arm"]
`

// fake answers each command by its argv; a command it was not told about fails the test.
type fake struct {
	t       *testing.T
	answers map[string]proc.Result
	errs    map[string]error
	ran     []string
}

func (f *fake) Run(_ context.Context, c proc.Cmd) (proc.Result, error) {
	key := strings.Join(c.Argv, " ")
	f.ran = append(f.ran, key)
	if err, ok := f.errs[key]; ok {
		return proc.Result{}, err
	}
	res, ok := f.answers[key]
	if !ok {
		f.t.Errorf("unexpected command %q", key)
		return proc.Result{Code: 1, Stderr: "unexpected"}, nil
	}
	return res, nil
}

type rig struct {
	t        *testing.T
	runner   *fake
	out, err bytes.Buffer
	env      map[string]string
	policy   string
	url      string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	return &rig{
		t: t, env: map[string]string{}, policy: policy, url: remoteURL,
		runner: &fake{t: t, answers: map[string]proc.Result{}, errs: map[string]error{}},
	}
}

// listing is GitHub's answer listing these runs, each given as a JSON object's fields.
func listing(objects ...string) string {
	wrapped := make([]string, 0, len(objects))
	for _, o := range objects {
		wrapped = append(wrapped, "{"+o+"}")
	}
	return `{"totalCount":` + strconv.Itoa(len(objects)) + `,"runs":[` + strings.Join(wrapped, ",") + "]}"
}

// ci makes GitHub answer the runs of tip with these runs.
func (r *rig) ci(objects ...string) {
	r.runner.answers[runs] = proc.Result{Stdout: listing(objects...)}
}

func run(status, conclusion, created, link string) string {
	return qa + `,"status":"` + status + `","conclusion":"` + conclusion + `","createdAt":"` + created + `","htmlUrl":"` + link + `"`
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

func (r *rig) run(stdin string) ExitCode {
	r.t.Helper()
	return Main(context.Background(), []string{"origin", r.url}, Env{
		Stdin:    strings.NewReader(stdin),
		Stdout:   &r.out,
		Stderr:   &r.err,
		Runner:   r.runner,
		Cwd:      cwd,
		Getenv:   func(k string) string { return r.env[k] },
		ReadFile: r.readFile,
	})
}

func (r *rig) text() string { return r.out.String() + r.err.String() }

func (r *rig) says(want ...string) {
	r.t.Helper()
	for _, w := range want {
		if !strings.Contains(r.text(), w) {
			r.t.Errorf("%q missing from:\n%s", w, r.text())
		}
	}
}

const pushF44 = "refs/heads/F44 " + tip + " refs/heads/F44 " + old + "\n"

func TestAProtectedBranchMovesToACommitCIPassed(t *testing.T) {
	r := newRig(t)
	r.ci(run("completed", "success", "2026-11-11T10:00:00Z", "https://example.com/run/1"))
	if code := r.run(pushF44); code != ExitOK {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	r.says("refs/heads/F44", tip[:12], "https://example.com/run/1")
}

func TestEveryProtectedBranchOfThePolicyIsJudged(t *testing.T) {
	for _, branch := range []string{"F43", "F44-arm"} {
		r := newRig(t)
		r.ci()
		if code := r.run("refs/heads/x " + tip + " refs/heads/" + branch + " " + old + "\n"); code != ExitRefused {
			t.Fatalf("%s: exit %d: %s", branch, code, r.text())
		}
		r.says(tip + ":" + branch)
	}
}

func TestAnyPassingRunOfTheCommitIsEnough(t *testing.T) {
	r := newRig(t)
	r.ci(run("completed", "failure", "2026-11-11T11:00:00Z", "https://example.com/run/2"),
		run("completed", "success", "2026-11-11T10:00:00Z", "https://example.com/run/1"))
	if code := r.run(pushF44); code != ExitOK {
		t.Fatalf("exit %d: %s", code, r.text())
	}
}

func TestACommitCIHasNotPassedIsRefusedSayingWhy(t *testing.T) {
	for _, tc := range []struct {
		name string
		runs []string
		want []string
	}{
		{"missing", nil, []string{"no QA run"}},
		{"in progress", []string{run("in_progress", "", "2026-11-11T10:00:00Z", "https://example.com/run/3")}, []string{"in_progress", "https://example.com/run/3"}},
		{"queued", []string{run("queued", "", "2026-11-11T10:00:00Z", "https://example.com/run/3")}, []string{"queued", "https://example.com/run/3"}},
		{"failed", []string{run("completed", "failure", "2026-11-11T10:00:00Z", "https://example.com/run/4")}, []string{"failure", "https://example.com/run/4"}},
		{"cancelled", []string{run("completed", "cancelled", "2026-11-11T10:00:00Z", "https://example.com/run/5")}, []string{"cancelled", "https://example.com/run/5"}},
		{"the newest of several", []string{
			run("completed", "failure", "2026-11-11T09:00:00Z", "https://example.com/run/old"),
			run("in_progress", "", "2026-11-11T10:00:00Z", "https://example.com/run/new"),
		}, []string{"in_progress", "https://example.com/run/new"}},
		{"another workflow's pass", []string{strings.Replace(run("completed", "success", "2026-11-11T10:00:00Z", "https://example.com/run/6"), "qa.yml", "other.yml", 1)}, []string{"no QA run"}},
		{"a pull request's pass", []string{strings.Replace(run("completed", "success", "2026-11-11T10:00:00Z", "https://example.com/run/7"), `"push"`, `"pull_request"`, 1)}, []string{"no QA run"}},
		{"another commit's pass", []string{strings.Replace(run("completed", "success", "2026-11-11T10:00:00Z", "https://example.com/run/8"), tip, old, 1)}, []string{"no QA run"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.ci(tc.runs...)
			if code := r.run(pushF44); code != ExitRefused {
				t.Fatalf("exit %d: %s", code, r.text())
			}
			r.says(append(tc.want, RuleID, tip[:12], "refs/heads/ci/", "gh run watch", tip+":F44")...)
		})
	}
}

func TestANonProtectedRefNeedsNoVerdict(t *testing.T) {
	r := newRig(t)
	if code := r.run("refs/heads/F44 " + tip + " refs/heads/ci/topic " + prepush.ZeroID + "\n" +
		"refs/tags/44.1.0 " + tip + " refs/tags/44.1.0 " + prepush.ZeroID + "\n"); code != ExitOK {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	if len(r.runner.ran) != 0 {
		t.Fatalf("asked %v for a push to no protected branch", r.runner.ran)
	}
}

func TestADeletionAPushOfNothingNewAndNoRefsPass(t *testing.T) {
	for name, stdin := range map[string]string{
		"deletion":    "(delete) " + prepush.ZeroID + " refs/heads/F44 " + old + "\n",
		"nothing new": "refs/heads/F44 " + old + " refs/heads/F44 " + old + "\n",
		"no refs":     "",
	} {
		r := newRig(t)
		if code := r.run(stdin); code != ExitOK {
			t.Fatalf("%s: exit %d: %s", name, code, r.text())
		}
		if len(r.runner.ran) != 0 {
			t.Fatalf("%s: asked %v", name, r.runner.ran)
		}
	}
}

func TestEveryRefIsJudged(t *testing.T) {
	r := newRig(t)
	r.ci(run("completed", "success", "2026-11-11T10:00:00Z", "https://example.com/run/1"))
	other := "5555555555555555555555555555555555555555"
	r.runner.answers["gh api repos/owner/repo/actions/runs?head_sha="+other+"&event=push&per_page=100 --jq "+runsFilter] = proc.Result{Stdout: listing()}
	code := r.run(pushF44 + "refs/heads/x " + other + " refs/heads/F43 " + old + "\n")
	if code != ExitRefused {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	r.says("refs/heads/F43", other[:12])
}

func TestGHFailingCannotJudge(t *testing.T) {
	for name, set := range map[string]func(*rig){
		"no gh":    func(r *rig) { r.runner.errs[runs] = errors.New(`exec: "gh": executable file not found in $PATH`) },
		"not JSON": func(r *rig) { r.runner.answers[runs] = proc.Result{Stdout: "<html>"} },
		"cut short": func(r *rig) {
			r.runner.answers[runs] = proc.Result{Stdout: `{"total_count":101,"workflow_runs":[]}`}
		},
		"timed out":     func(r *rig) { r.runner.answers[runs] = proc.Result{TimedOut: true, Code: -1} },
		"bad timestamp": func(r *rig) { r.ci(run("completed", "failure", "yesterday", "https://example.com/run/1")) },
	} {
		r := newRig(t)
		set(r)
		if code := r.run(pushF44); code != ExitCannotRun {
			t.Errorf("%s: exit %d: %s", name, code, r.text())
		}
	}
}

func TestUnauthenticatedGHIsReported(t *testing.T) {
	r := newRig(t)
	r.runner.answers[runs] = proc.Result{Code: 4, Stderr: "gh: To get started with GitHub CLI, please run:  gh auth login"}
	if code := r.run(pushF44); code != ExitCannotRun {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	r.says("gh auth login")
}

func TestTheRepositoryIsReadFromTheRemoteURL(t *testing.T) {
	for _, u := range []string{
		"git@github.com:owner/repo.git",
		"git@github.com:owner/repo",
		"https://github.com/owner/repo.git",
		"https://github.com/owner/repo",
		"ssh://git@github.com/owner/repo.git",
		"ssh://git@github.com:22/owner/repo.git",
	} {
		r := newRig(t)
		r.url = u
		r.ci(run("completed", "success", "2026-11-11T10:00:00Z", "https://example.com/run/1"))
		if code := r.run(pushF44); code != ExitOK {
			t.Errorf("%s: exit %d: %s", u, code, r.text())
		}
	}
	for _, u := range []string{"/srv/git/remote.git", "file:///srv/git/remote.git", "https://example.com/a/b/c.git", "git@github.com:repo.git", "https://github.com/owner/.git"} {
		r := newRig(t)
		r.url = u
		if code := r.run(pushF44); code != ExitCannotRun {
			t.Errorf("%s: exit %d: %s", u, code, r.text())
		}
	}
}

func TestAPolicyThatCannotBeReadCannotJudge(t *testing.T) {
	for name, body := range map[string]string{"missing": "", "malformed": "exempt = 3\n"} {
		r := newRig(t)
		r.policy = body
		if code := r.run(pushF44); code != ExitCannotRun {
			t.Errorf("%s: exit %d: %s", name, code, r.text())
		}
	}
}

// The checkout's own policy protects the release and architecture lines and nothing a work
// branch is called.
func TestTheRepositorysPolicyProtectsTheReleaseLines(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	pol, err := branchhygiene.ReadPolicy(os.ReadFile, root)
	if err != nil {
		t.Fatal(err)
	}
	for ref, want := range map[string]bool{
		"refs/heads/F44": true, "refs/heads/F43": true, "refs/heads/F44-arm": true,
		"refs/heads/ci/topic": false, "refs/heads/feat/F44": false, "refs/tags/F44": false,
	} {
		if got := protected(pol, ref) != ""; got != want {
			t.Errorf("%s protected = %t, want %t", ref, got, want)
		}
	}
}

func TestAnExemptionIsNamedAndPasses(t *testing.T) {
	r := newRig(t)
	r.env[ExemptEnv] = "release commit, judged by CI on its pull request"
	if code := r.run(pushF44); code != ExitOK {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	if !strings.Contains(r.err.String(), "release commit, judged by CI") || len(r.runner.ran) != 0 {
		t.Fatalf("an exemption must be printed with its reason and judge nothing: %s %v", r.err.String(), r.runner.ran)
	}
}

func TestBadUsageAndBadStdinCannotRun(t *testing.T) {
	r := newRig(t)
	if code := Main(context.Background(), nil, Env{Stdin: strings.NewReader(""), Stdout: &r.out, Stderr: &r.err, Runner: r.runner, Cwd: cwd, Getenv: func(string) string { return "" }, ReadFile: r.readFile}); code != ExitCannotRun {
		t.Fatalf("no remote: exit %d", code)
	}
	if code := r.run("refs/heads/F44 " + tip + "\n"); code != ExitCannotRun {
		t.Fatalf("a short ref line: exit %d", code)
	}
}

func TestHelpIsTheUsageOnStdout(t *testing.T) {
	r := newRig(t)
	if code := Main(context.Background(), []string{"--help"}, Env{Stdout: &r.out, Stderr: &r.err, Getenv: func(string) string { return "" }}); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.HasPrefix(r.out.String(), "usage: qa-push-gate ") {
		t.Fatalf("usage: %s", r.out.String())
	}
}
