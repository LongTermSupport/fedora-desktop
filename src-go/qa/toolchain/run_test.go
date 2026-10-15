package toolchain

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestT1HelpUnknownPositionalAndMissingValue(t *testing.T) {
	w := goodWorld()
	if code := w.run("--help"); code != ExitOK || !strings.Contains(w.out.String(), "usage: qa-toolchain") || w.err.Len() != 0 {
		t.Fatalf("--help: %d %q %q", code, w.out.String(), w.err.String())
	}
	for _, arg := range []string{"--bogus", "some/path"} {
		w = goodWorld()
		code := w.run(arg)
		if code != ExitCannotRun {
			t.Fatalf("%s: exit %d", arg, code)
		}
		mustContain(t, "stderr", w.err.String(), "ERROR: unknown argument '"+arg+"'", "usage:")
		if w.out.Len() != 0 {
			t.Errorf("%s: stdout %q", arg, w.out.String())
		}
	}
	w = goodWorld()
	if code := w.run("--root"); code != ExitCannotRun || !strings.HasPrefix(w.err.String(), "ERROR: ") {
		t.Fatalf("--root without value: %d %q", code, w.err.String())
	}
	w = goodWorld()
	if code := w.run("--scripts"); code != ExitCannotRun {
		t.Fatalf("--scripts without value: %d", code)
	}
	w = goodWorld()
	if code := w.run("--scripts", "/repo/scripts"); code != ExitOK {
		t.Fatalf("--scripts: %d %q", code, w.err.String())
	}
}

func TestT1RootThroughASymlinkIsResolvedAndNeverLeaks(t *testing.T) {
	realRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "view")
	if err := symlink(realRoot, link); err != nil {
		t.Fatal(err)
	}
	w := goodWorld()
	w.replies["ruff --version"] = reply{stdout: "ruff 9.9.9\n"}
	env := w.env()
	env.Resolve = filepath.EvalSymlinks
	if code := Main([]string{"--root", link}, env); code != ExitFailed {
		t.Fatalf("exit %d\n%s", code, w.err.String())
	}
	for _, c := range w.ran {
		if c.Dir != realRoot {
			t.Errorf("tool ran in %q, want the resolved root %q", c.Dir, realRoot)
		}
	}
	if all := w.out.String() + w.err.String(); strings.Contains(all, realRoot) || strings.Contains(all, link) {
		t.Fatalf("a checkout path leaked (T16):\n%s", all)
	}
	w = goodWorld()
	env = w.env()
	env.Resolve = filepath.EvalSymlinks
	if code := Main([]string{"--root", filepath.Join(t.TempDir(), "missing")}, env); code != ExitCannotRun {
		t.Fatalf("missing root: %d", code)
	}
}

func TestT2WriteFailureIsExitTwo(t *testing.T) {
	w := goodWorld()
	env := w.env()
	env.Stdout = failWriter{}
	if code := Main(nil, env); code != ExitCannotRun || !strings.Contains(w.err.String(), "ERROR: cannot write to stdout: ") {
		t.Fatalf("exit %d, stderr %q", code, w.err.String())
	}
}

func TestT15AMatchingMachinePassesWithOneStdoutLine(t *testing.T) {
	w := goodWorld()
	if code := w.run(); code != ExitOK {
		t.Fatalf("exit %d\n%s", code, w.err.String())
	}
	if got, want := w.out.String(), "TOOLCHAIN-OK 6 tool(s) match .qa-versions\n"; got != want {
		t.Fatalf("stdout %q, want %q", got, want)
	}
	if w.err.Len() != 0 {
		t.Fatalf("stderr on a passing run: %q", w.err.String())
	}
}

