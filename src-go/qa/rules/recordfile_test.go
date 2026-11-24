package rules

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A session record's file is the one the registry's reading found it in, never one rebuilt from
// the record's name= (fedora-desktop#396, #398): a hand-edited or renamed record is otherwise
// left behind as a stale record, or named wrongly. The fixtures are the defect (the picker's
// name-keyed Remove, the restore's join of the registry directory and rec.Name), the other
// spellings (Remove as a method value and a method expression, the directory held in a local,
// a package variable, a struct field set by a literal and by an assignment, joined by path.Join
// and by +), the clean forms (the registry's own writer, the file the reading carries, the
// directory alone, its parent, another directory, another package's Remove, tests). The root is
// its own, because the registry is matched by package path.
func TestRecordFileByName(t *testing.T) {
	root := filepath.Join(analysistest.TestData(), "recordfile")
	analysistest.Run(t, root, RecordFileByName,
		"fix/internal/registry", "fix/internal/badrecordfile", "fix/internal/goodrecordfile")
}
