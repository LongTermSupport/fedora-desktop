package rules

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A path in a checkout a container writes is opened, written, made, removed or walked only
// through an os.Root of the checkout (or internal/checkoutpin). The fixtures are the defect (the
// launcher's .gitignore write through a planted link, Plan 00266 review finding 6), the path
// reached through a constant, a local, a package function and pieces of a join, the module's
// atomic writer and a walk, and the clean forms beside them (an os.Root, the home ~/.claude, a
// message); the seam's own package, a test file and the nested qa module complete the picture.
func TestCheckoutPathSeam(t *testing.T) {
	root := filepath.Join(analysistest.TestData(), "checkoutpath")
	analysistest.Run(t, root, CheckoutPathSeam, "fix/internal/front", "fix/internal/checkoutpin", "fix/qa/scan")
}