func TestT8EveryToolIsAssertedInOrderAndAllMismatchesAreReported(t *testing.T) {
	w := goodWorld()
	w.replies["ruff --version"] = reply{stdout: "ruff 0.1.0\n"}
	w.replies["semgrep --version"] = reply{stdout: "0.2.0\n"}
	delete(w.tools, "shellcheck")
	w.replies["golangci-lint --version"] = reply{stdout: "golangci-lint has version 0.3.0 built\n"}
	w.replies["go-qa --version"] = reply{stdout: "v0.0.0-20260101000000-aaaaaaaaaaaa\n"}
	w.replies["go env GOVERSION"] = reply{stdout: "go1.20.1\n"}
	if code := w.run(); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
	got := w.err.String()
	last := -1
	for _, want := range []string{"ruff version mismatch", "semgrep version mismatch", "shellcheck is not installed", "golangci-lint version mismatch", "go-qa version mismatch", "go is older"} {
		at := strings.Index(got, want)
		if at < 0 || at < last {
			t.Fatalf("%q missing or out of order in:\n%s", want, got)
		}
		last = at
	}
	if w.out.Len() != 0 {
		t.Errorf("stdout on a failing run: %q", w.out.String())
	}
}

func TestT9AnAbsentToolIsAMismatchNeverASkip(t *testing.T) {
	w := goodWorld()
	delete(w.tools, "ruff")
	delete(w.tools, "go")
	if code := w.run(); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
	mustContain(t, "stderr", w.err.String(),
		"✗ toolchain: ruff is not installed (expected 0.16.8)",
		"✗ toolchain: go is not installed (need >= 1.26.9, src-go/qa/go.mod)")
	if strings.Contains(w.out.String(), "TOOLCHAIN-OK") {
		t.Fatalf("passed with tools absent: %q", w.out.String())
	}
}

func TestT10TheVersionIsReadPerToolInItsOwnFormat(t *testing.T) {
	cases := map[string]reply{
		"ruff --version":          {stdout: "ruff 0.16.8\n"},
		"semgrep --version":       {stdout: "1.177.0\n"},
		"shellcheck --version":    {stdout: "ShellCheck - shell script analysis tool\nversion: 0.11.0\n"},
		"golangci-lint --version": {stdout: "golangci-lint has version 2.14.0 built with go1.26.2\n"},
		"go-qa --version":         {stdout: "v0.0.0-20261014082313-803b122ce526\n"},
	}
	for argv, r := range cases {
		w := goodWorld()
		w.replies[argv] = r
		if code := w.run(); code != ExitOK {
			t.Errorf("%s: exit %d\n%s", argv, code, w.err.String())
		}
	}
}

func TestT10AnUnreadableVersionIsAMismatchWithItsEvidence(t *testing.T) {
	cases := map[string]reply{
		"non-zero exit":   {stdout: "ruff 0.16.8\n", stderr: "boom: broken install\n", code: 3},
		"no version text": {stdout: "\n", stderr: "warning only\n"},
		"timeout":         {timedOut: true},
		"cannot start":    {startErr: errFake},
		"shellcheck line": {stdout: "ShellCheck\nlicense: GPL\n"},
	}
	for name, r := range cases {
		w := goodWorld()
		w.replies["ruff --version"] = r
		w.replies["shellcheck --version"] = r
		if code := w.run(); code != ExitFailed {
			t.Errorf("%s: exit %d", name, code)
			continue
		}
		mustContain(t, name+" stderr", w.err.String(),
			"✗ toolchain: ruff is installed but its version could not be read (expected 0.16.8)",
			"its stderr was:")
		if r.stderr != "" {
			mustContain(t, name+" stderr", w.err.String(), strings.TrimSpace(r.stderr))
		}
	}
	w := goodWorld()
	w.replies["ruff --version"] = reply{startErr: errFake}
	w.run()
	mustContain(t, "cause when the tool has no stderr", w.err.String(), errFake.Error())
}

