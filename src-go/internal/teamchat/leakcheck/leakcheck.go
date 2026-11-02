// Package leakcheck fails a test, or a whole test binary, that leaves a goroutine running.
//
// A goroutine left running after the code that started it has ended is a leak the code's own
// tests cannot see: it holds what it references (a socket, a channel send) and the next test
// runs beside it. project_goroutine_end judges production code by reading it; this package is
// the run-time half, for the goroutines a test proves end.
//
// A package's tests declare
//
//	func TestMain(m *testing.M) { os.Exit(leakcheck.Tests(m).Run()) }
//
// and a test that proves one piece of code ends its goroutines calls Check first.
package leakcheck

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
)

// Runner is what Tests needs of testing.M.
type Runner interface{ Run() int }

// settle is how long a goroutine is given to end after its test has: tries readings, pause
// apart on clock. A goroutine still ending (its owner's teardown has stopped it, the scheduler
// has not run it yet) is not a leak.
type settle struct {
	tries int
	pause time.Duration
	clock clock.Clock
}

// standard is five seconds of the system clock, which a loaded host under the race detector
// needs.
var standard = settle{tries: 500, pause: 10 * time.Millisecond, clock: clock.System()}

// Tests is m whose Run is 1 when the tests passed and a goroutine they started is still
// running once they end; the goroutines are written to stderr.
func Tests(m Runner) Runner { return tests{m: m, out: os.Stderr, settle: standard} }

type tests struct {
	m      Runner
	out    io.Writer
	settle settle
}

func (t tests) Run() int {
	before := ids()
	code := t.m.Run()
	if code != 0 {
		return code
	}
	left := t.settle.leftSince(before)
	if len(left) == 0 {
		return 0
	}
	// The run fails either way; a report that cannot be written has nowhere else to go.
	if _, err := fmt.Fprintf(t.out, "leakcheck: %d goroutine(s) still running after the tests ended:\n\n%s\n", len(left), stacks(left)); err != nil {
		return 1
	}
	return 1
}

// Check fails tb when a goroutine started during it is still running once it ends. Call it
// first, and once a test, so it judges after the test's other cleanups have stopped what they
// own; a second Check judges before the cleanups registered after the first (the rule
// project_leakcheck_once).
func Check(tb testing.TB) {
	tb.Helper()
	check(tb, standard)
}

func check(tb testing.TB, s settle) {
	tb.Helper()
	before := ids()
	tb.Cleanup(func() {
		if left := s.leftSince(before); len(left) > 0 {
			tb.Errorf("leakcheck: %d goroutine(s) started by the test still running:\n\n%s", len(left), stacks(left))
		}
	})
}

// goroutine is one goroutine of a stack dump: its id and its stack.
type goroutine struct {
	id    int
	stack string
}

// leftSince is the goroutines not in before, other than the caller, once they have had s to end.
func (s settle) leftSince(before map[int]bool) []goroutine {
	var left []goroutine
	for i := range s.tries {
		if i > 0 {
			if s.clock.Sleep(context.Background(), s.pause) != nil {
				return left // the last reading's goroutines, unsettled: still reported
			}
		}
		left = left[:0]
		for _, g := range all()[1:] {
			if !before[g.id] {
				left = append(left, g)
			}
		}
		if len(left) == 0 {
			return nil
		}
	}
	return left
}

// ids is the set of running goroutines' ids.
func ids() map[int]bool {
	set := map[int]bool{}
	for _, g := range all() {
		set[g.id] = true
	}
	return set
}

// all is every goroutine, the caller's first.
func all() []goroutine {
	buf := make([]byte, 1<<16)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return parse(string(buf[:n]))
		}
		buf = make([]byte, 2*len(buf))
	}
}

// parse splits a runtime.Stack dump into its goroutines; a block with no readable id is not one.
func parse(dump string) []goroutine {
	var out []goroutine
	for block := range strings.SplitSeq(strings.TrimSpace(dump), "\n\n") {
		head := strings.TrimPrefix(block, "goroutine ")
		if i := strings.IndexByte(head, ' '); i >= 0 {
			head = head[:i]
		}
		id, err := strconv.Atoi(head)
		if err != nil {
			continue
		}
		out = append(out, goroutine{id: id, stack: block})
	}
	return out
}

func stacks(gs []goroutine) string {
	parts := make([]string, len(gs))
	for i, g := range gs {
		parts[i] = g.stack
	}
	return strings.Join(parts, "\n\n")
}
