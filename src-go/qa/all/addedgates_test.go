package all

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/gates"
)

// addedAfterPort are the gates the table gained after the bash was recorded. The goldens are
// what the shipped bash printed, so they have no line for these; the replay answers them with a
// passing reply and takes their stage line and their log entry out of the Go side before the
// comparison. This is not a deviation: it is not a bash bug, only a gate the bash never had.
var addedAfterPort = []struct{ name, reason string }{
	{"meta-deploy-queue", "Plan 00192, meta-deploy.list: the queue suite gets its own row instead of running inside the planlib suite"},
	{"hidden-bindings", "Plan 00203, Defence Before Fix: project_hidden_bindings outside Go (shell fzf and Python Textual)"},
	{"ccy-container-name", "Plan 00203, the container name race: ccy's name claim (ccy-teams internal claim-container-name) gets its own suite"},
	{"go-deps", "Plan 00204 D2: src-go may have dependencies, vendored, pinned and allowlisted, and this gate holds them to that"},
	{"go-vuln", "Plan 00204 D2: govulncheck over src-go's vendored dependencies"},
}

func isAddedAfterPort(name string) bool {
	for _, a := range addedAfterPort {
		if a.name == name {
			return true
		}
	}
	return false
}

// passingReply is what an added gate prints when it passes: its captured wording for a gate read
// by a detail pattern, else a case count.
func passingReply(t *testing.T, g gates.Gate) string {
	t.Helper()
	if slices.ContainsFunc(g.Readers, func(r gates.Reader) bool { return r.Detail != nil }) {
		return realWording(t, g.Name)
	}
	return "passed: 1\n"
}

// withoutAddedGates removes the stage lines of the added gates from the runner's stdout.
func withoutAddedGates(stdout string) string {
	lines := strings.SplitAfter(stdout, "\n")
	kept := lines[:0]
	for _, l := range lines {
		drop := false
		for _, a := range addedAfterPort {
			drop = drop || strings.HasPrefix(l, "✓ "+a.name+":")
		}
		if !drop {
			kept = append(kept, l)
		}
	}
	return strings.Join(kept, "")
}

// retiredAfterPort are the gates the bash ran that the table has since dropped: their suites
// tested a bash tool a Go port replaced, and the port's Go tests are the proof now. The goldens
// are what the bash printed over the table it had, so the replay puts each row back where it
// stood (before the row named by before, in list order) and answers it as the capture did.
var retiredAfterPort = []struct{ row, before, reason string }{
	{
		"gate\tvmtest-host-only-gate\tscripts/test-vmtest-host-only-gate.bash\tvmtest host-only gate unit tests failed\tcount",
		"vmtest-kernel-selection", "Plan 00192, vmtest port: the bash vmtest is gone; internal/vmtest TestE30* is the gate",
	},
	{
		"gate\tvmtest-reboot-dispatch\tscripts/test-vmtest-reboot-dispatch.bash\tvmtest reboot dispatch unit tests failed\tcount",
		"vmtest-kernel-selection", "Plan 00192, vmtest port: internal/vmtest TestE38*, TestE39* and TestE40* replace it",
	},
	{
		"gate\tvmtest-build-dir-cleanup\tscripts/test-vmtest-build-dir-cleanup.bash\tvmtest build directory cleanup tests failed\tcount",
		"vmtest-prepare-record", "Plan 00192, vmtest port: internal/vmtest TestE21* replaces it",
	},
	{
		"gate\tvmtest-member\tscripts/test-vmtest-member.bash\tvmtest member lifecycle tests failed\tcount",
		"vmtest-prepare-record", "Plan 00192, vmtest port: internal/vmtest TestE55* to TestE60* replace it",
	},
}

// gateName is a table row's gate name, or "" for a line that is not a gate row.
func gateName(line string) string {
	fields := strings.Split(line, "\t")
	if len(fields) < 2 || fields[0] != "gate" {
		return ""
	}
	return fields[1]
}

// tableAsRecorded is the real table with the retired rows put back where they stood.
func tableAsRecorded(t *testing.T) (string, []gates.Gate) {
	t.Helper()
	body, current := realTable(t)
	lines := strings.Split(body, "\n")
	for _, r := range retiredAfterPort {
		at := slices.IndexFunc(lines, func(l string) bool { return gateName(l) == r.before })
		if at < 0 {
			t.Fatalf("retired row %s stood before %s, which is no longer in the table", gateName(r.row), r.before)
		}
		lines = slices.Insert(lines, at, r.row)
	}
	restored := strings.Join(lines, "\n")
	path := filepath.Join(t.TempDir(), "qa-gates.tsv")
	if err := os.WriteFile(path, []byte(restored), 0o600); err != nil {
		t.Fatal(err)
	}
	gs, err := gates.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(gs) != len(current)+len(retiredAfterPort) {
		t.Fatalf("restored %d gates from %d and %d retired", len(gs), len(current), len(retiredAfterPort))
	}
	return restored, gs
}

// A retired gate still in the table, or one listed with no reason, is rot.
func TestEveryGateRetiredAfterThePortIsGoneFromTheTable(t *testing.T) {
	body, gs := realTable(t)
	for _, r := range retiredAfterPort {
		name := gateName(r.row)
		if name == "" || r.reason == "" {
			t.Errorf("%q is listed with no gate name or no reason", r.row)
		}
		if slices.ContainsFunc(gs, func(g gates.Gate) bool { return g.Name == name }) {
			t.Errorf("%s is listed as retired but is still in scripts/qa-gates.tsv: drop it from retiredAfterPort", name)
		}
	}
	restored, all := tableAsRecorded(t)
	for line := range strings.SplitSeq(body, "\n") {
		if !strings.Contains(restored, line) {
			t.Fatalf("the restored table lost %q", line)
		}
	}
	if len(all) != len(gs)+len(retiredAfterPort) {
		t.Fatalf("restored %d gates", len(all))
	}
}

// A listed gate that left the table, or a list entry with no reason, is rot.
func TestEveryGateAddedAfterThePortIsStillInTheTable(t *testing.T) {
	_, gs := realTable(t)
	for _, a := range addedAfterPort {
		if a.reason == "" {
			t.Errorf("%s is listed with no reason", a.name)
		}
		found := false
		for _, g := range gs {
			found = found || g.Name == a.name
		}
		if !found {
			t.Errorf("%s is listed as added after the port but is no longer in scripts/qa-gates.tsv: drop it from addedAfterPort", a.name)
		}
	}
}
