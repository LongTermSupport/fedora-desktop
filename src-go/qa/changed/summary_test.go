package changed

import "testing"

// The corpus is test-qa-helper-summary.bash's own cases for qa_gate_case_count, which this
// function replaces.
func TestGateCaseCountMatchesTheBashReader(t *testing.T) {
	cases := []struct{ name, capture, want string }{
		{"plain", "passed: 29 failed: 0", "passed: 29"},
		{"two spaces", "passed: 15  failed: 0", "passed: 15"},
		{"three spaces", "passed: 187   failed: 0", "passed: 187"},
		{"alone", "passed: 20", "passed: 20"},
		{"after a prefix", "ccy selinux-verdict: passed: 14  failed: 0", "passed: 14"},
		{"last line wins", "  PASS  a case mentioning passed: 3 in its label\npassed: 29 failed: 0\n", "passed: 29"},
		{"last match in the last line", "suite passed: 3 of them; passed: 29 failed: 0", "passed: 29"},
		{"wide gap", "passed:  29 failed: 0", "passed: 29"},
		{"summary then verdict", "passed: 18 failed: 0\nOK\nVERDICT: PASS\n", "passed: 18"},
		{"case matters", "PASSED (library version 1.2.0)", "passed"},
		{"empty", "", "passed"},
		{"no number", "passed: many", "passed"},
		{"leading zeros kept", "passed: 0073", "passed: 0073"},
	}
	for _, c := range cases {
		if got := gateCaseCount(c.capture); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}
