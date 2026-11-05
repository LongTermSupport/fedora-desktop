package branchdelete

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const goodPolicy = "cap = 10\nwarn_days = 3\nfail_days = 7\nexempt = [\"F44\", \"F42\", \"F43\"]\n"

// lsRemote is what `git ls-remote --symref origin` prints for the fake origin.
const lsRemote = "ref: refs/heads/F44\tHEAD\n" +
	tipA + "\tHEAD\n" +
	tipA + "\trefs/heads/F44\n" +
	tipA + "\trefs/heads/F42\n" +
	tipA + "\trefs/heads/feat/done\n" +
	tipB + "\trefs/heads/fix/old\n" +
	tipA + "\trefs/heads/feat/unarchived\n" +
	tipA + "\trefs/archive/F42\n" +
	tipA + "\trefs/archive/feat/done\n" +
	tipB + "\trefs/archive/fix/old\n" +
	tipB + "\trefs/pull/7/head\n" +
	tipA + "\trefs/tags/44.1.0\n"

type fakeGit struct {
	stdout string
	code   int
	err    error
	calls  []proc.Cmd
}

func (f *fakeGit) Run(ctx context.Context, c proc.Cmd) (proc.Result, error) {
	f.calls = append(f.calls, c)
	if f.err != nil {
		return proc.Result{}, f.err
	}
	return proc.Result{Stdout: f.stdout, Code: f.code, Stderr: "fatal: unable to access origin"}, ctx.Err()
}

type outcome struct {
	code     ExitCode
	out, err string
}

type world struct {
	git    *fakeGit
	policy string
	stdin  string
	files  map[string]string
	stdout writer
}

type writer interface{ Write(p []byte) (int, error) }

func newWorld() *world {
	return &world{git: &fakeGit{stdout: lsRemote}, policy: goodPolicy, files: map[string]string{}}
}

func (w *world) run(args ...string) outcome {
	var out, errOut bytes.Buffer
	var stdout writer = &out
	if w.stdout != nil {
		stdout = w.stdout
	}
	env := Env{
		Stdin: strings.NewReader(w.stdin), Stdout: stdout, Stderr: &errOut, Cwd: "/work",
		Eval: func(p string) (string, error) { return p, nil },
		Git:  w.git,
		ReadFile: func(p string) ([]byte, error) {
			if p == "/repo/scripts/branch-policy.toml" && w.policy != "" {
				return []byte(w.policy), nil
			}
			if body, ok := w.files[p]; ok {
				return []byte(body), nil
			}
			return nil, errors.New("open " + p + ": no such file or directory")
		},
	}
	code := Main(append([]string{"--root", "/repo"}, args...), env)
	return outcome{code, out.String(), errOut.String()}
}

func newWorldRun(stdin string) outcome {
	w := newWorld()
	w.stdin = stdin
	return w.run()
}

func TestPassingCandidatesFromStdinPrintOneDeleteCommandEach(t *testing.T) {
	w := newWorld()
	w.stdin = "feat/done\nfix/old\n"
	got := w.run()
	if got.code != ExitOK {
		t.Fatalf("exit %d: %s", got.code, got.err)
	}
	want := "git push origin --delete feat/done\ngit push origin --delete fix/old\n"
	if got.out != want {
		t.Fatalf("stdout %q", got.out)
	}
	if !strings.Contains(got.err, "2 candidates pass") || strings.Contains(newWorldRun("feat/done\n").err, "1 candidates") {
		t.Fatalf("stderr %q", got.err)
	}
	c := w.git.calls[0]
	if strings.Join(c.Argv, " ") != "git ls-remote --symref origin" || c.Dir != "/repo" {
		t.Fatalf("asked %+v", c)
	}
}

func TestCandidatesFromAFileRelativeToTheWorkingDirectory(t *testing.T) {
	w := newWorld()
	w.files["/work/list.txt"] = "feat/done\n"
	if got := w.run("list.txt"); got.code != ExitOK || got.out != "git push origin --delete feat/done\n" {
		t.Fatalf("%+v", got)
	}
	w.files["/abs/list.txt"] = "fix/old\n"
	if got := w.run("/abs/list.txt"); got.code != ExitOK || got.out != "git push origin --delete fix/old\n" {
		t.Fatalf("%+v", got)
	}
	w.stdin = "feat/done\n"
	if got := w.run("-"); got.code != ExitOK {
		t.Fatalf("- is stdin: %+v", got)
	}
}

