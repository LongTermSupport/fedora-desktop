package podfreeze

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
		"Usage: podfreeze [list|freeze|thaw] [target] [options]\n",
		"  --network NET      Every container attached to network NET.\n",
		"  -n, --dry-run      Print the resolved set and exit; change nothing.\n",
		"  podfreeze thaw --all\n",
	} {
		if !strings.Contains(usageText, want) {
			t.Errorf("usage lost %q", want)
		}
	}
	if !strings.HasSuffix(usageText, "\n") || strings.Contains(usageText, "\n\n\n") {
		t.Error("usage ends with one newline and has no doubled blank lines")
	}
	if strings.Contains(usageText, "--bridge") {
		t.Error("--bridge is lxcfreeze's")
	}
}
