// Package testchild is how a test reads the stderr of a child that is its own test binary (run
// again, or copied and run as another program by the name it is given): without the one line the
// coverage runtime writes there.
//
// A test binary built with -cover whose -coverpkg names no package it links has the coverage
// hooks and no meta-data, and every process of it writes "program not built with -cover" on
// stderr as it starts, whatever it is run as and whether or not GOCOVERDIR is set. qa-changed runs
// go-qa in diff mode, whose -coverpkg is the changed packages only, so a test that requires such
// a child's stderr to be empty fails there and passes in every full run (issue #155). The rule
// project_self_child_stderr (CLAUDE/GoProjectRules.md) flags a test that compares such a stderr
// with "" as it stands.
//
// The QA module (src-go/qa) cannot import this package and has its own twin,
// qa/internal/testchild.
package testchild

import "strings"

// CoverageLine is the line the coverage runtime writes, newline included.
const CoverageLine = "program not built with -cover\n"

// Stderr is a child's stderr without any whole line that is CoverageLine; every other byte is
// kept.
func Stderr[T ~string | ~[]byte](stderr T) string {
	text := string(stderr)
	var b strings.Builder
	for line := range strings.SplitAfterSeq(text, "\n") {
		if line != CoverageLine {
			b.WriteString(line)
		}
	}
	return b.String()
}
