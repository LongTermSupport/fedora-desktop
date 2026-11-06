package main

import (
	"os"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmaintest"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/privatenames"
)

func TestMain(m *testing.M) { os.Exit(cmdmaintest.Main(m)) }

func TestClosedStdoutIsRefused(t *testing.T) {
	cmdmaintest.RefusesClosedStdout(t, func() int { return run([]string{"--help"}) }, int(privatenames.ExitCannotRun))
}

func TestContainerIsTheWorkspaceCheckout(t *testing.T) {
	for cwd, want := range map[string]bool{
		"/workspace": true, "/workspace/.claude/worktrees/x": true, "/workspacey": false, "/home/u/repo": false, "": false,
	} {
		if got := inContainer(cwd); got != want {
			t.Errorf("inContainer(%q) = %t, want %t", cwd, got, want)
		}
	}
}
