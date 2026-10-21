package tmuxccy_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

// scripted answers each command from a function, so a test can change what tmux says from one
// call to the next. It honours the writers a caller hands in, as the real runner does.
type scripted struct {
	fn    func(execx.Cmd) execx.Result
	calls []execx.Cmd
}

func (s *scripted) Run(_ context.Context, c execx.Cmd) (execx.Result, error) {
	s.calls = append(s.calls, c)
	res := s.fn(c)
	if c.Stdout != nil {
		if _, err := c.Stdout.Write(res.Stdout); err != nil {
			return res, err
		}
	}
	if c.Stderr != nil {
		if _, err := c.Stderr.Write(res.Stderr); err != nil {
			return res, err
		}
	}
	if res.ExitCode != 0 {
		return res, &execx.ExitError{Name: c.Name, Code: res.ExitCode, Stderr: string(res.Stderr)}
	}
	return res, nil
}

func (s *scripted) lines() []string {
	out := make([]string, 0, len(s.calls))
	for _, c := range s.calls {
		out = append(out, c.Line())
	}
	return out
}

func (s *scripted) count(sub string) int {
	n := 0
	for _, l := range s.lines() {
		if strings.Contains(l, sub) {
			n++
		}
	}
	return n
}

func ok(stdout string) execx.Result { return execx.Result{Stdout: []byte(stdout)} }

const now = 1_000_000

// rig is a Tmux with a fixed clock and a sleep that only counts, over a scripted runner.
type rig struct {
	tm    *tmuxccy.Tmux
	out   *bytes.Buffer
	slept int
}

func newRig(r execx.Runner) *rig {
	tm, buf := newTerm(false)
	g := &rig{out: buf}
	g.tm = &tmuxccy.Tmux{
		Runner: r, Term: tm,
		Now:   func() time.Time { return time.Unix(now, 0) },
		Sleep: func(time.Duration) { g.slept++ },
	}
	return g
}

func TestIdleWords(t *testing.T) {
	cases := map[int64]string{
		-5: "idle 0 s", 0: "idle 0 s", 59: "idle 59 s", 60: "idle 1 min", 599: "idle 9 min", 3599: "idle 59 min",
		3600: "idle 1 h", 10800: "idle 3 h", 86399: "idle 23 h", 86400: "idle 1 d", 200000: "idle 2 d",
	}
	for secs, want := range cases {
		if got := tmuxccy.IdleWords(secs); got != want {
			t.Errorf("IdleWords(%d) = %q, want %q", secs, got, want)
		}
	}
}

func TestClientWords(t *testing.T) {
	clients := []tmuxccy.Client{{TTY: "/dev/pts/3", Activity: now - 3*3600}, {TTY: "/dev/pts/4", Activity: now - 600}}
	if got := tmuxccy.ClientWords(clients, now); got != "/dev/pts/3 (idle 3 h), /dev/pts/4 (idle 10 min)" {
		t.Fatalf("got %q", got)
	}
	if got := tmuxccy.ClientWords(nil, now); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := tmuxccy.ClientWords([]tmuxccy.Client{{TTY: "/dev/pts/9", Activity: now + 50}}, now); got != "/dev/pts/9 (idle 0 s)" {
		t.Fatalf("clock moved back: %q", got)
	}
}

func TestOtherTerminals(t *testing.T) {
	g := newRig(&scripted{fn: func(execx.Cmd) execx.Result { return ok("/dev/pts/3 989200\n") }})
	got, err := g.tm.OtherTerminals(context.Background(), "alpha")
	if err != nil || got != "/dev/pts/3 (idle 3 h)" {
		t.Fatalf("got %q, %v", got, err)
	}
	g = newRig(&scripted{fn: func(execx.Cmd) execx.Result { return ok("") }})
	if got, err = g.tm.OtherTerminals(context.Background(), "alpha"); err != nil || got != "" {
		t.Fatalf("got %q, %v", got, err)
	}
	g = newRig(&scripted{fn: func(execx.Cmd) execx.Result { return fail("can't find session: alpha") }})
	if _, err = g.tm.OtherTerminals(context.Background(), "alpha"); !errors.Is(err, tmuxccy.ErrSessionGone) {
		t.Fatalf("got %v", err)
	}
}

