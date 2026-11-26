package rules

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// An os.Root follows a link that stays inside the root at the last name, O_NOFOLLOW or not, so
// a create through it without O_EXCL makes the file a dangling link names (the batch 22b review
// of the seat-record lock, Plan 00265 R36). The fixtures are the defect, O_NOFOLLOW added, the
// flags held in a local, Create, WriteFile and a function literal; and the clean forms (O_EXCL,
// an open without O_CREATE, os.OpenFile, flags the body does not spell, a test).
func TestRootCreateFollowsLink(t *testing.T) {
	root := filepath.Join(analysistest.TestData(), "rootcreate")
	analysistest.Run(t, root, RootCreateFollowsLink, "fix/internal/badroot", "fix/internal/goodroot")
}
