package gates

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The corpus below is scripts/test-qa-helper-summary.bash's own, case for case: that suite drove
// the bash readers (helper_counts_summary, qa_gate_case_count, qa_gate_detail) and these are the
// Go readers that replaced them. A change to a reader that one of these cases pins is a change to
// what the stage line can claim.

const token = "tok-12345"

func countsFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "counts")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func goodCounts(tests, skipped, modules, tracked string) string {
	return "token=" + token + "\ntests=" + tests + "\nskipped=" + skipped + "\nmodules=" + modules + "\ntracked=" + tracked + "\n"
}

func TestHelperCountsAreReadExactly(t *testing.T) {
	cases := []struct{ name, content, want string }{
		{"every count is reported", goodCounts("1464", "1", "64", "64"), "Ran 1464 tests in 64 modules (64 tracked), 1 skipped"},
		{"a clean run reports zero skips rather than omitting them", goodCounts("1464", "0", "64", "64"), "Ran 1464 tests in 64 modules (64 tracked), 0 skipped"},
		{"a single test is not pluralised into a mismatch", goodCounts("1", "0", "1", "1"), "Ran 1 test in 1 module (1 tracked), 0 skipped"},
		{"a large skip count is not truncated", goodCounts("1464", "137", "64", "64"), "Ran 1464 tests in 64 modules (64 tracked), 137 skipped"},
		{"more skips than tests is reported rather than clamped", goodCounts("1", "3", "1", "1"), "Ran 1 test in 1 module (1 tracked), 3 skipped"},
		{"the keys are read by name, not by position", "token=" + token + "\nmodules=2\ntracked=2\nskipped=2\ntests=9\n", "Ran 9 tests in 2 modules (2 tracked), 2 skipped"},
		{"an untracked module shows as modules above tracked", goodCounts("1483", "1", "66", "65"), "Ran 1483 tests in 66 modules (65 tracked), 1 skipped"},
		{"a trailing blank line is not an error", goodCounts("3", "0", "1", "1") + "\n", "Ran 3 tests in 1 module (1 tracked), 0 skipped"},
		{"a file with no trailing newline is still read", "token=" + token + "\ntests=3\nskipped=1\nmodules=1\ntracked=1", "Ran 3 tests in 1 module (1 tracked), 1 skipped"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := HelperCounts(countsFile(t, c.content), token)
			if err != nil || got != c.want {
				t.Fatalf("got %q, %v; want %q", got, err, c.want)
			}
			if strings.Contains(got, "\n") {
				t.Fatalf("the summary is not one line: %q", got)
			}
		})
	}
}

func TestHelperCountsRefusesAnUnreadableFileNeverReadingItAsZero(t *testing.T) {
	cases := []struct{ name, content, mentions string }{
		{"an empty file", "", "is empty"},
		{"no tests=", "token=" + token + "\nskipped=0\nmodules=1\ntracked=1\n", "tests="},
		{"no skipped=", "token=" + token + "\ntests=3\nmodules=1\ntracked=1\n", "skipped="},
		{"no modules=", "token=" + token + "\ntests=3\nskipped=0\ntracked=1\n", "modules="},
		{"no tracked=", "token=" + token + "\ntests=3\nskipped=0\nmodules=1\n", "tracked="},
		{"an empty tests value", "token=" + token + "\ntests=\nskipped=0\nmodules=1\ntracked=1\n", "tests="},
		{"an empty skipped value", "token=" + token + "\ntests=3\nskipped=\nmodules=1\ntracked=1\n", "skipped="},
		{"a non-numeric skip count", "token=" + token + "\ntests=3\nskipped=none\nmodules=1\ntracked=1\n", "skipped="},
		{"a negative count", "token=" + token + "\ntests=3\nskipped=-1\nmodules=1\ntracked=1\n", "skipped="},
		{"a count with trailing text", "token=" + token + "\ntests=3\nskipped=1 skipped\nmodules=1\ntracked=1\n", "skipped="},
		{"an unknown key", "token=" + token + "\ntests=3\nskipped=1\nmodules=1\nerrors=2\n", "errors"},
		{"a duplicated key", "token=" + token + "\ntests=3\ntests=4\nskipped=1\nmodules=1\ntracked=1\n", "duplicate tests="},
		{"a duplicate whose first value is empty", "token=" + token + "\ntests=\ntests=5\nskipped=0\nmodules=1\ntracked=1\n", "duplicate tests="},
		{"a duplicate token whose first value is empty", "token=\ntoken=" + token + "\ntests=5\nskipped=0\nmodules=1\ntracked=1\n", "duplicate token="},
		{"a well-formed file carrying the wrong token", "token=someone-elses\ntests=3\nskipped=0\nmodules=1\ntracked=1\n", "token"},
		{"a well-formed file carrying no token at all", "tests=3\nskipped=0\nmodules=1\ntracked=1\n", "token"},
		{"unittest's human-readable output", "Ran 1464 tests in 6.153s\n\nOK (skipped=1)\n", "Ran 1464 tests in 6.153s"},
		{"a forged result line", "OK (skipped=99)\n", "OK (skipped"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := HelperCounts(countsFile(t, c.content), token)
			if err == nil {
				t.Fatalf("answered %q instead of failing", got)
			}
			if got != "" {
				t.Fatalf("returned %q while failing", got)
			}
			if !strings.Contains(err.Error(), c.mentions) {
				t.Fatalf("the diagnostic %q does not mention %q", err, c.mentions)
			}
		})
	}
}

