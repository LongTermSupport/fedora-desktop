package businstall

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

type sweepCase struct {
	name  string
	setup func(h *harness) string // returns the team file when the command needs one
	args  func(h *harness, file string) []string
}

// sweepAllow gives the team a firewall rule, so firewall-cmd is among the calls swept.
var sweepAllow = []string{"198.51.100.0/24"}

func installedAlpha(h *harness) string { return h.installTeam(sweepAllow) }

func backedUpAlpha(h *harness) string {
	file := h.installTeam(nil)
	if code := h.run("backup-now", "--team", "alpha"); code != 0 {
		h.t.Fatalf("backup-now exited %d:\n%s", code, h.stderr())
	}
	return file
}

var sweepCases = []sweepCase{
	{"software", func(*harness) string { return "" }, func(h *harness, _ string) []string {
		return []string{"software", "--source", h.source, "--bus-address", "192.0.2.10"}
	}},
	{"team (first)", func(h *harness) string { h.software(); return h.teamFile("alpha", []string{"192.0.2.10"}, sweepAllow) }, func(_ *harness, file string) []string { return []string{"team", "--team-file", file} }},
	{"team (again)", installedAlpha, func(_ *harness, file string) []string { return []string{"team", "--team-file", file} }},
	{"check", installedAlpha, func(*harness, string) []string { return []string{"check", "--team", "alpha"} }},
	{"backup-now", installedAlpha, func(*harness, string) []string { return []string{"backup-now", "--team", "alpha"} }},
	{"restore", backedUpAlpha, func(*harness, string) []string { return []string{"restore", "--team", "alpha", "--backup", "1"} }},
	{"remove", installedAlpha, func(*harness, string) []string { return []string{"remove", "--team", "alpha"} }},
	{"remove --purge", installedAlpha, func(*harness, string) []string { return []string{"remove", "--team", "alpha", "--purge"} }},
}

// callsOf is the distinct external calls a successful run of the case makes, in order.
func callsOf(t *testing.T, c sweepCase) []string {
	t.Helper()
	h := newHarness(t)
	file := c.setup(h)
	if code := h.run(c.args(h, file)...); code != 0 {
		t.Fatalf("%s exited %d:\n%s", c.name, code, h.stderr())
	}
	var calls []string
	for _, line := range h.sim.log {
		line = strings.ReplaceAll(strings.ReplaceAll(line, h.prefix, "{P}"), h.source, "{S}")
		// A work directory is named at random; the call is told apart by what comes before it.
		if before, _, found := strings.Cut(line, workDirPattern); found {
			line = before + workDirPattern
		}
		if !slices.Contains(calls, line) {
			calls = append(calls, line)
		}
	}
	return calls
}

// resultProbes are the calls whose failure is an answer, not an error: a package that is not
// installed is installed, a unit that is not enabled is enabled, a rich rule that is not there
// is added (firewalld.Query).
var resultProbes = regexp.MustCompile(`^(rpm -q --quiet |systemctl is-enabled --quiet |` + ruleQueryText + `)`)

// ruleQuery is a rich-rule query, which `check` turns into a FAIL fact instead of a result.
var ruleQuery = regexp.MustCompile(`^` + ruleQueryText)

const ruleQueryText = `firewall-cmd .*--query-rich-rule=`

func TestEveryExternalCallThatFailsFailsTheCommandThatMadeIt(t *testing.T) {
	for _, c := range sweepCases {
		for _, call := range callsOf(t, c) {
			t.Run(c.name+" / "+call, func(t *testing.T) {
				h := newHarness(t)
				file := c.setup(h)
				rule := strings.ReplaceAll(strings.ReplaceAll(call, "{P}", h.prefix), "{S}", h.source)
				h.sim.fails = append(h.sim.fails, failRule{prefix: rule, code: 1})
				code := h.run(c.args(h, file)...)
				switch {
				case c.name == "check" && ruleQuery.MatchString(call):
					// check reports a rule that is not there as a FAIL fact, and fails on it.
					if code != term.ExitFailure || !strings.Contains(h.stdout(), "FAIL") {
						t.Fatalf("a rule query that fails is a FAIL fact, but exit %d, stdout %q", code, h.stdout())
					}
				case resultProbes.MatchString(call):
					if code != term.ExitOK {
						t.Fatalf("a failing probe is a result, but exit %d:\n%s", code, h.stderr())
					}
				case code == term.ExitOK:
					t.Fatalf("%s went on and succeeded although %q failed", c.name, call)
				case h.stderr() == "":
					t.Fatalf("%s exit %d without saying why %q failed", c.name, code, call)
				}
			})
		}
	}
}
