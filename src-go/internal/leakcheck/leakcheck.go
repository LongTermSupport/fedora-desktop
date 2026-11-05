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
	"regexp"
	"runtime"
	"slices"
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

// blockedStates are the waits that only another goroutine ends, by the start of the state a
// dump names: a channel, a select, a lock, a wait group, a condition, a synctest bubble's end. A
// goroutine that is running, runnable, in a system call or the network poller, or asleep is not
// in one: it can make progress by itself.
var blockedStates = []string{"chan receive", "chan send", "select", "sync.", "semacquire", "synctest."}

// stateOf reads the state out of a dump's header line: "goroutine 7 [chan receive, 3 minutes]:".
var stateOf = regexp.MustCompile(`^goroutine \d+ \[([^\],]*)`)

// blocked is whether the goroutine is in one of blockedStates; a header that names no state is
// not read as blocked.
func (g goroutine) blocked() bool {
	m := stateOf.FindStringSubmatch(g.stack)
	if m == nil {
		return false
	}
	return slices.ContainsFunc(blockedStates, func(prefix string) bool { return strings.HasPrefix(m[1], prefix) })
}

// Idle is whether every goroutine but the caller's is blocked on a channel, a select, a lock or a
// wait group, so that nothing can happen until the caller acts: a wait on a condition that is
// false then can never end. A goroutine that is running, runnable, in a system call or the network
// poller, or asleep can still make progress, so it makes the answer false: a pending timer or an
// external event is never read as a deadlock. The dump stops the world, so it is one instant, and
// a goroutine that a send or a close has woken is already runnable in it.
func Idle() bool {
	return !slices.ContainsFunc(all()[1:], func(g goroutine) bool { return !g.blocked() })
}

// Poll yields until cond holds, and fails tb once every other goroutine is blocked (Idle) and cond
// still does not hold: nothing can change it, so the wait would otherwise spin one CPU to the
// package's timeout. cond is read again after the Idle reading, which keeps the verdict sound: no
// goroutine runs between the two but the caller's. A world with a goroutine in the network
// poller (a listener, a connection) is never Idle, so there Poll waits as a bare yield loop does.
func Poll(tb testing.TB, cond func() bool) {
	tb.Helper()
	for !cond() {
		if Idle() && !cond() {
			tb.Fatal("leakcheck: every other goroutine is blocked and the condition does not hold")
		}
		runtime.Gosched()
	}
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