func TestHelperCountsRefusesAMissingFileByName(t *testing.T) {
	_, err := HelperCounts(filepath.Join(t.TempDir(), "absent"), token)
	if err == nil || !strings.Contains(err.Error(), "no counts file at") {
		t.Fatalf("got %v", err)
	}
}

// An empty file says nothing wrote it; a wrong token says something else did. The remedies differ.
func TestHelperCountsDistinguishesAnUnwrittenFileFromAForeignOne(t *testing.T) {
	_, empty := HelperCounts(countsFile(t, ""), token)
	_, foreign := HelperCounts(countsFile(t, "token=not-ours\ntests=3\nskipped=0\nmodules=1\ntracked=1\n"), token)
	if empty == nil || !strings.Contains(empty.Error(), "is empty") || strings.Contains(empty.Error(), "carries token") {
		t.Fatalf("empty: %v", empty)
	}
	if foreign == nil || !strings.Contains(foreign.Error(), "carries token") || strings.Contains(foreign.Error(), "is empty") {
		t.Fatalf("foreign: %v", foreign)
	}
}

func TestCaseCountReadsTheLastCountLineAndItsLastMatch(t *testing.T) {
	cases := []struct{ name, capture, want string }{
		{"a bare summary line", "passed: 29 failed: 0", "passed: 29"},
		{"two spaces before failed", "passed: 15  failed: 0", "passed: 15"},
		{"three spaces before failed", "passed: 187   failed: 0", "passed: 187"},
		{"a count with no failed at all", "passed: 20", "passed: 20"},
		{"a line prefixed with the gate's own name", "ccy selinux-verdict: passed: 14  failed: 0", "passed: 14"},
		{"an earlier count in the capture does not join the summary", "  PASS  a case mentioning passed: 3 in its label\npassed: 29 failed: 0\n", "passed: 29"},
		{"a count line followed by other output", "passed: 18 failed: 0\nOK\nVERDICT: PASS\n", "passed: 18"},
		{"an earlier count on the same line does not win", "suite passed: 3 of them; passed: 29 failed: 0", "passed: 29"},
		{"extra whitespace after the colon", "passed:  29 failed: 0", "passed: 29"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := CaseCount(c.capture)
			if !ok || got != c.want || strings.Contains(got, "\n") {
				t.Fatalf("got %q, %v; want %q", got, ok, c.want)
			}
		})
	}
}

func TestCaseCountAnswersNoCountAsNotFoundNeverAsANumber(t *testing.T) {
	for _, capture := range []string{"PASSED (library version 1.2.0)", "", "Summary: 3 passed, 0 failed"} {
		if got, ok := CaseCount(capture); ok || got != "" {
			t.Fatalf("%q: got %q, %v", capture, got, ok)
		}
	}
}

func TestDetailReadsTheRealWordingOfAGate(t *testing.T) {
	cases := []struct{ name, capture, pattern, want string }{
		{"the nokill gate", "✓ no-kill gate: 3 container-watch file(s) clean — reporting-only confirmed", "[0-9]+ container-watch file[(]s[)] clean", "3 container-watch file(s) clean"},
		{"the planlib gate", "PASS: a case\ntest-planlib: PASSED (library version 1.2.0)\n", "PASSED [(]library version [0-9.]+[)]", "PASSED (library version 1.2.0)"},
		{"the last matching line wins", "1 files clean\n2 files clean\n", "[0-9]+ files clean", "2 files clean"},
		{"leftmost-longest within the line, as bash =~ answers", "x 12 files clean and 3 files clean", "[0-9]+ files clean", "12 files clean"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := Detail(c.capture, regexp.MustCompilePOSIX(c.pattern))
			if !ok || got != c.want {
				t.Fatalf("got %q, %v; want %q", got, ok, c.want)
			}
		})
	}
}

// A pattern that stops matching must produce something that cannot be mistaken for an answer.
func TestDetailAnswersNoMatchAsNotFound(t *testing.T) {
	re := regexp.MustCompilePOSIX("[0-9]+ call site[s]? checked")
	if got, ok := Detail("✓ no-kill gate: 3 container-watch file(s) clean", re); ok || got != "" {
		t.Fatalf("got %q, %v", got, ok)
	}
	if got, ok := Detail("", regexp.MustCompilePOSIX("anything")); ok || got != "" {
		t.Fatalf("an empty capture: got %q, %v", got, ok)
	}
}
