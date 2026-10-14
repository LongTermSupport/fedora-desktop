package podfreeze

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// usageEcho is one `echo "..."` line of the bash tool's usage() function.
var usageEcho = regexp.MustCompile(`^    echo "(.*)"$`)

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for dir := wd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if _, statErr := os.Stat(filepath.Join(dir, "files/home/.local/bin/podfreeze")); statErr == nil {
			return dir
		}
	}
	t.Fatalf("repo root not found above %s", wd)
	return ""
}

// The usage text is the bash tool's usage() function, not its header comment, so the embedded
// file must equal what the shipped tool prints. This parity test reads the shipped bash and is
// retired with it at cut-over; the anchor test below is what stays.
func TestUsageEqualsWhatTheBashToolPrints(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "files/home/.local/bin/podfreeze"))
	if err != nil {
		t.Fatal(err)
	}
	var want strings.Builder
	inUsage := false
	for line := range strings.SplitSeq(string(raw), "\n") {
		switch {
		case line == "usage() {":
			inUsage = true
		case inUsage && line == "}":
			if usageText != want.String() {
				t.Fatalf("embedded usage differs from the bash usage():\n--- embedded\n%s\n--- bash\n%s", usageText, want.String())
			}
			return
		case inUsage:
			m := usageEcho.FindStringSubmatch(line)
			if m == nil {
				t.Fatalf("usage() holds a line this test does not understand: %q", line)
			}
			want.WriteString(strings.ReplaceAll(m[1], `\"`, `"`) + "\n")
		}
	}
	t.Fatal("usage() not found in the bash tool")
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
