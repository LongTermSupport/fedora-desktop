package rules

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// Bounds on real time: a bound a test can shorten (the originating probe, a timer on a field,
// a call's field, and the ways round it) and a test that passes when a real timer fires are
// flagged; constant bounds, an injected clock, safety nets, and the rows of an allowance table
// (a real clock, a process bound) are not, and each row excuses nothing beside it.
func TestRealTimeBound(t *testing.T) {
	allow := []allowance{
		{pkg: "internal/realclock", fn: "systemClock.Sleep", reason: "the real side of a clock seam"},
		{pkg: "internal/realclock", fn: "runChild", reason: "a bound on a real child process"},
	}
	analysistest.Run(t, analysistest.TestData(), newRealTimeBound(allow),
		"fix/internal/realtime", "fix/internal/realclock")
}

func TestEveryRealTimeAllowanceHasAReason(t *testing.T) {
	for _, a := range realTimeAllowances() {
		if a.reason == "" || a.pkg == "" || a.fn == "" {
			t.Errorf("allowance %+v needs a package, a function and a reason", a)
		}
	}
}
