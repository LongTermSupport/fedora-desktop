// Package testfiles stands for src-go's internal/teamchat/testfiles in the rule fixtures.
package testfiles

import "testing"

// Executable stands for the running test binary, open for reading.
func Executable(tb testing.TB) string {
	tb.Helper()
	return "self"
}
