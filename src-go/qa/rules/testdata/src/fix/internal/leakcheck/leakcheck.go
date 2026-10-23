// Package leakcheck is a stub of internal/teamchat/leakcheck: Tests wraps a TestMain's run,
// and Check judges one test.
package leakcheck

import "testing"

// Runner is what Tests needs of testing.M.
type Runner interface{ Run() int }

// Tests is m, with a leak check after it.
func Tests(m *testing.M) Runner { return m }

// Check judges tb once its cleanups have run.
func Check(tb testing.TB) { tb.Helper() }
