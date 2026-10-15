package main

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmaintest"
)

// The report writer the command wires decides who can read the report: a swap of the
// world-readable and the private writer fails here.
func TestTheWiredReportWriterSetsTheReportMode(t *testing.T) {
	cmdmaintest.WritesReportWithMode(t, newEnv("/").WriteFile, 0o600)
}
