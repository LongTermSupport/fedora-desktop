package godeps

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const (
	goodGoMod     = "module ex.com/me\n\ngo 1.26.0\n\nrequire ex.com/a v1.0.0\n"
	goodAllowlist = "module\tex.com/a\tthe one dependency\n"
	vendoredFile  = "ex.com/a/a.go"
)

// vendorTree is what go mod vendor writes for goodGoMod, and what a repository in step holds.
var vendorTree = map[string]string{"modules.txt": "# ex.com/a v1.0.0\n## explicit\nex.com/a\n", vendoredFile: "package a\n"}

// fakeGo answers the go commands: a status and output per subcommand, and go mod vendor -o
// writes the tree it is given.
type fakeGo struct {
	calls   []proc.Cmd
	codes   map[string]int
	outputs map[string]string
	vendor  map[string]string
	failRun string
	t       *testing.T
}

// subcommand names a go command by its words after "go", up to the first flag value that varies.
func subcommand(argv []string) string {
	switch {
	case slices.Equal(argv[:3], []string{"go", "mod", "verify"}):
		return "verify"
	case slices.Equal(argv[:3], []string{"go", "mod", "tidy"}):
		return "tidy"
	case slices.Equal(argv[:3], []string{"go", "mod", "vendor"}):
		return "vendor"
	case slices.Equal(argv[:3], []string{"go", "tool", "govulncheck"}):
		return "govulncheck"
	}
	return strings.Join(argv, " ")
}

func (f *fakeGo) Run(_ context.Context, c proc.Cmd) (proc.Result, error) {
	f.calls = append(f.calls, c)
	name := subcommand(c.Argv)
	if name == f.failRun {
		return proc.Result{}, errors.New("cannot start go")
	}
	if name == "vendor" && f.codes[name] == 0 {
		writeTree(f.t, c.Argv[len(c.Argv)-1], f.vendor)
	}
	return proc.Result{Stdout: f.outputs[name], Code: f.codes[name]}, nil
}

// repo is a repository root whose src-go holds the files.
type repo struct {
	root     string
	goFake   *fakeGo
	out, err strings.Builder
	scratch  string
}

func newRepo(t *testing.T, files map[string]string) *repo {
	t.Helper()
	r := &repo{root: t.TempDir(), goFake: &fakeGo{codes: map[string]int{}, outputs: map[string]string{}, vendor: vendorTree, t: t}}
	writeTree(t, filepath.Join(r.root, "src-go"), files)
	return r
}

// inStep is a repository whose dependencies the gate passes.
func inStep(t *testing.T) *repo {
	t.Helper()
	files := map[string]string{"go.mod": goodGoMod, "go-deps.allowlist": goodAllowlist}
	for rel, text := range vendorTree {
		files["vendor/"+rel] = text
	}
	return newRepo(t, files)
}

func (r *repo) env(t *testing.T) Env {
	t.Helper()
	return Env{
		Stdout:  &r.out,
		Stderr:  &r.err,
		Cwd:     r.root,
		Resolve: func(p string) (string, error) { return p, nil },
		Runner:  r.goFake,
		TempDir: func() (string, error) {
			r.scratch = t.TempDir()
			return r.scratch, nil
		},
	}
}

func (r *repo) deps(t *testing.T, args ...string) ExitCode {
	t.Helper()
	return Main(args, r.env(t))
}

func (r *repo) vuln(t *testing.T, args ...string) ExitCode {
	t.Helper()
	return MainVuln(args, r.env(t))
}

func TestARepositoryInStepPasses(t *testing.T) {
	r := inStep(t)
	if code := r.deps(t); code != ExitOK {
		t.Fatalf("code %d, stderr:\n%s", code, r.err.String())
	}
	want := "GO-DEPS-OK 1 module(s) allowlisted (0 at a pseudo-version, 0 replaced); go.sum verified, tidy, vendor/ in sync (2 files)\n"
	if r.out.String() != want {
		t.Fatalf("stdout = %q", r.out.String())
	}
	if r.err.Len() != 0 {
		t.Fatalf("stderr = %q", r.err.String())
	}
}

