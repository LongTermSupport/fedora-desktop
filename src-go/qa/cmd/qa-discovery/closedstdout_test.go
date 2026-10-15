package main

import (
	"os"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/discovery"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmaintest"
)

// A closed stdout cannot carry the listing, so the command refuses to run.
func TestClosedStdoutIsRefused(t *testing.T) {
	cmdmaintest.RefusesClosedStdout(t, func() int { return run([]string{"--help"}, os.Stdout, os.Stderr) }, int(discovery.ExitCannotRun))
}
