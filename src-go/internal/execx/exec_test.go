package execx

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"testing"
)

func TestCombinedMergesStdoutAndStderrInOrder(t *testing.T) {
	out, err := Combined(context.Background(), OS{}, Cmd{Name: "sh", Args: []string{"-c", "echo one; echo two >&2; echo three"}})
	if err != nil {
		t.Fatal(err)
	}
	if out != "one\ntwo\nthree\n" {
		t.Fatalf("got %q", out)
	}
}

func TestCombinedPutsTheMergedTextInTheExitError(t *testing.T) {
	out, err := Combined(context.Background(), OS{}, Cmd{Name: "sh", Args: []string{"-c", "echo before; echo no server running >&2; exit 1"}})
	exit, ok := errors.AsType[*ExitError](err)
	if !ok {
		t.Fatalf("want *ExitError, got %v", err)
	}
	if out != "before\nno server running\n" || exit.Stderr != out || exit.Code != 1 {
		t.Fatalf("out %q, exit %+v", out, exit)
	}
}

func TestCombinedWithFakeMergesBothStreams(t *testing.T) {
	f := &Fake{}
	f.On([]string{"probe"}, Result{Stdout: []byte("out\n"), Stderr: []byte("err\n")})
	out, err := Combined(context.Background(), f, Cmd{Name: "probe"})
	if err != nil || out != "out\nerr\n" {
		t.Fatalf("got %q, %v", out, err)
	}
	f.On([]string{"bad"}, Result{Stderr: []byte("boom"), ExitCode: 2})
	out, err = Combined(context.Background(), f, Cmd{Name: "bad"})
	if exit, ok := errors.AsType[*ExitError](err); !ok || out != "boom" || exit.Stderr != "boom" {
		t.Fatalf("got %q, %v", out, err)
	}
}

func TestCombinedKeepsTheCallersOtherFields(t *testing.T) {
	f := &Fake{}
	f.On([]string{"probe"}, Result{})
	dir := t.TempDir()
	if _, err := Combined(context.Background(), f, Cmd{Name: "probe", Dir: dir, Env: []string{"A=1"}, Unset: []string{"B"}}); err != nil {
		t.Fatal(err)
	}
	got := f.Calls()[0]
	if got.Dir != dir || !slices.Equal(got.Env, []string{"A=1"}) || !slices.Equal(got.Unset, []string{"B"}) {
		t.Fatalf("got %+v", got)
	}
}

func TestCombinedPropagatesRunnerFailures(t *testing.T) {
	_, err := Combined(context.Background(), &Fake{}, Cmd{Name: "unscripted"})
	if !errors.Is(err, ErrUnexpected) {
		t.Fatalf("got %v", err)
	}
}

func TestChildEnvAddsAndRemoves(t *testing.T) {
	got := ChildEnv([]string{"A=1", "B=2", "C=3"}, []string{"D=4", "A=9"}, []string{"B", "C"})
	want := []string{"A=1", "D=4", "A=9"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v", got)
	}
	if got := ChildEnv([]string{"X=1"}, nil, nil); !slices.Equal(got, []string{"X=1"}) {
		t.Fatalf("got %v", got)
	}
}

func TestChildEnvDoesNotRemoveALongerNamePrefixMatch(t *testing.T) {
	got := ChildEnv([]string{"PASS=1", "PASSWORD=2"}, nil, []string{"PASS"})
	if !slices.Equal(got, []string{"PASSWORD=2"}) {
		t.Fatalf("got %v", got)
	}
}

func TestOSRunRemovesUnsetVariablesFromTheChild(t *testing.T) {
	t.Setenv("EXECX_SECRET", "hunter2")
	t.Setenv("EXECX_KEEP", "kept")
	res, err := OS{}.Run(context.Background(), Cmd{
		Name:  "sh",
		Args:  []string{"-c", `printf '[%s][%s]' "${EXECX_SECRET-unset}" "$EXECX_KEEP"`},
		Unset: []string{"EXECX_SECRET"},
	})
	if err != nil || string(res.Stdout) != "[unset][kept]" {
		t.Fatalf("got %q, %v", res.Stdout, err)
	}
}

