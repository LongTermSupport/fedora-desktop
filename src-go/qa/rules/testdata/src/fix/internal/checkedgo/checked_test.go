package checkedgo

import (
	"os"
	"testing"

	"fix/internal/leakcheck"
)

func TestMain(m *testing.M) { os.Exit(leakcheck.Tests(m).Run()) }

// TestStarts starts a goroutine the binary's leak check judges.
func TestStarts(t *testing.T) {
	done := make(chan struct{})
	go func() { <-done }()
	close(done)
	t.Log("started")
}
