package pushgate

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/prepush"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/qaverdict"
)

const (
	judged = "1111111111111111111111111111111111111111"
	tip    = "2222222222222222222222222222222222222222"
	old    = "3333333333333333333333333333333333333333"
	later  = "4444444444444444444444444444444444444444"
	cwd    = "/repo"
)

var (
	started  = time.Unix(1000, 0).UTC()
	finished = time.Unix(2000, 0).UTC()
)

// fake answers git by its argv; a command it was not told about fails the test.
type fake struct {
	t       *testing.T
	answers map[string]proc.Result
	ran     []string
}

func (f *fake) Run(_ context.Context, c proc.Cmd) (proc.Result, error) {
	key := strings.Join(c.Argv, " ")
	f.ran = append(f.ran, key)
	res, ok := f.answers[key]
	if !ok {
		f.t.Errorf("unexpected command %q", key)
		return proc.Result{Code: 128, Stderr: "unexpected"}, nil
	}
	return res, nil
}

type rig struct {
	t        *testing.T
	runner   *fake
	common   string
	out, err bytes.Buffer
	env      map[string]string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	common := t.TempDir()
	r := &rig{t: t, common: common, env: map[string]string{}}
	r.runner = &fake{t: t, answers: map[string]proc.Result{
		"git -C /repo rev-parse --verify --quiet HEAD^{commit}":           {Stdout: tip + "\n"},
		"git -C /repo rev-parse --path-format=absolute --git-common-dir":  {Stdout: common + "\n"},
		"git -C /repo rev-list " + tip + " --not --remotes=origin " + old: {Stdout: tip + "\n" + later + "\n"},
		"git -C /repo merge-base --is-ancestor " + judged + " " + tip:     {},
		"git -C /repo log --format=%H %ct " + judged + ".." + tip:         {Stdout: tip + " 1500\n" + later + " 1999\n"},
	}}
	return r
}

func (r *rig) verdict(status string) {
	r.t.Helper()
	rec := qaverdict.Record{Status: status, Commit: judged, Started: started, Finished: finished}
	if err := qaverdict.Write(filepath.Join(r.common, qaverdict.FileName), rec); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) run(stdin string) ExitCode {
	r.t.Helper()
	return Main(context.Background(), []string{"origin", "git@example.com:o/r.git"}, Env{
		Stdin:  strings.NewReader(stdin),
		Stdout: &r.out,
		Stderr: &r.err,
		Runner: r.runner,
		Cwd:    cwd,
		Getenv: func(k string) string { return r.env[k] },
	})
}

func (r *rig) text() string { return r.out.String() + r.err.String() }

const pushF44 = "refs/heads/F44 " + tip + " refs/heads/F44 " + old + "\n"

