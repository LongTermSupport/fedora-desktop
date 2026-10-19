package cmdmaintest

import (
	"fmt"
	"os"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
)

func TestMain(m *testing.M) { os.Exit(Main(m)) }

func TestRefusesClosedStdoutAcceptsAMainThatRefuses(t *testing.T) {
	RefusesClosedStdout(t, func() int {
		if cmdmain.RefuseClosedStdout() {
			return 2
		}
		return 0
	}, 2)
}

// The child's own code and stderr come back, with Main's closing line taken off.
func TestChildReturnsTheRunsCodeAndStderr(t *testing.T) {
	res := Child(t, ">/dev/null", func() int {
		fmt.Fprintln(os.Stderr, "from the child")
		return 5
	})
	if res.Code != 5 || res.Stderr != "from the child\n" {
		t.Fatalf("exit %d stderr %q", res.Code, res.Stderr)
	}
}

func TestWritesReportWithModeTellsTheTwoWritersApart(t *testing.T) {
	WritesReportWithMode(t, cmdmain.WriteReport, 0o644)
	WritesReportWithMode(t, cmdmain.WriteReportPrivate, 0o600)
}
