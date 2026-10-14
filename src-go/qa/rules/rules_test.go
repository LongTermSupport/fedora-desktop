package rules

import (
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
)

// Each analyzer is proven alone against a fixture: every line the rule must flag carries a
// want comment, and the clean constructs beside them carry none, so a rule that fires too
// much or too little fails here. The fixtures reproduce the defects the tools actually hit.

func TestSeamExec(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), SeamExec, "fix/internal/execx", "fix/qa/internal/proc", "fix/internal/badexec")
}

func TestSeamCommands(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), SeamCommands,
		"fix/internal/execx", "fix/internal/sudo", "fix/internal/hostnet", "fix/internal/badcmd", "fix/other/internal/tmuxccy",
		"fix/internal/podfreeze", "fix/internal/lxcfreeze", "fix/internal/sessprobe", "fix/internal/badrun")
}

func TestSeamCommandsDynamicNames(t *testing.T) {
	allow := []allowance{
		{pkg: "internal/dynamic", fn: "Launch", reason: "the program is chosen at run time"},
		{pkg: "internal/dynamic", fn: "Transient", reason: "a row excuses the Name, not a hidden wrapper program"},
		{pkg: "internal/dynamic", fn: "Runner.Run", reason: "the program is chosen at run time"},
	}
	old := programRunners
	programRunners = append(slices.Clone(old), programRunner{pkg: "internal/dynamic", recv: "Systemd", method: "RunTransient", argIndex: 2})
	t.Cleanup(func() { programRunners = old })
	analysistest.Run(t, analysistest.TestData(), newSeamCommands(allow), "fix/internal/dynamic")
}

func TestTmuxTargets(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), TmuxTargets, "fix/internal/tmuxccy", "fix/internal/term")
}

func TestTestClock(t *testing.T) {
	allow := []clockAllowance{{pkg: "internal/allowedclock", fn: "TestRealWait", reason: "proves the real sleep ran"}}
	analysistest.Run(t, analysistest.TestData(), newTestClock(allow), "fix/internal/badclock", "fix/internal/allowedclock")
}

func TestHomeAbbrev(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), HomeAbbrev, "fix/internal/term", "fix/qa/rules", "fix/internal/badhome")
}

func TestAllIsValid(t *testing.T) {
	if err := analysis.Validate(All()); err != nil {
		t.Fatal(err)
	}
	if got := len(All()); got != 5 {
		t.Fatalf("want 5 rules, got %d", got)
	}
}

func TestEveryDynamicNameHasAReason(t *testing.T) {
	for _, a := range dynamicNames {
		if a.reason == "" || a.pkg == "" || a.fn == "" {
			t.Errorf("allowance %+v needs a package, a function and a reason", a)
		}
	}
}

func TestEveryClockAllowanceHasAReason(t *testing.T) {
	for _, a := range clockAllowances {
		if a.reason == "" || a.pkg == "" || a.fn == "" {
			t.Errorf("allowance %+v needs a package, a function and a reason", a)
		}
	}
}

func TestOwnedBy(t *testing.T) {
	cases := []struct {
		pkg, owner string
		want       bool
	}{
		{"github.com/x/src-go/internal/execx", "internal/execx", true},
		{"internal/execx", "internal/execx", true},
		{"github.com/x/src-go/internal/execx_test", "internal/execx", true},
		{"github.com/x/src-go/internal/execx [github.com/x/src-go/internal/execx.test]", "internal/execx", true},
		{"github.com/x/src-go/internal/notexecx", "internal/execx", false},
		{"github.com/x/src-go/internal/execx/sub", "internal/execx", false},
	}
	for _, c := range cases {
		if got := ownedBy(c.pkg, c.owner); got != c.want {
			t.Errorf("ownedBy(%q, %q) = %v, want %v", c.pkg, c.owner, got, c.want)
		}
	}
}
