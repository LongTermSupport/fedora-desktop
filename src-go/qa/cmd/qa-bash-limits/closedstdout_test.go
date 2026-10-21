package main

import (
	"os"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/bashlimits"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmaintest"
)

func TestMain(m *testing.M) { os.Exit(cmdmaintest.Main(m)) }

// A closed stdout cannot carry the verdict, so the command refuses to run.
func TestClosedStdoutIsRefused(t *testing.T) {
	cmdmaintest.RefusesClosedStdout(t, func() int { return run([]string{"--help"}) }, bashlimits.ExitCannotRun)
}
