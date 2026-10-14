package podfreeze

import (
	"strings"
	"testing"
)

// The embedded usage text was checked byte for byte against the bash tool's usage() function; the
// golden replay (internal/freeze/golden) keeps that record, and these anchors stay.
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
