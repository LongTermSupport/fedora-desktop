package rules

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// An approval is recorded only where a person was asked. The fixtures are the defect (the
// attended compose check, which recorded whatever the files held, unshown), a record in a
// closure asked nowhere, and the clean forms beside them: the approve verb, which asks first;
// a question asked through a ui prompt's YesNo; and WriteLastLaunch, which records the bytes it
// itself writes. A test file is not judged.
func TestPinRecordUnconfirmed(t *testing.T) {
	root := filepath.Join(analysistest.TestData(), "pinrecord")
	analysistest.Run(t, root, PinRecordUnconfirmed, "fix/internal/checkoutpin", "fix/internal/ccylaunch")
}
