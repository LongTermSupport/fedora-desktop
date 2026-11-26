package rules

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// The files of a checkout that the host applies (the project mounts, the compose files) are named
// only in internal/checkoutpin, which reads them through the approval a person recorded. The
// fixtures are the defect (restore's own list of compose files, which it used to judge a compose
// answer by), the mounts path spelt whole and in a constant expression, an override file, a
// command naming one, and the clean forms beside them (a name of the pin package, a word that is
// only part of a name); the owner, a test file and a package of the nested qa module complete
// the picture.
func TestCheckoutConfigPin(t *testing.T) {
	root := filepath.Join(analysistest.TestData(), "checkoutpin")
	analysistest.Run(t, root, CheckoutConfigPin,
		"fix/internal/checkoutpin", "fix/internal/sessions", "fix/cmd/ccy-teams", "fix/qa/scan")
}
