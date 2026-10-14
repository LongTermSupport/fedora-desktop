package changed

import "testing"

// The corpus is test-qa-helper-summary.bash's own cases for qa_gate_case_count, which this
// function replaces.
func TestGateCaseCountMatchesTheBashReader(t *testing.T) {
	cases := []struct {
		name, capture, want string
		counted             bool
	}{
		{"plain", "passed: 29 failed: 0", "passed: 29", true},
		{"two spaces", "passed: 15  failed: 0", "passed: 15", true},
		{"three spaces", "passed: 187   failed: 0", "passed: 187", true},
		{"alone", "passed: 20", "passed: 20", true},
		{"after a prefix", "ccy selinux-verdict: passed: 14  failed: 0", "passed: 14", true},
		{"last line wins", "  PASS  a case mentioning passed: 3 in its label\npassed: 29 failed: 0\n", "passed: 29", true},
		{"last match in the last line", "suite passed: 3 of them; passed: 29 failed: 0", "passed: 29", true},
		{"wide gap", "passed:  29 failed: 0", "passed: 29", true},
		{"summary then verdict", "passed: 18 failed: 0\nOK\nVERDICT: PASS\n", "passed: 18", true},
		{"case matters", "PASSED (library version 1.2.0)", "passed", false},
		{"empty", "", "passed", false},
		{"no number", "passed: many", "passed", false},
		{"leading zeros kept", "passed: 0073", "passed: 0073", true},
	}
	for _, c := range cases {
		got, counted := gateCaseCount(c.capture)
		if got != c.want || counted != c.counted {
			t.Errorf("%s: got %q,%v want %q,%v", c.name, got, counted, c.want, c.counted)
		}
	}
}
