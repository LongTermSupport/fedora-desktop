package rules

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A directory listing's entry is used as a directory only once its type is asked
// (fedora-desktop#404): seats abandon took every entry of control/ for a session folder, and a
// regular file there refused every run. The fixtures are the defect (a path built in another
// package from the parsed name), the other spellings (one Join, a ReadDir of the entry, a local
// relaying the entry's path, Readdir's FileInfo through an fs.FS, a + chain, a listing a function
// returns), and the clean forms (IsDir, a switch on Type, a Stat of the entry itself, the entry
// opened as a file, names only, a range over strings, a test).
func TestWalkEntryType(t *testing.T) {
	root := filepath.Join(analysistest.TestData(), "walkentry")
	analysistest.Run(t, root, WalkEntryType, "fix/internal/badwalkentry", "fix/internal/goodwalkentry")
}
