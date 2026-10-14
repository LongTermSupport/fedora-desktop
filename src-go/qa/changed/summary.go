package changed

import (
	"regexp"
	"strings"
)

var passedCount = regexp.MustCompile(`passed:[[:space:]]+([0-9]+)`)

// gateCaseCount turns a passing suite's output into the text of its report line: the number
// from the last line holding "passed: <digits>", the last such match on that line, as
// "passed: N"; no match is the bare word "passed" (a degraded line, never a wrong number). It
// is scripts/lib/qa-helper-summary.bash's qa_gate_case_count.
func gateCaseCount(capture string) string {
	last := ""
	for line := range strings.SplitSeq(capture, "\n") {
		if passedCount.MatchString(line) {
			last = line
		}
	}
	matches := passedCount.FindAllStringSubmatch(last, -1)
	if len(matches) == 0 {
		return "passed"
	}
	return "passed: " + matches[len(matches)-1][1]
}
