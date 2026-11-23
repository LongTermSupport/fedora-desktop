package rules

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// An event floor held as a millisecond alone (Plan 00209 R0 step 7: the questions floor dropped an
// owner's answer sent in the millisecond of the post before it): the originating qtake shape, a
// floor raised by max, floor against floor, a struct field, a map read, a call's result, a
// Floor's MS taken apart, and a since bound cut by <= or > are flagged; Floor's own methods,
// Covers and Max, an equality, a since cut by <, a test file and a package outside teamchat are not.
func TestFloorByTime(t *testing.T) {
	analysistest.Run(t, filepath.Join(analysistest.TestData(), "floor"), FloorByTime,
		"fix/internal/teamchat/member/state", "fix/internal/teamchat/member/badfloor",
		"fix/internal/teamchat/admin/badsince", "fix/internal/floorelsewhere")
}
