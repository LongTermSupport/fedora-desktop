package toolchain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	goqaci "github.com/LongTermSupport/go-qa-ci"

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
	cannotRun(t, w, ".qa-versions:6")
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
		w.files[".qa-versions"] = "SEMGREP=1.177.0\nSHELLCHECK=0.11.0\nGOLANGCI_LINT=2.14.0\n" + line
		cannotRun(t, w, ".qa-versions:4")
		_ = name
	}
}

func TestT5TheKeysAreExactlyTheFourPins(t *testing.T) {
	cases := map[string]struct{ file, want string }{
		"missing key": {"RUFF=1\nSEMGREP=1\nSHELLCHECK=1\n", "GOLANGCI_LINT"},
		"empty value": {"RUFF=\nSEMGREP=1\nSHELLCHECK=1\nGOLANGCI_LINT=1\n", "RUFF"},
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
		cannotRun(t, w, "RUFF", "SEMGREP", "SHELLCHECK", "GOLANGCI_LINT")
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
	for _, file := range []string{"src-go/go.mod", "src-go/qa/go.mod"} {
		for name, c := range cases {
			w := goodWorld()
			delete(w.files, file)
			if c.set {
				w.files[file] = c.text
			}
			t.Run(file+" "+name, func(t *testing.T) { cannotRun(t, w, file) })
		}
	}
}

// T6: both modules are built by the same go, so the floor is the higher of their go directives,
// named with the file that sets it.
func TestT6TheGoFloorIsTheHigherOfTheTwoModules(t *testing.T) {
	for file, other := range map[string]string{"src-go/qa/go.mod": "src-go/go.mod", "src-go/go.mod": "src-go/qa/go.mod"} {
		w := goodWorld()
		w.files[file] = "module m\n\ngo 1.26.9\n"
		w.files[other] = "module m\n\ngo 1.26.4\n"
		w.replies["go env GOVERSION"] = reply{stdout: "go1.26.5\n"}
		if code := w.run(); code != ExitFailed {
			t.Fatalf("%s: 1.26.5 satisfied 1.26.9: exit %d", file, code)
		}
		mustContain(t, "stderr", w.err.String(), "go is older than "+file+" requires", "need >= : 1.26.9  ("+file+")")
	}
}

// The golangci-lint pinned here (for the hooks daemon's lint of every Go edit) and the one
// go-qa-ci installs for the Go gate are one version: a bump of either alone fails here.
func TestTheGolangciLintPinIsTheOneGoQaCiInstalls(t *testing.T) {
	pins, err := readPins(os.DirFS(filepath.Join("..", "..", "..")))
	if err != nil {
		t.Fatal(err)
	}
	section, lock := "", ""
	for line := range strings.SplitSeq(goqaci.ToolsLock, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			section = line
			continue
		}
		if value, ok := strings.CutPrefix(line, `golangci-lint = "`); ok && section == "[release]" {
			lock = strings.TrimSuffix(value, `"`)
		}
	}
	if lock == "" {
		t.Fatal("go-qa-ci's tools.lock names no [release] golangci-lint")
	}
	if pins["GOLANGCI_LINT"] != lock {
		t.Fatalf(".qa-versions pins golangci-lint %s, go-qa-ci installs %s: bump them together", pins["GOLANGCI_LINT"], lock)
	}
}

// T6: the floor is validated where it is read, so a floor the version rules cannot compare is a
// bad input (exit 2) and never machine drift. modfile and go/version accept the same strings
// today, so this pins the accepted forms; the branch itself is defence.
func TestT6TheFloorIsValidatedWhereItIsRead(t *testing.T) {
	for _, floor := range []string{"1.26", "1.26.4", "1.26rc1"} {
		got, err := readGoDirective(fstest.MapFS{goModFiles[0]: {Data: []byte("module m\n\ngo " + floor + "\n")}}, goModFiles[0])
		if err != nil || got != floor {
			t.Errorf("%s: got %q, %v", floor, got, err)
		}
	}
}

func TestT6AGoLineInsideABlockIsNotTheDirective(t *testing.T) {
	w := goodWorld()
	w.files["src-go/go.mod"] = "module m\n\ngo 1.26\n\nrequire (\n\tgo.example/x v1.0.0\n)\n"
	if code := w.run(); code != ExitOK {
		t.Fatalf("exit %d\n%s", code, w.err.String())
	}
}

// go-qa looks for .golangci.yml only in a module's own directory and writes its own baseline when
// there is none, so the qa module is linted against the project's rules only through this link.
func TestTheQaModuleLintsAgainstTheProjectGolangciConfig(t *testing.T) {
	srcGo := filepath.Join("..", "..")
	link := filepath.Join(srcGo, "qa", ".golangci.yml")
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("src-go/qa/.golangci.yml is gone, so go-qa lints the qa module with its own baseline: %v", err)
	}
	got, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(filepath.Join(srcGo, ".golangci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("src-go/qa/.golangci.yml resolves to %s, want %s", got, want)
	}
}
