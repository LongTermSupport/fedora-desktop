package convert

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// world is one run's surroundings: a temporary directory for the files, a fake runner whose
// pandoc writes what it was asked to, and the two streams.
type world struct {
	t       *testing.T
	dir     string
	fake    *execx.Fake
	stdout  bytes.Buffer
	stderr  bytes.Buffer
	missing map[string]bool
	table   []InputType
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{t: t, dir: t.TempDir(), fake: &execx.Fake{}, missing: map[string]bool{}, table: Table}
	w.fake.OnFunc([]string{"pandoc"}, writeOutput("DOCX", 0, ""))
	return w
}

// writeOutput answers an engine run by writing body to its --output path, then exiting code.
func writeOutput(body string, code int, stderr string) func(execx.Cmd) (execx.Result, error) {
	return func(c execx.Cmd) (execx.Result, error) {
		for _, a := range c.Args {
			if path, ok := strings.CutPrefix(a, "--output="); ok {
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					return execx.Result{}, err
				}
			}
		}
		return execx.Result{ExitCode: code, Stderr: []byte(stderr)}, nil
	}
}

func (w *world) file(name, body string) string {
	w.t.Helper()
	path := filepath.Join(w.dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		w.t.Fatal(err)
	}
	return path
}

func (w *world) env() Env {
	return Env{
		Runner: w.fake, Stdout: &w.stdout, Stderr: &w.stderr, Pid: 7, Table: w.table,
		LookPath: func(name string) (string, error) {
			if w.missing[name] {
				return "", errors.New("not found")
			}
			return "/usr/bin/" + name, nil
		},
	}
}

func (w *world) run(args ...string) term.ExitCode {
	return App{Env: w.env()}.Run(context.Background(), args)
}

func (w *world) expect(code, want term.ExitCode, stderrHas string) {
	w.t.Helper()
	if code != want || !strings.Contains(w.stderr.String(), stderrHas) {
		w.t.Fatalf("exit %d (want %d), stderr %q (want %q)", code, want, w.stderr.String(), stderrHas)
	}
}

func (w *world) exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestHelpPrintsUsageOnStdout(t *testing.T) { // E1
	w := newWorld(t)
	w.expect(w.run("--help"), term.ExitOK, "")
	if !strings.Contains(w.stdout.String(), "convert FILE --to FORMAT") {
		t.Fatalf("stdout %q", w.stdout.String())
	}
}

func TestUsageErrorIsExit64(t *testing.T) { // E2
	w := newWorld(t)
	w.expect(w.run("--bogus"), term.ExitUsage, "convert: unknown option '--bogus'")
}

func TestMissingAndNonRegularFilesFail(t *testing.T) { // E3
	w := newWorld(t)
	w.expect(w.run(filepath.Join(w.dir, "absent.md")), term.ExitFailure, "absent.md' does not exist.")
	w = newWorld(t)
	if err := os.Mkdir(filepath.Join(w.dir, "d.md"), 0o700); err != nil {
		t.Fatal(err)
	}
	w.expect(w.run(filepath.Join(w.dir, "d.md")), term.ExitFailure, "d.md' is not a regular file.")
}

func TestUnknownInputTypeNamesTheKnownOnes(t *testing.T) { // E4
	w := newWorld(t)
	w.expect(w.run(w.file("a.txt", "x")), term.ExitFailure, "known input types are markdown (.md, .markdown).")
}

func TestNoToListsTheOutputsAndRunsNothing(t *testing.T) { // E5
	w := newWorld(t)
	w.expect(w.run(w.file("a.md", "# A")), term.ExitOK, "")
	if w.stdout.String() != "docx  Word document (pandoc)\n" || len(w.fake.Calls()) != 0 {
		t.Fatalf("stdout %q, calls %v", w.stdout.String(), w.fake.Calls())
	}
}

func TestUnsupportedFormatIsUsageAndNamesTheSupported(t *testing.T) { // E7
	w := newWorld(t)
	in := w.file("a.md", "# A")
	w.expect(w.run(in, "--to", "docx,pdf"), term.ExitUsage, "(markdown) cannot become 'pdf'; it can become: docx.")
	if len(w.fake.Calls()) != 0 {
		t.Fatalf("ran %v", w.fake.Calls())
	}
}

func TestConvertWritesBesideTheInputThroughATemporary(t *testing.T) { // E8, E11, E13
	w := newWorld(t)
	in := w.file("NOTES.MD", "# A")
	w.expect(w.run(in, "--to", "docx"), term.ExitOK, "")
	out := filepath.Join(w.dir, "NOTES.docx")
	if w.stdout.String() != out+"\n" {
		t.Fatalf("stdout %q", w.stdout.String())
	}
	if !w.exists(out) || w.exists(out+".tmp.7") {
		t.Fatalf("output %v, temporary %v", w.exists(out), w.exists(out+".tmp.7"))
	}
	calls := w.fake.Calls()
	if len(calls) != 1 || calls[0].Line() != "pandoc --from=markdown --to=docx --output="+out+".tmp.7 "+in {
		t.Fatalf("calls %v", calls)
	}
}

func TestOutputIsPrintedAsGivenRelativeToTheInvocation(t *testing.T) { // E13
	w := newWorld(t)
	w.file("n.md", "# A")
	t.Chdir(w.dir)
	w.expect(w.run("n.md", "--to=docx"), term.ExitOK, "")
	if w.stdout.String() != "n.docx\n" {
		t.Fatalf("stdout %q", w.stdout.String())
	}
	// The engine is given the input as an absolute path, so no name reads as an option.
	if args := w.fake.Calls()[0].Args; !filepath.IsAbs(args[len(args)-1]) {
		t.Fatalf("input %q", args[len(args)-1])
	}
}

