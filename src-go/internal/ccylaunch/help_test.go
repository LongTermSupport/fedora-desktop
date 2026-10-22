package ccylaunch

import (
	"strconv"
	"strings"
	"testing"
)

// The embedded help is the bash launcher's --help text byte for byte: the bash still prints it
// under CCY_LEGACY, so a change to one that is not made to the other fails here.
func TestHelpIsTheBashLaunchersText(t *testing.T) {
	text := checkoutFile(t, launcherRel)
	open := "if [ \"$1\" = \"--help\" ] || [ \"$1\" = \"-h\" ]; then\n    cat << 'EOF'\n"
	start := strings.Index(text, open)
	if start < 0 {
		t.Fatal("the launcher has no --help heredoc")
	}
	body := text[start+len(open):]
	end := strings.Index(body, "\nEOF\n")
	if end < 0 {
		t.Fatal("the launcher's --help heredoc does not end")
	}
	want := body[:end+1]
	if helpText != want {
		t.Fatalf("help.txt differs from the launcher's --help text:\n%s", firstDifference(want, helpText))
	}
}

// firstDifference is the first line where got leaves want, with its number.
func firstDifference(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := range max(len(w), len(g)) {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			return "line " + strconv.Itoa(i+1) + ":\n  launcher: " + wl + "\n  help.txt: " + gl
		}
	}
	return "(same lines)"
}
