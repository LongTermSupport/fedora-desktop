package changed

import (
	"regexp"
	"strings"
)

var passedCount = regexp.MustCompile(`passed:[[:space:]]+([0-9]+)`)

// gateCaseCount turns a passing suite's output into the text of its report line: the number
// from the last line holding "passed: <digits>", the last such match on that line, as
// "passed: N" and true. No match is ("passed", false): the caller decides, and a suite that
// exited 0 without a count has proved nothing (SPEC-qa-changed.md B29; the bash it replaced,
// scripts/lib/qa-helper-summary.bash's qa_gate_case_count, reported the bare word as a pass).
func gateCaseCount(capture string) (string, bool) {
	last := ""
	for line := range strings.SplitSeq(capture, "\n") {
		if passedCount.MatchString(line) {
			last = line
		}
	}
	matches := passedCount.FindAllStringSubmatch(last, -1)
	if len(matches) == 0 {
		return "passed", false
	}
	return "passed: " + matches[len(matches)-1][1], true
}