func TestExistingOutputNeedsForce(t *testing.T) { // E9
	w := newWorld(t)
	in := w.file("a.md", "# A")
	out := w.file("a.docx", "old")
	w.expect(w.run(in, "--to", "docx"), term.ExitFailure, "a.docx' already exists; use --force to replace it.")
	if len(w.fake.Calls()) != 0 {
		t.Fatalf("ran %v", w.fake.Calls())
	}
	w = newWorld(t)
	w.dir = filepath.Dir(in)
	w.expect(w.run(in, "--to", "docx", "--force"), term.ExitOK, "")
	// The fake pandoc writes "DOCX" (4 bytes) over the 3-byte "old".
	if info, err := os.Stat(out); err != nil || info.Size() != int64(len("DOCX")) {
		t.Fatalf("output %v %v", info, err)
	}
}

func TestMissingEngineIsRefusedBeforeAnythingRuns(t *testing.T) { // E10
	w := newWorld(t)
	w.missing["pandoc"] = true
	w.expect(w.run(w.file("a.md", "# A"), "--to", "docx"), term.ExitFailure,
		"convert: pandoc is not installed; the play that installs it is "+cliToolsPlay)
	if len(w.fake.Calls()) != 0 {
		t.Fatalf("ran %v", w.fake.Calls())
	}
}

func TestEngineVanishingAfterTheCheckGivesTheSameMessage(t *testing.T) { // E10
	w := newWorld(t)
	w.fake.OnError([]string{"pandoc"}, &execx.NotInstalledError{Name: "pandoc"})
	w.expect(w.run(w.file("a.md", "# A"), "--to", "docx"), term.ExitFailure, "pandoc is not installed; the play")
}

func TestEngineFailureRemovesTheTemporary(t *testing.T) { // E12
	w := newWorld(t)
	w.fake.OnFunc([]string{"pandoc"}, writeOutput("half", 1, "bad table\n"))
	in := w.file("a.md", "# A")
	w.expect(w.run(in, "--to", "docx"), term.ExitFailure, "convert: pandoc could not convert '"+in+"' to docx: bad table")
	out := filepath.Join(w.dir, "a.docx")
	if w.exists(out) || w.exists(out+".tmp.7") || w.stdout.Len() != 0 {
		t.Fatalf("left output %v, temporary %v, stdout %q", w.exists(out), w.exists(out+".tmp.7"), w.stdout.String())
	}
}

func TestEngineFailureWithNoStderrSaysHowItExited(t *testing.T) { // E12
	w := newWorld(t)
	w.fake.OnFunc([]string{"pandoc"}, writeOutput("", 3, ""))
	w.expect(w.run(w.file("a.md", "# A"), "--to", "docx"), term.ExitFailure, "to docx: pandoc exited 3")
}

// failing is an engine that always fails to start with err.
type failing struct{ err error }

func (failing) Program() string { return "lowriter" }
func (failing) Play() string    { return "playbooks/x.yml" }
func (f failing) Convert(context.Context, execx.Runner, string, string) error {
	return f.err
}

func TestEarlierOutputsStayWhenALaterFormatFails(t *testing.T) { // E12, E13
	w := newWorld(t)
	w.table = []InputType{{Name: "markdown", Exts: []string{".md"}, Outputs: []Output{
		{Format: "docx", Desc: "Word document", Engine: Pandoc{From: "markdown", To: "docx"}},
		{Format: "odt", Desc: "OpenDocument text", Engine: failing{err: errors.New("broken")}},
	}}}
	in := w.file("a.md", "# A")
	w.expect(w.run(in, "--to", "docx,odt"), term.ExitFailure, "convert: lowriter could not convert '"+in+"' to odt: broken")
	if w.stdout.String() != filepath.Join(w.dir, "a.docx")+"\n" {
		t.Fatalf("stdout %q", w.stdout.String())
	}
}

func TestCancelledRunRemovesTheTemporaryAndExits130(t *testing.T) { // E14
	w := newWorld(t)
	ctx, cancel := context.WithCancel(context.Background())
	w.fake.OnFunc([]string{"pandoc"}, func(c execx.Cmd) (execx.Result, error) {
		if _, err := writeOutput("half", 0, "")(c); err != nil {
			return execx.Result{}, err
		}
		cancel()
		return execx.Result{}, context.Canceled
	})
	in := w.file("a.md", "# A")
	code := App{Env: w.env()}.Run(ctx, []string{in, "--to", "docx"})
	w.expect(code, term.ExitCancelled, "convert: cancelled.")
	if w.exists(filepath.Join(w.dir, "a.docx.tmp.7")) || w.exists(filepath.Join(w.dir, "a.docx")) {
		t.Fatal("a file was left behind")
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestUnwritableStdoutFailsTheRun(t *testing.T) { // E15
	w := newWorld(t)
	env := w.env()
	env.Stdout = brokenWriter{}
	code := App{Env: env}.Run(context.Background(), []string{w.file("a.md", "# A")})
	w.expect(code, term.ExitFailure, "could not write the output: closed")
}

func TestNewEnvIsTheRealProcess(t *testing.T) {
	env := NewEnv()
	if env.Stdout != os.Stdout || env.Stderr != os.Stderr || env.Pid != os.Getpid() || len(env.Table) == 0 {
		t.Fatalf("env %+v", env)
	}
	if _, ok := env.Runner.(execx.OS); !ok {
		t.Fatalf("runner %T", env.Runner)
	}
}