// takeOverScript is tmux for a take-over: clientsAt answers list-clients by call number until
// the attach, after which list-clients answers afterAttach.
func takeOverScript(clientsAt func(n int) execx.Result, detach execx.Result, afterAttach string) *scripted {
	listCalls := 0
	attached := false
	s := &scripted{}
	s.fn = func(c execx.Cmd) execx.Result {
		switch {
		case strings.Contains(c.Line(), "list-clients"):
			if attached {
				return ok(afterAttach)
			}
			listCalls++
			return clientsAt(listCalls)
		case strings.Contains(c.Line(), "detach-client"):
			return detach
		case strings.Contains(c.Line(), "attach-session"):
			attached = true
			return ok("")
		case strings.Contains(c.Line(), "set-hook"):
			return ok("")
		}
		return fail("unexpected: " + c.Line())
	}
	return s
}

func TestTakeOverDetachesWaitsAndAttaches(t *testing.T) {
	// list-clients: one client; still there after the detach; gone; then Attach's own check.
	s := takeOverScript(func(n int) execx.Result {
		if n <= 2 {
			return ok("/dev/pts/3 989200\n")
		}
		return ok("")
	}, ok(""), "")
	g := newRig(s)
	outcome, err := g.tm.TakeOver(context.Background(), "alpha", 50)
	if err != nil || outcome != tmuxccy.Detached {
		t.Fatalf("got %v, %v", outcome, err)
	}
	if g.slept != 1 {
		t.Fatalf("slept %d times", g.slept)
	}
	if !strings.Contains(g.out.String(), "Detached 'alpha' from /dev/pts/3 (idle 3 h); the session keeps running.") {
		t.Fatalf("output %q", g.out.String())
	}
	if s.count("detach-client -s =alpha") != 1 || s.count("attach-session -t =alpha") != 1 {
		t.Fatalf("calls %v", s.lines())
	}
	if s.count("kill") != 0 {
		t.Fatal("a take-over never kills anything")
	}
	if want := "tmux -L ccy detach-client -s =alpha"; s.lines()[1] != want {
		t.Fatalf("second call %q, want %q", s.lines()[1], want)
	}
}

func TestTakeOverWithNoOtherTerminalJustAttaches(t *testing.T) {
	s := takeOverScript(func(int) execx.Result { return ok("") }, ok(""), "")
	g := newRig(s)
	outcome, err := g.tm.TakeOver(context.Background(), "alpha", 50)
	if err != nil || outcome != tmuxccy.Detached {
		t.Fatalf("got %v, %v", outcome, err)
	}
	if s.count("detach-client -s") != 0 || strings.Contains(g.out.String(), "Detached 'alpha' from") {
		t.Fatalf("calls %v, out %q", s.lines(), g.out.String())
	}
}

func TestTakeOverGoneAtTheStart(t *testing.T) {
	g := newRig(&scripted{fn: func(execx.Cmd) execx.Result { return fail("can't find session: alpha") }})
	outcome, err := g.tm.TakeOver(context.Background(), "alpha", 50)
	if outcome != tmuxccy.Refused || !errors.Is(err, tmuxccy.ErrSessionGone) || err.Error() != "'alpha' has ended; there is nothing to take over." {
		t.Fatalf("got %v, %v", outcome, err)
	}
}

func TestTakeOverListFailureAtTheStart(t *testing.T) {
	g := newRig(&scripted{fn: func(execx.Cmd) execx.Result { return fail("protocol version mismatch") }})
	_, err := g.tm.TakeOver(context.Background(), "alpha", 50)
	if err == nil || errors.Is(err, tmuxccy.ErrSessionGone) || !strings.Contains(err.Error(), "protocol version mismatch") {
		t.Fatalf("got %v", err)
	}
}

func TestTakeOverDetachFailsButTheClientHadLeft(t *testing.T) {
	g := newRig(takeOverScript(func(n int) execx.Result {
		if n == 1 {
			return ok("/dev/pts/3 989200\n")
		}
		return ok("")
	}, fail("no client"), ""))
	outcome, err := g.tm.TakeOver(context.Background(), "alpha", 50)
	if err != nil || outcome != tmuxccy.Detached {
		t.Fatalf("got %v, %v", outcome, err)
	}
	if !strings.Contains(g.out.String(), "The detach from /dev/pts/3 (idle 3 h) failed (no client); checking whether 'alpha' is free anyway.") {
		t.Fatalf("output %q", g.out.String())
	}
}

func TestTakeOverDetachFailsAndTheClientStays(t *testing.T) {
	s := takeOverScript(func(int) execx.Result { return ok("/dev/pts/3 989200\n") }, fail("no permission\n"), "")
	g := newRig(s)
	outcome, err := g.tm.TakeOver(context.Background(), "alpha", 50)
	if outcome != tmuxccy.Refused || err == nil || err.Error() != "could not detach 'alpha' from /dev/pts/3 (idle 3 h): no permission" {
		t.Fatalf("got %v, %v", outcome, err)
	}
	if g.slept != 0 || s.count("attach-session") != 0 {
		t.Fatalf("slept %d, calls %v", g.slept, s.lines())
	}
}

