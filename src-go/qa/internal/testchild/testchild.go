// Package testchild is the QA module's twin of src-go/internal/testchild, which this module cannot
// import: a child's stderr read without the line "program not built with -cover", which every
// process of a test binary built with -cover and no instrumented package writes as it starts
// (issue #155, rule project_self_child_stderr in CLAUDE/GoProjectRules.md).
package testchild

import "strings"

// CoverageLine is the line the coverage runtime writes, newline included.
const CoverageLine = "program not built with -cover\n"

// Stderr is a child's stderr without any whole line that is CoverageLine; every other byte is
// kept.
func Stderr(stderr string) string {
	var b strings.Builder
	for line := range strings.SplitAfterSeq(stderr, "\n") {
		if line != CoverageLine {
			b.WriteString(line)
		}
	}
	return b.String()
}
