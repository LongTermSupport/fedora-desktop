package toolchain

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

var errFake = errors.New("fake failure to start")

// deadlineSpy records whether every run carried a deadline.
type deadlineSpy struct {
	inner            proc.Runner
	calls, unbounded int
}

func (d *deadlineSpy) Run(ctx context.Context, c proc.Cmd) (proc.Result, error) {
	d.calls++
	if _, ok := ctx.Deadline(); !ok {
		d.unbounded++
	}
	return d.inner.Run(ctx, c)
}

func cannotRun(t *testing.T, w *world, want ...string) {
	t.Helper()
	if code := w.run(); code != ExitCannotRun {
		t.Fatalf("exit %d, want 2\nstderr: %s", code, w.err.String())
	}
	got := w.err.String()
	if !strings.HasPrefix(got, "ERROR: ") {
		t.Errorf("stderr does not start ERROR: %q", got)
	}
	mustContain(t, "stderr", got, want...)
	if strings.Contains(w.out.String(), "TOOLCHAIN-OK") {
		t.Errorf("a run that could not judge passed: %q", w.out.String())
	}
	if len(w.ran) != 0 {
		t.Errorf("tools were run before the inputs were valid: %v", w.ran)
	}
}

func TestT3AMissingPinFileIsExitTwo(t *testing.T) {
	w := goodWorld()
	delete(w.files, ".qa-versions")
	cannotRun(t, w, ".qa-versions", "single source of truth")
}

func TestT3ThePinFileIsParsedNeverExecuted(t *testing.T) {
	w := goodWorld()
	w.files[".qa-versions"] = goodPins + "touch /tmp/should-not-run\n"
	cannotRun(t, w, ".qa-versions:7")
}

func TestT4OnlyPlainAssignmentsAreAccepted(t *testing.T) {
	bad := map[string]string{
		"quoted":        "RUFF=\"0.16.8\"\n",
		"single quoted": "RUFF='0.16.8'\n",
		"expansion":     "RUFF=$OTHER\n",
		"command subst": "RUFF=$(echo 1)\n",
		"export":        "export RUFF=0.16.8\n",
		"append":        "RUFF+=1\n",
		"array":         "RUFF=(1 2)\n",
		"command":       "echo hi\n",
		"env prefix":    "RUFF=1 echo hi\n",
		"two on a line": "RUFF=1 SEMGREP=2\n",
		"redirect":      "RUFF=1 > /tmp/x\n",
		"syntax error":  "RUFF=(\n",
		"background":    "RUFF=1 &\n",
		"compound":      "if true; then RUFF=1; fi\n",
	}
	for name, line := range bad {
		w := goodWorld()
		w.files[".qa-versions"] = "SEMGREP=1.177.0\nSHELLCHECK=0.11.0\nGOLANGCI_LINT=2.14.0\nGO_QA_CI=803b122ce526\n" + line
		cannotRun(t, w, ".qa-versions:5")
		_ = name
	}
}

func TestT5TheKeysAreExactlyTheFivePins(t *testing.T) {
	cases := map[string]struct{ file, want string }{
		"missing key": {"RUFF=1\nSEMGREP=1\nSHELLCHECK=1\nGOLANGCI_LINT=1\n", "GO_QA_CI"},
		"empty value": {"RUFF=\nSEMGREP=1\nSHELLCHECK=1\nGOLANGCI_LINT=1\nGO_QA_CI=1\n", "RUFF"},
		"duplicate":   {goodPins + "RUFF=0.1\n", "RUFF"},
		"unknown key": {goodPins + "BLACK=1.0\n", "BLACK"},
	}
	for name, c := range cases {
		w := goodWorld()
		w.files[".qa-versions"] = c.file
		t.Run(name, func(t *testing.T) { cannotRun(t, w, c.want) })
	}
}

func TestT7AnEmptyOrCommentOnlyPinFileNeverPasses(t *testing.T) {
	for _, text := range []string{"", "# nothing here\n\n"} {
		w := goodWorld()
		w.files[".qa-versions"] = text
		cannotRun(t, w, "RUFF", "SEMGREP", "SHELLCHECK", "GOLANGCI_LINT", "GO_QA_CI")
	}
}

