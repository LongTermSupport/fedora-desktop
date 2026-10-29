package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmaintest"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/qalock"
)

func TestMain(m *testing.M) { os.Exit(cmdmaintest.Main(m)) }

// A closed stdout cannot carry the command's output, so qa-lock refuses to run.
func TestClosedStdoutIsRefused(t *testing.T) {
	cmdmaintest.RefusesClosedStdout(t, func() int { return run(context.Background(), []string{"--help"}, osDeps()) }, exitCannotRun)
}

// recordingTaker is a free lock that records the take and whether it was released.
type recordingTaker struct {
	root     string
	noWait   bool
	taken    int
	released int
	err      error
}

func (r *recordingTaker) take(_ context.Context, root string, noWait bool) (func() error, error) {
	r.root, r.noWait = root, noWait
	r.taken++
	if r.err != nil {
		return nil, r.err
	}
	return func() error { r.released++; return nil }, nil
}

func testDeps(r *recordingTaker, out, errOut *strings.Builder) deps {
	return deps{
		taker:  func(string) qalock.Taker { return r.take },
		runner: proc.OS{},
		stdin:  strings.NewReader(""),
		stdout: out,
		stderr: errOut,
	}
}

func TestTheCommandRunsUnderTheLockWithItsOwnExitCode(t *testing.T) {
	r := &recordingTaker{}
	var out, errOut strings.Builder
	code := run(context.Background(), []string{"--name", "qa-go-qa", "--root", "/repo", "--", "sh", "-c", "echo out; echo err >&2; exit 3"}, testDeps(r, &out, &errOut))
	if code != 3 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	if out.String() != "out\n" || errOut.String() != "err\n" {
		t.Fatalf("streams %q %q", out.String(), errOut.String())
	}
	if r.taken != 1 || r.released != 1 || r.root != "/repo" || r.noWait {
		t.Fatalf("taker %+v", r)
	}
}

func TestNoWaitReachesTheLockAndARefusalRunsNothing(t *testing.T) {
	r := &recordingTaker{err: errors.New("the QA lock is held by the QA run 9 (qa-all, ...)")}
	var out, errOut strings.Builder
	marker := filepath.Join(t.TempDir(), "ran")
	code := run(context.Background(), []string{"--no-wait", "--name", "qa-go-qa", "--root", "/repo", "--", "touch", marker}, testDeps(r, &out, &errOut))
	if code != exitCannotRun || !r.noWait {
		t.Fatalf("exit %d, taker %+v", code, r)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the command ran without the lock")
	}
	if !strings.Contains(errOut.String(), "ERROR: qa-go-qa: the QA lock is held by the QA run 9") {
		t.Fatalf("stderr %q", errOut.String())
	}
}

func TestAnInterruptWhileWaitingIsExitInterrupted(t *testing.T) {
	r := &recordingTaker{err: context.Canceled}
	var out, errOut strings.Builder
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := run(ctx, []string{"--name", "n", "--root", "/r", "--", "true"}, testDeps(r, &out, &errOut)); code != exitInterrupted {
		t.Fatalf("exit %d", code)
	}
}

func TestACommandThatCannotStartOrIsKilledIsExitTwo(t *testing.T) {
	r := &recordingTaker{}
	var out, errOut strings.Builder
	if code := run(context.Background(), []string{"--name", "n", "--root", "/r", "--", "/no/such/program"}, testDeps(r, &out, &errOut)); code != exitCannotRun {
		t.Fatalf("missing program: exit %d", code)
	}
	if r.released != 1 {
		t.Fatal("the lock was not released after a command that could not start")
	}
	errOut.Reset()
	if code := run(context.Background(), []string{"--name", "n", "--root", "/r", "--", "sh", "-c", "kill -9 $$"}, testDeps(r, &out, &errOut)); code != exitCannotRun {
		t.Fatalf("killed: exit %d", code)
	}
	if !strings.Contains(errOut.String(), "killed") {
		t.Fatalf("stderr %q", errOut.String())
	}
}

func TestTheCommandLineIsStrict(t *testing.T) {
	cases := map[string][]string{
		"no command":     {"--name", "n", "--root", "/r", "--"},
		"no separator":   {"--name", "n", "--root", "/r", "true"},
		"no name":        {"--root", "/r", "--", "true"},
		"no root":        {"--name", "n", "--", "true"},
		"unknown flag":   {"--nope", "--name", "n", "--root", "/r", "--", "true"},
		"missing value":  {"--name"},
		"flag as value":  {"--name", "--root", "/r", "--", "true"},
		"empty argument": {},
	}
	for name, args := range cases {
		r := &recordingTaker{}
		var out, errOut strings.Builder
		if code := run(context.Background(), args, testDeps(r, &out, &errOut)); code != exitCannotRun {
			t.Errorf("%s: exit %d", name, code)
		}
		if r.taken != 0 {
			t.Errorf("%s: took the lock", name)
		}
		if !strings.Contains(errOut.String(), "usage: qa-lock") {
			t.Errorf("%s: no usage on stderr: %q", name, errOut.String())
		}
	}
	var out, errOut strings.Builder
	if code := run(context.Background(), []string{"--help"}, testDeps(&recordingTaker{}, &out, &errOut)); code != 0 || !strings.HasPrefix(out.String(), "usage: qa-lock") {
		t.Fatalf("--help: exit %d out %q", code, out.String())
	}
}

// TestTheRealLockIsExportedToTheCommand runs a real command under the real lock of a scratch
// repository: the command sees QA_LOCK_HELD naming the lock, which is what lets a QA run it
// starts inherit the lock instead of waiting for it.
func TestTheRealLockIsExportedToTheCommand(t *testing.T) {
	t.Setenv(qalock.HeldEnv, "")
	t.Setenv(qalock.TimeoutEnv, "")
	root := t.TempDir()
	if res, err := (proc.OS{}).Run(context.Background(), proc.Cmd{Argv: []string{"git", "init", "-q", root}}); err != nil || res.Code != 0 {
		t.Fatalf("git init: %v %+v", err, res)
	}
	var out, errOut strings.Builder
	d := osDeps()
	d.stdout, d.stderr, d.stdin = &out, &errOut, strings.NewReader("")
	code := run(context.Background(), []string{"--name", "qa-go-qa", "--root", root, "--", "sh", "-c", `printf %s "$QA_LOCK_HELD"`}, d)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	if want := filepath.Join(root, ".git", qalock.FileName); out.String() != want {
		t.Fatalf("the command saw %s=%q, want %q", qalock.HeldEnv, out.String(), want)
	}
}
