package rules

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// teamchat's bounds on real time: a bound a test can shorten (the originating probe, the guest
// root's timer, a call's field, and the ways round it) and a test that passes when a real timer
// fires are flagged; constant bounds, the injected clock, safety nets, the clock package, a
// realClocks row and a package outside teamchat are not.
func TestRealTimeBound(t *testing.T) {
	allow := []allowance{{pkg: "internal/teamchat/realclock", fn: realSleep, reason: "the real side of a clock seam"}}
	analysistest.Run(t, analysistest.TestData(), newRealTimeBound(allow),
		"fix/internal/teamchat/realtime", "fix/internal/teamchat/clock", "fix/internal/teamchat/realclock",
		"fix/internal/elsewheretime")
}

func TestEveryRealClockHasAReason(t *testing.T) {
	for _, a := range realClocks {
		if a.reason == "" || a.pkg == "" || a.fn == "" {
			t.Errorf("allowance %+v needs a package, a function and a reason", a)
		}
	}
}
