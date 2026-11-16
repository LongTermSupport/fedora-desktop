// Package testconn bounds a test's wait on a network peer (rule project_test_conn_read).
//
// A test that reads a connection, or accepts on a listener, waits until the peer writes, closes
// or dials. When the code under test never does, which is the defect the test exists to catch,
// an unbounded wait blocks to the package's timeout with a panic that names no assertion
// (fedora-desktop#300). Tests read no wall clock (project_testclock), so a read deadline is not
// available to them; Bound is the bound instead:
//
//	c := dial(t)
//	testconn.Bound(t, c)
//	line, err := bufio.NewReader(c).ReadString('\n') // ends within Limit, or the test fails
//
// The function that waits calls Bound on what it waits on; a second Bound of the same value is
// harmless.
package testconn

import (
	"io"
	"testing"
	"time"
)

// Limit is how long a bounded wait may last. It is fixed, so no test can shorten it into a race
// with the step it asserts, and far beyond any answer a sound peer gives on a loaded host.
const Limit = time.Minute

// Bound fails tb and closes c if tb has not ended within Limit, so a read or accept waiting on c
// ends with an error instead of blocking to the package's timeout. The timer stops when tb ends.
func Bound(tb testing.TB, c io.Closer) {
	tb.Helper()
	bound(tb, c, func(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop })
}

// bound is Bound with the timer a seam: after arms f to run once d has passed and returns the
// timer's Stop, which reports whether it stopped f from running.
func bound(tb testing.TB, c io.Closer, after func(time.Duration, func()) func() bool) {
	tb.Helper()
	fired := make(chan struct{})
	stop := after(Limit, func() {
		defer close(fired)
		tb.Errorf("testconn: no answer within %v; closing the %T so the wait on it ends", Limit, c)
		if err := c.Close(); err != nil {
			tb.Errorf("testconn: closing the %T: %v", c, err)
		}
	})
	tb.Cleanup(func() {
		if !stop() {
			<-fired
		}
	})
}
