package rules

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A session record's launcher= path is read only in internal/registry, whose Launcher gives the
// fixed table's program for the record's prefix. The fixtures are the defect (restore's start of
// a recorded session) and the second instance the sweep found (a team restart's start), a read
// kept in a variable, the clean forms beside them (the table, a comparison, a write), a test file
// and a Launcher field of another type.
func TestRecordLauncherExec(t *testing.T) {
	root := filepath.Join(analysistest.TestData(), "recordlauncher")
	analysistest.Run(t, root, RecordLauncherExec,
		"fix/internal/registry", "fix/internal/sessions", "fix/internal/teams/restart", "fix/internal/other")
}