func TestT10EachRunIsBoundedAndRunsInTheRoot(t *testing.T) {
	w := goodWorld()
	env := w.env()
	env.Timeout = 0
	env.Runner = &deadlineSpy{inner: w}
	if code := Main(nil, env); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	spy, ok := env.Runner.(*deadlineSpy)
	if !ok || spy.calls != 6 || spy.unbounded != 0 {
		t.Fatalf("runs %d, unbounded %d", spy.calls, spy.unbounded)
	}
	for _, c := range w.ran {
		if c.Dir != "/repo" {
			t.Errorf("%v ran in %q, want the root", c.Argv, c.Dir)
		}
	}
}

func TestT11TheVersionsMustBeEqualStrings(t *testing.T) {
	for _, found := range []string{"0.16.80", "v0.16.8", "0.16", "0.16.8.1"} {
		w := goodWorld()
		w.replies["ruff --version"] = reply{stdout: "ruff " + found + "\n"}
		if code := w.run(); code != ExitFailed {
			t.Errorf("%s accepted as 0.16.8", found)
			continue
		}
		mustContain(t, "stderr", w.err.String(),
			"✗ toolchain: ruff version mismatch",
			"    expected : 0.16.8  (.qa-versions)",
			"    found    : "+found+"  (/bin/ruff)")
	}
}

func TestT12GoFloor(t *testing.T) {
	pass := map[string]string{"go1.26.9\n": "", "go1.27.0\n": "", "go1.26.9 X:nocoverageredesign\n": ""}
	for out := range pass {
		w := goodWorld()
		w.replies["go env GOVERSION"] = reply{stdout: out}
		if code := w.run(); code != ExitOK {
			t.Errorf("%q: exit %d\n%s", out, code, w.err.String())
		}
	}
	w := goodWorld()
	w.replies["go env GOVERSION"] = reply{stdout: "go1.25.9\n"}
	if code := w.run(); code != ExitFailed {
		t.Fatalf("1.25.9: exit %d", code)
	}
	mustContain(t, "stderr", w.err.String(),
		"✗ toolchain: go is older than src-go/qa/go.mod requires",
		"    need >= : 1.26.9  (src-go/qa/go.mod)",
		"    found   : 1.25.9  (/bin/go)")
	for _, r := range []reply{{stdout: "devel garbage\n"}, {stdout: ""}, {stdout: "go1.26.2\n", code: 1, stderr: "go: broken\n"}, {timedOut: true}, {startErr: errFake}} {
		w = goodWorld()
		w.replies["go env GOVERSION"] = r
		if code := w.run(); code != ExitFailed {
			t.Errorf("%+v: exit %d", r, code)
			continue
		}
		mustContain(t, "stderr", w.err.String(), "✗ toolchain: go is installed but its version could not be read (need >= 1.26.9)")
	}
}

func TestT12ThreeComponentFloorIsTakenFromGoMod(t *testing.T) {
	w := goodWorld()
	w.files["src-go/go.mod"] = "module m\n\ngo 1.26.4\n"
	w.files["src-go/qa/go.mod"] = goodQAMod[:len("module example.test/m/qa\n\n")] + "go 1.26.4" + goodQAMod[len("module example.test/m/qa\n\ngo 1.26.9"):]
	w.replies["go env GOVERSION"] = reply{stdout: "go1.26.2\n"}
	if code := w.run(); code != ExitFailed {
		t.Fatalf("1.26.2 satisfied 1.26.4: exit %d", code)
	}
	mustContain(t, "stderr", w.err.String(), "need >= : 1.26.4")
}

func TestT14AMismatchEndsWithTheRemedyAndExitOne(t *testing.T) {
	w := goodWorld()
	w.replies["ruff --version"] = reply{stdout: "ruff 0.1.0\n"}
	if code := w.run(); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
	mustContain(t, "stderr", w.err.String(), "\n\n", "different verdict", "play-python.yml", "ccy --rebuild")
	if w.out.Len() != 0 {
		t.Errorf("stdout %q", w.out.String())
	}
}
