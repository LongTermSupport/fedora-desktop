package rules

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A failed call's class alone is a line that drops the cause (fedora-desktop#265). The fixtures
// are the ways it is printed (a log line, Sprintf, a concatenated error, stderr, a helper that
// hands the class back, the function as a value, a record nothing bounds, a test's log) and
// the clean forms beside them (the cause, a method named Class, the bounded records of
// classRecords by field, by position and through a pointer); package matrix defines the class
// and a command completes the picture. A variable between the class and a bounded record is
// judged as a line.
func TestClassWithoutDetail(t *testing.T) {
	root := filepath.Join(analysistest.TestData(), "classdetail")
	analysistest.Run(t, root, ClassWithoutDetail,
		"fix/internal/teamchat/matrix", "fix/internal/teamchat/seat", "fix/internal/teamchat/badclass", "fix/cmd/tool")
}

func TestEveryClassRecordHasAReason(t *testing.T) {
	for _, r := range classRecords {
		if r.pkg == "" || r.typ == "" || r.reason == "" {
			t.Errorf("class record %+v needs a package, a type and a reason", r)
		}
	}
}
