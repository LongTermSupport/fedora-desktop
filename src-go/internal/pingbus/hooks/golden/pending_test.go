package golden

import "github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/hooks"

// While internal/pingbus/hooks is the stand-in, the default run has nothing to judge. The port
// does not declare Placeholder: delete this file when it lands, and the default run judges it.
func init() {
	goPending = hooks.Placeholder
}
