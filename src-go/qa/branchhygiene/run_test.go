package branchhygiene

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// fakeGit answers the three questions the command asks.
type fakeGit struct {
	refs        string
	refsCode    int
	baseCode    int
	mergeCode   int
	mergedNames map[string]bool
	err         error
}

func (f *fakeGit) Run(ctx context.Context, c proc.Cmd) (proc.Result, error) {
	if f.err != nil {
		return proc.Result{}, f.err
	}
	switch c.Argv[1] {
	case "for-each-ref":
		return proc.Result{Stdout: f.refs, Code: f.refsCode, Stderr: "refs failed"}, nil
	case "rev-parse":
		return proc.Result{Code: f.baseCode}, nil
	case "merge-base":
		if f.mergeCode != 0 {
			return proc.Result{Code: f.mergeCode, Stderr: "fatal"}, nil
		}
		if f.mergedNames[strings.TrimPrefix(c.Argv[3], "refs/remotes/origin/")] {
			return proc.Result{}, nil
		}
		return proc.Result{Code: 1}, nil
	}
	return proc.Result{Code: 128, Stderr: "unexpected"}, ctx.Err()
}

const goodPolicy = "cap = 1\nwarn_days = 3\nfail_days = 7\nexempt = [\"F44\"]\n"

func refLine(name string, days int) string {
	return fmt.Sprintf("%s\t%d\n", name, now.Add(-time.Duration(days)*day).Unix())
}

type outcome struct {
	code     int
	out, err string
}

func runMain(g *fakeGit, policy string, args []string, ci bool) outcome {
	var out, errOut bytes.Buffer
	env := Env{
		Stdout: &out, Stderr: &errOut, Cwd: "/repo",
		Eval: func(p string) (string, error) { return p, nil },
		Now:  func() time.Time { return now }, Git: g,
		ReadFile: func(string) ([]byte, error) {
			if policy == "" {
				return nil, errors.New("no such file")
			}
			return []byte(policy), nil
		},
		Getenv: func(k string) string {
			if k == "GITHUB_ACTIONS" && ci {
				return "true"
			}
			return ""
		},
	}
	code := Main(args, env)
	return outcome{int(code), out.String(), errOut.String()}
}

func TestGreenRunExitsZero(t *testing.T) {
	g := &fakeGit{refs: refLine("F44", 0) + refLine("HEAD", 0) + refLine("feat/a", 1)}
	r := runMain(g, goodPolicy, nil, false)
	if r.code != 0 || r.err != "" || !strings.Contains(r.out, "1 open") {
		t.Fatalf("%+v", r)
	}
}

func TestFindingsExitOneGroupedWithAnnotations(t *testing.T) {
	g := &fakeGit{
		refs:        refLine("feat/old", 9) + refLine("feat/new", 1) + refLine("feat/done", 2),
		mergedNames: map[string]bool{"feat/done": true},
	}
	r := runMain(g, goodPolicy, nil, true)
	if r.code != 1 {
		t.Fatalf("%+v", r)
	}
	for _, want := range []string{"MERGED-NOT-DELETED", "OVER-CAP", "DORMANT-FAIL", "::error", "feat/done"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("missing %q in %q", want, r.out)
		}
	}
	if strings.Index(r.out, "MERGED-NOT-DELETED") > strings.Index(r.out, "OVER-CAP") {
		t.Errorf("groups out of order: %q", r.out)
	}
}

func TestWarningOnlyIsGreenWithAWarningAnnotation(t *testing.T) {
	r := runMain(&fakeGit{refs: refLine("feat/slow", 4)}, goodPolicy, nil, true)
	if r.code != 0 || !strings.Contains(r.out, "::warning") || strings.Contains(r.out, "::error") {
		t.Fatalf("%+v", r)
	}
}

func TestNoAnnotationsOutsideActions(t *testing.T) {
	r := runMain(&fakeGit{refs: refLine("feat/old", 9)}, goodPolicy, nil, false)
	if strings.Contains(r.out, "::") {
		t.Fatalf("annotation outside Actions: %q", r.out)
	}
}

func TestParkedIsShownAndNotFailing(t *testing.T) {
	policy := goodPolicy + "[[parked]]\nbranch = \"feat/old\"\nreason = \"owner decision\"\nuntil = \"2027-01-01\"\n"
	r := runMain(&fakeGit{refs: refLine("feat/old", 30)}, policy, nil, false)
	if r.code != 0 || !strings.Contains(r.out, "PARKED") || !strings.Contains(r.out, "owner decision") {
		t.Fatalf("%+v", r)
	}
}

func TestCannotJudgeIsExitTwo(t *testing.T) {
	cases := map[string]struct {
		g      *fakeGit
		policy string
		args   []string
	}{
		"no policy":      {&fakeGit{}, "", nil},
		"bad toml":       {&fakeGit{}, "cap = ", nil},
		"unknown key":    {&fakeGit{}, goodPolicy + "capp = 1\n", nil},
		"invalid policy": {&fakeGit{}, "cap = 0\nwarn_days = 3\nfail_days = 7\n", nil},
		"no origin/F44":  {&fakeGit{baseCode: 128}, goodPolicy, nil},
		"git fails":      {&fakeGit{refsCode: 128}, goodPolicy, nil},
		"git missing":    {&fakeGit{err: errors.New("no git")}, goodPolicy, nil},
		"bad row":        {&fakeGit{refs: "justaname\n"}, goodPolicy, nil},
		"bad time":       {&fakeGit{refs: "a\tyesterday\n"}, goodPolicy, nil},
		"merge-base":     {&fakeGit{refs: refLine("a", 1), mergeCode: 128}, goodPolicy, nil},
		"bad flag":       {&fakeGit{}, goodPolicy, []string{"--bogus"}},
	}
	for name, c := range cases {
		if r := runMain(c.g, c.policy, c.args, false); r.code != 2 || r.err == "" {
			t.Errorf("%s: %+v", name, r)
		}
	}
}

func TestHelpAndUsage(t *testing.T) {
	if r := runMain(&fakeGit{}, goodPolicy, []string{"--help"}, false); r.code != 0 || !strings.Contains(r.out, "usage:") {
		t.Fatalf("help: %+v", r)
	}
	if r := runMain(&fakeGit{}, goodPolicy, []string{"--nope"}, false); r.code != 2 || !strings.Contains(r.err, "usage:") {
		t.Fatalf("usage: %+v", r)
	}
}

func TestBadRootIsExitTwo(t *testing.T) {
	var errOut bytes.Buffer
	env := Env{
		Stdout: &bytes.Buffer{}, Stderr: &errOut, Cwd: "/repo",
		Eval: func(string) (string, error) { return "", errors.New("gone") },
	}
	if code := Main(nil, env); code != ExitCannotRun || errOut.Len() == 0 {
		t.Fatalf("code %d err %q", code, errOut.String())
	}
}

func TestUnwritableStdoutIsExitTwo(t *testing.T) {
	var errOut bytes.Buffer
	env := Env{
		Stdout: failWriter{}, Stderr: &errOut, Cwd: "/repo",
		Eval: func(p string) (string, error) { return p, nil },
		Now:  func() time.Time { return now }, Git: &fakeGit{},
		ReadFile: func(string) ([]byte, error) { return []byte(goodPolicy), nil },
		Getenv:   func(string) string { return "" },
	}
	if code := Main(nil, env); code != ExitCannotRun {
		t.Fatalf("code %d", code)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }
