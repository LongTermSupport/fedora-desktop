package lxcfreeze

import (
	"strings"
	"testing"
)

// sudo is re-readied before each load and each freeze or thaw (the idle menu may outlive sudo's
// timestamp), so the usage text must not promise one prompt per run.
func TestUsageSaysWhenSudoIsAsked(t *testing.T) {
	if strings.Contains(usageText, "once per run") {
		t.Error("usage promises one password prompt per run; sudo is asked again after its timestamp expires")
	}
	want := "  /var/lib/lxc is root-only. sudo is asked again if its timestamp has expired.\n"
	if !strings.Contains(usageText, want) {
		t.Errorf("usage lost %q", want)
	}
}

// The embedded usage text was checked byte for byte against the bash tool's usage() function; the
// golden replay (internal/freeze/golden) keeps that record, and these anchors stay.
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