func TestOSRunMapsAMissingProgramToNotInstalled(t *testing.T) {
	_, err := OS{}.Run(context.Background(), Cmd{Name: "execx-no-such-program-anywhere"})
	if !IsNotInstalled(err) || !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	ni, ok := errors.AsType[*NotInstalledError](err)
	if !ok || ni.Name != "execx-no-such-program-anywhere" {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(ni.Error(), "execx-no-such-program-anywhere is not installed") {
		t.Fatalf("text %q", ni.Error())
	}
}

func TestIsNotInstalledIsFalseForOtherErrors(t *testing.T) {
	if IsNotInstalled(nil) || IsNotInstalled(errors.New("x")) || IsNotInstalled(&ExitError{Name: "x", Code: 1}) {
		t.Fatal("only a missing program is not-installed")
	}
}

func TestFakeOnErrorAnswersWithTheError(t *testing.T) {
	f := &Fake{}
	f.OnError([]string{"fzf"}, &NotInstalledError{Name: "fzf"})
	_, err := f.Run(context.Background(), Cmd{Name: "fzf", Args: []string{"--ansi"}})
	if !IsNotInstalled(err) {
		t.Fatalf("got %v", err)
	}
	if !f.Called("fzf") {
		t.Fatal("the call is still recorded")
	}
}

func TestFakeExecRecordsAndAnswers(t *testing.T) {
	f := &Fake{}
	if err := f.Exec(Cmd{Name: "/launcher", Args: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	execs := f.Execs()
	if len(execs) != 1 || execs[0].Name != "/launcher" || execs[0].Args[0] != "a" {
		t.Fatalf("got %+v", execs)
	}
	if len(f.Calls()) != 0 {
		t.Fatal("an exec is not a run")
	}
	want := errors.New("exec refused")
	f.ExecError(want)
	if err := f.Exec(Cmd{Name: "/launcher"}); !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
}

func TestOSExecReplacesTheProcessThroughTheSeam(t *testing.T) {
	var gotPath string
	var gotArgv, gotEnv []string
	old := syscallExec
	syscallExec = func(path string, argv, env []string) error {
		gotPath, gotArgv, gotEnv = path, argv, env
		return nil
	}
	t.Cleanup(func() { syscallExec = old })
	t.Setenv("EXECX_SECRET", "x")
	err := OS{}.Exec(Cmd{Name: "sh", Args: []string{"-c", "true"}, Env: []string{"ADDED=1"}, Unset: []string{"EXECX_SECRET"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(gotPath, "/sh") || !slices.Equal(gotArgv, []string{"sh", "-c", "true"}) {
		t.Fatalf("path %q argv %v", gotPath, gotArgv)
	}
	if !slices.Contains(gotEnv, "ADDED=1") || slices.Contains(gotEnv, "EXECX_SECRET=x") {
		t.Fatalf("env %v", gotEnv)
	}
}

func TestOSExecWithAPathNeedsNoLookup(t *testing.T) {
	var gotPath string
	old := syscallExec
	syscallExec = func(path string, _, _ []string) error { gotPath = path; return nil }
	t.Cleanup(func() { syscallExec = old })
	if err := (OS{}).Exec(Cmd{Name: "/opt/launcher"}); err != nil || gotPath != "/opt/launcher" {
		t.Fatalf("path %q, %v", gotPath, err)
	}
}

func TestOSExecMissingProgramAndSyscallFailure(t *testing.T) {
	err := OS{}.Exec(Cmd{Name: "execx-no-such-program-anywhere"})
	if !IsNotInstalled(err) {
		t.Fatalf("got %v", err)
	}
	old := syscallExec
	syscallExec = func(string, []string, []string) error { return syscall.EACCES }
	t.Cleanup(func() { syscallExec = old })
	err = OS{}.Exec(Cmd{Name: "/opt/launcher"})
	if !errors.Is(err, syscall.EACCES) || !strings.Contains(err.Error(), "/opt/launcher") {
		t.Fatalf("got %v", err)
	}
}
