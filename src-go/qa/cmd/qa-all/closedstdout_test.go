package main

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/all"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmaintest"
)

// A closed stdout cannot carry the verdict, so the command refuses to run.
func TestClosedStdoutIsRefused(t *testing.T) {
	cmdmaintest.RefusesClosedStdout(t, func() int { return run([]string{"--help"}) }, int(all.ExitCannotRun))
}
