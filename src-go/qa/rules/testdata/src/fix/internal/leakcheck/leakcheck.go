// Package leakcheck is a stub of internal/teamchat/leakcheck: Tests wraps a TestMain's run.
package leakcheck

import "testing"

// Runner is what Tests needs of testing.M.
type Runner interface{ Run() int }

// Tests is m, with a leak check after it.
func Tests(m *testing.M) Runner { return m }
