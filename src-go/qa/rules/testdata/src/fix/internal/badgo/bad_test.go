package badgo

import "testing"

// A test's goroutines are the test binary's to answer for (a TestMain leak check), not this
// rule's.
func TestStartsUnowned(t *testing.T) {
	go func() { select {} }()
	t.Log("started")
}
