package all

import (
	"strings"
	"testing"
)

// addedAfterPort are the gates the table gained after the bash was recorded. The goldens are
// what the shipped bash printed, so they have no line for these; the replay answers them with a
// passing reply and takes their stage line and their log entry out of the Go side before the
// comparison. This is not a deviation: it is not a bash bug, only a gate the bash never had.
var addedAfterPort = []struct{ name, reason string }{
	{"meta-deploy-queue", "Plan 00192, meta-deploy.list: the queue suite gets its own row instead of running inside the planlib suite"},
}

func isAddedAfterPort(name string) bool {
	for _, a := range addedAfterPort {
		if a.name == name {
			return true
		}
	}
	return false
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
