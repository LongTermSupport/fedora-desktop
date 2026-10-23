package rclonetail

import (
	"os"
	"syscall"
	"testing"
)

// D5, E15: the production OnStop calls stop on each SIGINT or SIGTERM, a second as well as the
// first, and a release (twice is harmless) ends the subscription.
func TestOnSignal(t *testing.T) {
	stopped := make(chan struct{}, 2)
	release := onSignal(func() { stopped <- struct{}{} })
	for range 2 {
		if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
		<-stopped
	}
	release()
	release()
	// Released before any signal: stop is never called.
	never := onSignal(func() { t.Error("stop called after release") })
	never()
}