// G10: the go commands run in src-go with an environment the caller's cannot change.
func TestTheGoCommandsRunInSrcGoUnderAFixedEnvironment(t *testing.T) {
	r := inStep(t)
	t.Setenv("GOFLAGS", "-mod=vendor")
	if code := r.deps(t); code != ExitOK {
		t.Fatalf("code %d: %s", code, r.err.String())
	}
	names := make([]string, 0, len(r.goFake.calls))
	for _, c := range r.goFake.calls {
		names = append(names, subcommand(c.Argv))
		if c.Dir != filepath.Join(r.root, "src-go") {
			t.Errorf("%v ran in %s", c.Argv, c.Dir)
		}
		for _, want := range []string{"GOFLAGS=", "GOWORK=off", "GOTOOLCHAIN=local"} {
			if !slices.Contains(c.Env, want) {
				t.Errorf("%v has no %s in %v", c.Argv, want, c.Env)
			}
		}
	}
	if !slices.Equal(names, []string{"verify", "tidy", "vendor"}) {
		t.Fatalf("ran %v", names)
	}
	if got := r.goFake.calls[1].Argv; !slices.Equal(got, []string{"go", "mod", "tidy", "-diff"}) {
		t.Fatalf("tidy argv = %v", got)
	}
}

// G10: the scratch tree go mod vendor writes is removed.
func TestTheScratchTreeIsRemoved(t *testing.T) {
	r := inStep(t)
	r.deps(t)
	if _, err := os.Stat(r.scratch); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scratch %s left behind: %v", r.scratch, err)
	}
}

// G7, G8, G9, G11: each failure is a finding, and all of them are reported.
func TestEveryFailureIsReported(t *testing.T) {
	r := inStep(t)
	r.goFake.codes["verify"] = 1
	r.goFake.outputs["verify"] = "ex.com/a v1.0.0: dir has been modified\n"
	r.goFake.codes["tidy"] = 1
	r.goFake.outputs["tidy"] = "--- current/go.mod\n+++ tidy/go.mod\n"
	r.goFake.vendor = map[string]string{"modules.txt": vendorTree["modules.txt"], vendoredFile: "package a // changed\n"}
	if code := r.deps(t); code != ExitFailed {
		t.Fatalf("code %d", code)
	}
	for _, want := range []string{"dir has been modified", "go mod verify", "+++ tidy/go.mod", "go mod tidy", "vendor/" + vendoredFile + " differs", "go mod vendor"} {
		if !strings.Contains(r.err.String(), want) {
			t.Errorf("stderr lacks %q:\n%s", want, r.err.String())
		}
	}
	if r.out.Len() != 0 {
		t.Fatalf("stdout = %q", r.out.String())
	}
}

func TestAFailedVendorRunIsAFinding(t *testing.T) {
	r := inStep(t)
	r.goFake.codes["vendor"] = 1
	r.goFake.outputs["vendor"] = "go: no network\n"
	if code := r.deps(t); code != ExitFailed {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(r.err.String(), "go: no network") {
		t.Fatalf("stderr = %s", r.err.String())
	}
}

func TestAnAllowlistFindingFailsTheGate(t *testing.T) {
	r := inStep(t)
	writeTree(t, filepath.Join(r.root, "src-go"), map[string]string{"go-deps.allowlist": "module\tex.com/other\twhy\n"})
	if code := r.deps(t); code != ExitFailed {
		t.Fatalf("code %d", code)
	}
	for _, want := range []string{"ex.com/a", "ex.com/other"} {
		if !strings.Contains(r.err.String(), want) {
			t.Errorf("stderr lacks %q:\n%s", want, r.err.String())
		}
	}
}

// G1, G2: what the gate reads must be there and well formed, or it cannot judge.
func TestWhatTheGateCannotReadIsExitTwo(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"no allowlist":    {"go.mod": goodGoMod},
		"bad allowlist":   {"go.mod": goodGoMod, "go-deps.allowlist": "module\tex.com/a\n"},
		"no go.mod":       {"go-deps.allowlist": goodAllowlist},
		"unparsed go.mod": {"go.mod": "module\nrequire (\n", "go-deps.allowlist": goodAllowlist},
	} {
		r := newRepo(t, files)
		if code := r.deps(t); code != ExitCannotRun {
			t.Errorf("%s: code %d", name, code)
		}
		if len(r.goFake.calls) != 0 {
			t.Errorf("%s: ran %v", name, r.goFake.calls)
		}
	}
}

func TestAGoCommandThatCannotStartIsExitTwo(t *testing.T) {
	for _, step := range []string{"verify", "tidy", "vendor"} {
		r := inStep(t)
		r.goFake.failRun = step
		if code := r.deps(t); code != ExitCannotRun {
			t.Errorf("%s: code %d", step, code)
		}
	}
}