func TestTakeOverTimesOut(t *testing.T) {
	s := takeOverScript(func(int) execx.Result { return ok("/dev/pts/3 989200\n") }, ok(""), "")
	g := newRig(s)
	outcome, err := g.tm.TakeOver(context.Background(), "alpha", 30)
	want := "'alpha' was told to leave /dev/pts/3 (idle 3 h), but it is still open there after 3 s. Nothing was attached; the session keeps running"
	if outcome != tmuxccy.Refused || err == nil || err.Error() != want {
		t.Fatalf("got %v, %v", outcome, err)
	}
	if g.slept != 30 || s.count("attach-session") != 0 {
		t.Fatalf("slept %d, calls %v", g.slept, s.lines())
	}
}

// clientsThenError answers list-clients with one client the first time and then with failure.
func clientsThenError(failure string) *scripted {
	calls := 0
	return &scripted{fn: func(c execx.Cmd) execx.Result {
		if strings.Contains(c.Line(), "list-clients") {
			calls++
			if calls == 1 {
				return ok("/dev/pts/3 989200\n")
			}
			return fail(failure)
		}
		return ok("")
	}}
}

func TestTakeOverSessionEndsWhileWaiting(t *testing.T) {
	g := newRig(clientsThenError("can't find session: alpha"))
	_, err := g.tm.TakeOver(context.Background(), "alpha", 50)
	if !errors.Is(err, tmuxccy.ErrSessionGone) || err.Error() != "'alpha' ended while it was being taken over; nothing was attached." {
		t.Fatalf("got %v", err)
	}
}

func TestTakeOverListFailureWhileWaiting(t *testing.T) {
	g := newRig(clientsThenError("protocol version mismatch"))
	_, err := g.tm.TakeOver(context.Background(), "alpha", 50)
	if err == nil || errors.Is(err, tmuxccy.ErrSessionGone) {
		t.Fatalf("got %v", err)
	}
}

func TestTakeOverAttachKickedAndTakenAgain(t *testing.T) {
	g := newRig(takeOverScript(func(int) execx.Result { return ok("") }, ok(""), "/dev/pts/9 5\n"))
	outcome, err := g.tm.TakeOver(context.Background(), "alpha", 50)
	if err != nil || outcome != tmuxccy.Kicked {
		t.Fatalf("got %v, %v", outcome, err)
	}
	// Taken again just before the attach: Attach refuses it as open elsewhere.
	calls := 0
	g = newRig(&scripted{fn: func(c execx.Cmd) execx.Result {
		if strings.Contains(c.Line(), "list-clients") {
			calls++
			if calls == 1 {
				return ok("")
			}
			return ok("/dev/pts/9 5\n")
		}
		return ok("")
	}})
	if _, err = g.tm.TakeOver(context.Background(), "alpha", 50); !errors.Is(err, tmuxccy.ErrOpenElsewhere) {
		t.Fatalf("got %v", err)
	}
}

func TestTakeOverDefaultsToTheRealClockAndSleep(t *testing.T) {
	tm := &tmuxccy.Tmux{
		Runner: takeOverScript(func(int) execx.Result { return ok("") }, ok(""), ""),
		Term:   &term.Term{Err: &bytes.Buffer{}, IsTerminal: func(any) bool { return false }, Getenv: func(string) string { return "" }},
	}
	if _, err := tm.TakeOver(context.Background(), "alpha", 50); err != nil {
		t.Fatal(err)
	}
	tm.Runner = takeOverScript(func(int) execx.Result { return ok("/dev/pts/3 1\n") }, ok(""), "")
	// One tenth of a second to wait means one real 100 ms sleep: the take-over must still be
	// running when a 90 ms timer fires, and then end in the timeout.
	done := make(chan error, 1)
	go func() {
		_, err := tm.TakeOver(context.Background(), "alpha", 1)
		done <- err
	}()
	select {
	case <-done:
		t.Fatal("the real sleep did not run")
	case <-time.After(90 * time.Millisecond):
	}
	if err := <-done; err == nil {
		t.Fatal("want a timeout")
	}
}

func TestTakeOverNeedsAName(t *testing.T) {
	g := newRig(&scripted{fn: func(execx.Cmd) execx.Result { return ok("") }})
	if _, err := g.tm.TakeOver(context.Background(), "", 5); err == nil {
		t.Fatal("want error")
	}
}