func TestAPushAfterAPassingRunOfAnAncestorInTheSameBurstIsAllowed(t *testing.T) {
	r := newRig(t)
	r.verdict(qaverdict.StatusPass)
	if code := r.run(pushF44); code != ExitOK {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	if !strings.Contains(r.out.String(), "qa-all passed at "+judged[:12]) {
		t.Fatalf("the pass does not name the judged commit: %s", r.text())
	}
}

func TestAPushAfterAFailedRunIsRefused(t *testing.T) {
	r := newRig(t)
	r.verdict(qaverdict.StatusFail)
	if code := r.run(pushF44); code != ExitRefused {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	for _, want := range []string{RuleID, "FAILED", judged[:12]} {
		if !strings.Contains(r.err.String(), want) {
			t.Errorf("%q missing from %s", want, r.err.String())
		}
	}
}

func TestAPushWithNoVerdictIsRefused(t *testing.T) {
	r := newRig(t)
	if code := r.run(pushF44); code != ExitRefused {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	if !strings.Contains(r.err.String(), "no qa-all verdict") {
		t.Fatalf("does not say why: %s", r.err.String())
	}
}

func TestADamagedVerdictCannotBeJudged(t *testing.T) {
	r := newRig(t)
	r.verdict(qaverdict.StatusPass)
	if err := writeFile(filepath.Join(r.common, qaverdict.FileName), `{"status":"pass"}`); err != nil {
		t.Fatal(err)
	}
	if code := r.run(pushF44); code != ExitCannotRun {
		t.Fatalf("exit %d: %s", code, r.text())
	}
}

func TestAPassThatIsNotInThePushIsRefused(t *testing.T) {
	r := newRig(t)
	r.verdict(qaverdict.StatusPass)
	r.runner.answers["git -C /repo merge-base --is-ancestor "+judged+" "+tip] = proc.Result{Code: 1}
	if code := r.run(pushF44); code != ExitRefused {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	if !strings.Contains(r.err.String(), "not in what is pushed") {
		t.Fatalf("does not say why: %s", r.err.String())
	}
}

func TestAnAncestorCheckGitCannotAnswerCannotBeJudged(t *testing.T) {
	r := newRig(t)
	r.verdict(qaverdict.StatusPass)
	r.runner.answers["git -C /repo merge-base --is-ancestor "+judged+" "+tip] = proc.Result{Code: 128, Stderr: "bad object"}
	if code := r.run(pushF44); code != ExitCannotRun {
		t.Fatalf("exit %d: %s", code, r.text())
	}
}

func TestACommitMadeAfterTheRunEndedIsRefused(t *testing.T) {
	r := newRig(t)
	r.verdict(qaverdict.StatusPass)
	r.runner.answers["git -C /repo log --format=%H %ct "+judged+".."+tip] = proc.Result{Stdout: tip + " 2001\n" + later + " 1999\n"}
	if code := r.run(pushF44); code != ExitRefused {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	if !strings.Contains(r.err.String(), tip[:12]+" was committed after") {
		t.Fatalf("does not name the commit: %s", r.err.String())
	}
}

func TestAPushOfNothingNewNeedsNoVerdict(t *testing.T) {
	r := newRig(t)
	r.runner.answers["git -C /repo rev-list "+tip+" --not --remotes=origin "+old] = proc.Result{}
	if code := r.run(pushF44); code != ExitOK {
		t.Fatalf("exit %d: %s", code, r.text())
	}
}

func TestADeletionAndNoRefsPushNoCommit(t *testing.T) {
	r := newRig(t)
	if code := r.run("refs/heads/x " + prepush.ZeroID + " refs/heads/x " + old + "\n"); code != ExitOK {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	r = newRig(t)
	if code := r.run(""); code != ExitOK {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	if len(r.runner.ran) != 0 {
		t.Fatalf("asked git %v for a push of nothing", r.runner.ran)
	}
}

func TestEveryRefIsJudged(t *testing.T) {
	r := newRig(t)
	r.verdict(qaverdict.StatusPass)
	other := "5555555555555555555555555555555555555555"
	r.runner.answers["git -C /repo rev-list "+other+" --not --remotes=origin"] = proc.Result{Stdout: other + "\n"}
	r.runner.answers["git -C /repo merge-base --is-ancestor "+judged+" "+other] = proc.Result{Code: 1}
	code := r.run(pushF44 + "refs/heads/side " + other + " refs/heads/side " + prepush.ZeroID + "\n")
	if code != ExitRefused {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	if !strings.Contains(r.err.String(), "refs/heads/side") {
		t.Fatalf("does not name the refused ref: %s", r.err.String())
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

func writeFile(path, body string) error { return os.WriteFile(path, []byte(body), 0o600) }

func TestBadUsageAndBadStdinCannotRun(t *testing.T) {
	r := newRig(t)
	if code := Main(context.Background(), nil, Env{Stdin: strings.NewReader(""), Stdout: &r.out, Stderr: &r.err, Runner: r.runner, Cwd: cwd, Getenv: func(string) string { return "" }}); code != ExitCannotRun {
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
