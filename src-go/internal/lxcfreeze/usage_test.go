package lxcfreeze

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze/freezetest"
)

// The usage text is the bash tool's usage() function, not its header comment, so the embedded
// file must equal what the shipped tool prints. This parity test reads the shipped bash and is
// retired with it at cut-over; the anchor test below is what stays.
func TestUsageEqualsWhatTheBashToolPrints(t *testing.T) {
	if want := freezetest.BashUsage(t, tool); usageText != want {
		t.Fatalf("embedded usage differs from the bash usage():\n--- embedded\n%s\n--- bash\n%s", usageText, want)
	}
}

func TestUsageAnchors(t *testing.T) {
	for _, want := range []string{
		"Usage: lxcfreeze [list|freeze|thaw] [target] [options]\n",
		"  --bridge BR        Every container whose first interface is on BR.\n",
		"  Every operation needs root, including the read-only inventory:\n",
		"  -n, --dry-run      Print the resolved set and exit; change nothing.\n",
		"  lxcfreeze thaw mycontainer\n",
	} {
		if !strings.Contains(usageText, want) {
			t.Errorf("usage lost %q", want)
		}
	}
	if !strings.HasSuffix(usageText, "\n") || strings.Contains(usageText, "\n\n\n") {
		t.Error("usage ends with one newline and has no doubled blank lines")
	}
	for _, foreign := range []string{"--ccy", "--network", "--github", "--token", "--ssh-key", "podman "} {
		if strings.Contains(usageText, foreign) {
			t.Errorf("%q is podfreeze's", foreign)
		}
	}
}