func TestAScratchDirectoryThatCannotBeMadeIsExitTwo(t *testing.T) {
	r := inStep(t)
	env := r.env(t)
	env.TempDir = func() (string, error) { return "", errors.New("disk full") }
	if code := Main(nil, env); code != ExitCannotRun {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(r.err.String(), "disk full") {
		t.Fatalf("stderr = %s", r.err.String())
	}
}

// B1: the shared grammar.
func TestHelpAndBadArguments(t *testing.T) {
	r := inStep(t)
	if code := r.deps(t, "--help"); code != ExitOK || !strings.Contains(r.out.String(), "usage: qa-go-deps") {
		t.Fatalf("code %d, stdout %q", code, r.out.String())
	}
	r = inStep(t)
	if code := r.deps(t, "--bogus"); code != ExitCannotRun || !strings.Contains(r.err.String(), "usage: qa-go-deps") {
		t.Fatalf("code %d, stderr %q", code, r.err.String())
	}
	r = inStep(t)
	if code := r.vuln(t, "--help"); code != ExitOK || !strings.Contains(r.out.String(), "usage: qa-go-vuln") {
		t.Fatalf("code %d, stdout %q", code, r.out.String())
	}
	r = inStep(t)
	if code := r.vuln(t, "--root"); code != ExitCannotRun {
		t.Fatalf("code %d", code)
	}
}

func TestARootThatCannotBeResolvedIsExitTwo(t *testing.T) {
	r := inStep(t)
	env := r.env(t)
	env.Resolve = func(string) (string, error) { return "", errors.New("no such root") }
	if code := Main(nil, env); code != ExitCannotRun {
		t.Fatalf("deps code %d", code)
	}
	if code := MainVuln(nil, env); code != ExitCannotRun {
		t.Fatalf("vuln code %d", code)
	}
}

func TestTheRootComesFromTheScriptsDirectory(t *testing.T) {
	r := inStep(t)
	env := r.env(t)
	env.Cwd = t.TempDir()
	if code := Main([]string{"--scripts", filepath.Join(r.root, "scripts")}, env); code != ExitOK {
		t.Fatalf("code %d: %s", code, r.err.String())
	}
}

// V1, V3: govulncheck is the qa module's pinned tool, run over src-go.
func TestVulnRunsThePinnedGovulncheckOverSrcGo(t *testing.T) {
	r := inStep(t)
	if code := r.vuln(t); code != ExitOK {
		t.Fatalf("code %d: %s", code, r.err.String())
	}
	if len(r.goFake.calls) != 1 {
		t.Fatalf("calls = %v", r.goFake.calls)
	}
	c := r.goFake.calls[0]
	srcGo := filepath.Join(r.root, "src-go")
	if !slices.Equal(c.Argv, []string{"go", "tool", "govulncheck", "-C", srcGo, "./..."}) {
		t.Fatalf("argv = %v", c.Argv)
	}
	if c.Dir != filepath.Join(srcGo, "qa") {
		t.Fatalf("dir = %s", c.Dir)
	}
	for _, want := range []string{"GOFLAGS=", "GOWORK=off", "GOTOOLCHAIN=local"} {
		if !slices.Contains(c.Env, want) {
			t.Errorf("no %s in %v", want, c.Env)
		}
	}
	if r.out.String() != "GO-VULN-OK src-go: govulncheck found no vulnerability the code reaches\n" {
		t.Fatalf("stdout = %q", r.out.String())
	}
}

// V2: a vulnerability found is a failure; anything else govulncheck says is "cannot judge".
func TestVulnStatuses(t *testing.T) {
	for code, want := range map[int]ExitCode{3: ExitFailed, 1: ExitCannotRun, 2: ExitCannotRun} {
		r := inStep(t)
		r.goFake.codes["govulncheck"] = code
		r.goFake.outputs["govulncheck"] = "Vulnerability #1: GO-2026-0001\n"
		if got := r.vuln(t); got != want {
			t.Errorf("govulncheck status %d: exit %d, want %d", code, got, want)
		}
		if !strings.Contains(r.err.String(), "GO-2026-0001") || r.out.Len() != 0 {
			t.Errorf("status %d: stdout %q, stderr %q", code, r.out.String(), r.err.String())
		}
	}
}

func TestVulnThatCannotStartIsExitTwo(t *testing.T) {
	r := inStep(t)
	r.goFake.failRun = "govulncheck"
	if code := r.vuln(t); code != ExitCannotRun {
		t.Fatalf("code %d", code)
	}
}
