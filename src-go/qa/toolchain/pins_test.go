package toolchain

import (
	"context"
	"errors"
	"strings"
	"testing"

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
	for name, c := range cases {
		w := goodWorld()
		delete(w.files, "src-go/go.mod")
		if c.set {
			w.files["src-go/go.mod"] = c.text
		}
		t.Run(name, func(t *testing.T) { cannotRun(t, w, "src-go/go.mod") })
	}
}

func TestT6AGoLineInsideABlockIsNotTheDirective(t *testing.T) {
	w := goodWorld()
	w.files["src-go/go.mod"] = "module m\n\ngo 1.26\n\nrequire (\n\tgo.example/x v1.0.0\n)\n"
	if code := w.run(); code != ExitOK {
		t.Fatalf("exit %d\n%s", code, w.err.String())
	}
}
