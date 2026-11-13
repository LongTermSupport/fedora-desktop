package rules

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A unit kept running by Restart=always and written with no [Install] section is flagged in
// each form a unit text takes in Go: a constant built from other constants (the homeserver's
// head, issue 247), a concatenation with non-constant parts (the terminator), a raw string, and a
// list of lines. A unit with [Install], a timer-started oneshot, a drop-in with no Description=
// and a unit text in a test file are not.
func TestUnitNoInstall(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), UnitNoInstall, "fix/internal/badunit")
}
