package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/branchhygiene"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmaintest"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func TestMain(m *testing.M) { os.Exit(cmdmaintest.Main(m)) }

// A closed stdout cannot carry the verdict, so the command refuses to run.
func TestClosedStdoutIsRefused(t *testing.T) {
	cmdmaintest.RefusesClosedStdout(t, func() int { return run([]string{"--help"}) }, int(branchhygiene.ExitCannotRun))
}

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(branchhygiene.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(branchhygiene.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
}

// A real repository with no origin cannot be judged, and says so rather than passing.
func TestRepositoryWithoutOriginIsExitTwo(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o750); err != nil {
		t.Fatal(err)
	}
	policy := "cap = 10\nwarn_days = 3\nfail_days = 7\n"
	if err := os.WriteFile(filepath.Join(root, "scripts", "branch-policy.toml"), []byte(policy), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := proc.OS{}.Run(t.Context(), proc.Cmd{Argv: []string{"git", "init", "-q"}, Dir: root})
	if err != nil || res.Code != 0 {
		t.Fatalf("git init: %v %+v", err, res)
	}
	if code := run([]string{"--root", root}); code != int(branchhygiene.ExitCannotRun) {
		t.Fatalf("exit %d", code)
	}
}
