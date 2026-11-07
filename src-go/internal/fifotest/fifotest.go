// Package fifotest plants a named pipe for a test that proves code skips or refuses one,
// without the test hanging when the code regresses and opens it.
package fifotest

import (
	"os"
	"sync/atomic"
	"syscall"
	"testing"
)

// Plant makes a named pipe at path and returns a probe that says whether anything opened it
// for reading during the test. A goroutine waits in the open of the write end and closes it as
// soon as a reader arrives, so code that opens the pipe reads end of file at once: a regression
// fails the test's assertions instead of blocking for ever. The cleanup releases the goroutine.
func Plant(tb testing.TB, path string) func() bool {
	tb.Helper()
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		tb.Fatalf("mkfifo %s: %v", path, err)
	}
	var hit atomic.Bool
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		w, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			done <- err
			return
		}
		select {
		case <-release:
		default:
			hit.Store(true)
		}
		done <- w.Close()
	}()
	tb.Cleanup(func() {
		close(release)
		r, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			tb.Errorf("releasing the writer of %s: %v", path, err)
			return
		}
		if err := <-done; err != nil {
			tb.Errorf("writer of %s: %v", path, err)
		}
		if err := r.Close(); err != nil {
			tb.Errorf("closing %s: %v", path, err)
		}
	})
	return hit.Load
}