// One offender refuses the whole list: no partial delete list reaches stdout.
func TestARefusalPrintsNoDeleteListAndNamesEachOffender(t *testing.T) {
	w := newWorld()
	w.stdin = "feat/done\nF42\nfeat/unarchived\nF44\n"
	got := w.run()
	if got.code != ExitRefused {
		t.Fatalf("exit %d", got.code)
	}
	if got.out != "" {
		t.Fatalf("stdout must be empty, got %q", got.out)
	}
	for _, want := range []string{"REFUSED F42:", "REFUSED feat/unarchived:", "REFUSED F44:", "default branch", "3 of 4 candidates refused"} {
		if !strings.Contains(got.err, want) {
			t.Errorf("stderr lacks %q:\n%s", want, got.err)
		}
	}
	if strings.Contains(got.err, "REFUSED feat/done") {
		t.Error("feat/done passes")
	}
}

func TestCouldNotJudgeIsExitTwo(t *testing.T) {
	cases := map[string]func(w *world) []string{
		"no candidates":   func(w *world) []string { w.stdin = "\n\n"; return nil },
		"policy missing":  func(w *world) []string { w.policy = ""; return nil },
		"policy invalid":  func(w *world) []string { w.policy = "cap = 0\n"; return nil },
		"list unreadable": func(_ *world) []string { return []string{"missing.txt"} },
		"git fails":       func(w *world) []string { w.git.code = 128; return nil },
		"git cannot start": func(w *world) []string {
			w.git.err = errors.New("exec: git not found")
			return nil
		},
		"no default branch": func(w *world) []string {
			w.git.stdout = tipA + "\trefs/heads/feat/done\n"
			return nil
		},
		"unreadable ls-remote": func(w *world) []string { w.git.stdout = "garbage\n"; return nil },
		"two files":            func(_ *world) []string { return []string{"a", "b"} },
		"unknown flag":         func(_ *world) []string { return []string{"--nope"} },
		"root needs a value":   func(_ *world) []string { return []string{"--root"} },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld()
			w.stdin = "feat/done\n"
			args := setup(w)
			got := w.run(args...)
			if got.code != ExitCannotRun || got.out != "" || !strings.Contains(got.err, "ERROR") {
				t.Fatalf("%+v", got)
			}
		})
	}
}

func TestHelpIsPrintedOnStdout(t *testing.T) {
	got := newWorld().run("--help")
	if got.code != ExitOK || !strings.Contains(got.out, "usage: branch-delete-list") {
		t.Fatalf("%+v", got)
	}
}

func TestAnUnresolvableRootIsExitTwo(t *testing.T) {
	var errOut bytes.Buffer
	env := Env{
		Stdin: strings.NewReader("x\n"), Stdout: &bytes.Buffer{}, Stderr: &errOut, Cwd: "/work",
		Eval:     func(string) (string, error) { return "", errors.New("no such directory") },
		Git:      &fakeGit{},
		ReadFile: func(string) ([]byte, error) { return nil, errors.New("unused") },
	}
	if code := Main([]string{"--scripts", "/repo/scripts"}, env); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

// A delete list that could not be written is not a pass.
func TestAnUnwritableStdoutIsExitTwo(t *testing.T) {
	w := newWorld()
	w.stdin = "feat/done\n"
	w.stdout = failingWriter{}
	if got := w.run(); got.code != ExitCannotRun || !strings.Contains(got.err, "cannot write to stdout") {
		t.Fatalf("%+v", got)
	}
}

func TestParseRemote(t *testing.T) {
	r, err := ParseRemote(lsRemote)
	if err != nil {
		t.Fatal(err)
	}
	if r.Default != "F44" || r.Heads["fix/old"] != tipB || r.Archives["feat/done"] != tipA || len(r.Heads) != 5 || len(r.Archives) != 3 {
		t.Fatalf("%+v", r)
	}
}