func TestT6TheGoFloorComesFromGoMod(t *testing.T) {
	cases := map[string]struct {
		set  bool
		text string
	}{
		"missing":      {false, ""},
		"no directive": {true, "module m\n"},
		"unparseable":  {true, "module m\ngo\n"},
		"bad version":  {true, "module m\ngo one.two\n"},
	}
	for name, c := range cases {
		for _, file := range []string{"src-go/go.mod", "src-go/qa/go.mod"} {
			w := goodWorld()
			delete(w.files, file)
			if c.set {
				w.files[file] = c.text
			}
			t.Run(name+" "+file, func(t *testing.T) { cannotRun(t, w, file) })
		}
	}
}

// T6: the floor is validated where it is read, so a floor the version rules cannot compare is a
// bad input (exit 2) and never machine drift. modfile and go/version accept the same strings
// today, so this pins the accepted forms; the branch itself is defence.
func TestT6TheFloorIsValidatedWhereItIsRead(t *testing.T) {
	for _, floor := range []string{"1.26", "1.26.4", "1.26rc1"} {
		got, err := readGoFloor(fstest.MapFS{
			"src-go/go.mod":    {Data: []byte("module m\n\ngo " + floor + "\n")},
			"src-go/qa/go.mod": {Data: []byte("module m\n\ngo 1.1\n")},
		})
		if err != nil || got.version != floor || got.file != "src-go/go.mod" {
			t.Errorf("%s: got %q, %v", floor, got, err)
		}
	}
}

// go is floored on the higher of the two modules' go directives, and the failure names the
// file that set it, so a host that can build src-go but not the QA module is a drifted machine.
func TestT6TheFloorIsTheHigherOfTheTwoModules(t *testing.T) {
	cases := map[string]struct{ root, qa, wantFile, wantVersion string }{
		"the qa module is higher":   {"1.26", "1.26.9", "src-go/qa/go.mod", "1.26.9"},
		"the root module is higher": {"1.27", "1.26.9", "src-go/go.mod", "1.27"},
		"equal: the root module":    {"1.26.9", "1.26.9", "src-go/go.mod", "1.26.9"},
	}
	for name, c := range cases {
		got, err := readGoFloor(fstest.MapFS{
			"src-go/go.mod":    {Data: []byte("module m\n\ngo " + c.root + "\n")},
			"src-go/qa/go.mod": {Data: []byte("module m\n\ngo " + c.qa + "\n")},
		})
		if err != nil || got.version != c.wantVersion || got.file != c.wantFile {
			t.Errorf("%s: got %+v, %v", name, got, err)
		}
	}
}

func TestT6AGoOlderThanTheQAModuleNeedsFailsTheGate(t *testing.T) {
	w := goodWorld()
	w.replies["go env GOVERSION"] = reply{stdout: "go1.26.2\n"}
	if code := w.run(); code != ExitFailed {
		t.Fatalf("exit %d\n%s", code, w.err.String())
	}
	mustContain(t, "stderr", w.err.String(), "go is older than src-go/qa/go.mod requires")
}

// The go-qa binary (GO_QA_CI) and the go-qa-ci the project rules are built against
// (src-go/qa/go.mod) are one commit; two commits is a drifted repository.
func TestT5GoQACIAndTheQAModuleRequireAgree(t *testing.T) {
	w := goodWorld()
	w.files["src-go/qa/go.mod"] = "module m\n\ngo 1.26.9\n\nrequire github.com/LongTermSupport/go-qa-ci v0.0.0-20260101000000-aaaaaaaaaaaa\n"
	if code := w.run(); code != ExitFailed {
		t.Fatalf("exit %d, want %d\n%s", code, ExitFailed, w.err.String())
	}
	mustContain(t, "stderr", w.err.String(), "GO_QA_CI", "aaaaaaaaaaaa", "803b122ce526", "src-go/qa/go.mod")
}

func TestT5AQAModuleWithoutTheGoQACIRequireCannotBeJudged(t *testing.T) {
	w := goodWorld()
	w.files["src-go/qa/go.mod"] = "module m\n\ngo 1.26.9\n"
	cannotRun(t, w, "src-go/qa/go.mod", "go-qa-ci")
}

func TestT6AGoLineInsideABlockIsNotTheDirective(t *testing.T) {
	w := goodWorld()
	w.files["src-go/go.mod"] = "module m\n\ngo 1.26\n\nrequire (\n\tgo.example/x v1.0.0\n)\n"
	if code := w.run(); code != ExitOK {
		t.Fatalf("exit %d\n%s", code, w.err.String())
	}
}
