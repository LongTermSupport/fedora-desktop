package badgo

import "testing"

// TestStartsUnowned's goroutine is judged like any other: no TestMain here runs a leak check.
func TestStartsUnowned(t *testing.T) {
	go func() { select {} }() // want `goroutine signals no end, so no owner can wait for it`
	t.Log("started")
}
