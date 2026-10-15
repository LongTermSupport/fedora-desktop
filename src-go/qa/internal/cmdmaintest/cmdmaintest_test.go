package cmdmaintest

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
)

func TestRefusesClosedStdoutAcceptsAMainThatRefuses(t *testing.T) {
	RefusesClosedStdout(t, func() int {
		if cmdmain.RefuseClosedStdout() {
			return 2
		}
		return 0
	}, 2)
}

func TestWritesReportWithModeTellsTheTwoWritersApart(t *testing.T) {
	WritesReportWithMode(t, cmdmain.WriteReport, 0o644)
	WritesReportWithMode(t, cmdmain.WriteReportPrivate, 0o600)
}
