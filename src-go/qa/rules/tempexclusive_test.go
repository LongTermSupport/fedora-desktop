package rules

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A temporary that is renamed or linked into place is created exclusively (fedora-desktop#417
// review): fsx.WriteAtomic wrote `<path>.tmp.<pid>` with os.WriteFile, which follows a link
// planted at that predictable name, so anyone who can write the directory (a container sharing
// it) could make the write land elsewhere. The fixtures are the defect, the other create calls
// (os.Create, OpenFile without O_EXCL, an os.Root's WriteFile and Create, flags held in a
// variable, unix.Openat), a link in place of a rename and a function literal; and the clean
// forms (os.CreateTemp, O_EXCL spelled and held in a constant, the raw call with O_EXCL, a file
// written in place, a file renamed aside that the function did not create, a test).
func TestTempNotExclusive(t *testing.T) {
	root := filepath.Join(analysistest.TestData(), "tempexclusive")
	analysistest.Run(t, root, TempNotExclusive, "fix/internal/badtemp", "fix/internal/goodtemp")
}
