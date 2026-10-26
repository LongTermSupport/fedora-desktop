package ttyecho

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// fakeSignals stands in for the process's signals: what the hold asked for, and the delivery.
type fakeSignals struct {
	ch      chan<- os.Signal
	asked   []os.Signal
	stopped bool
	raised  []os.Signal
}

func (s *fakeSignals) notify(c chan<- os.Signal, sigs ...os.Signal) { s.ch, s.asked = c, sigs }

func (s *fakeSignals) stop(c chan<- os.Signal) { s.stopped = c == s.ch }

func (s *fakeSignals) raise(sig os.Signal) error {
	s.raised = append(s.raised, sig)
	return nil
}

// world is a hold over a fake terminal in cooked mode, and the fakes it works through.
type world struct {
	h     *Hold
	modes *FakeModes
	sigs  *fakeSignals
}

func held(t *testing.T) world {
	t.Helper()
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	})
	w := world{sigs: &fakeSignals{}}
	w.h, w.modes = Fake(f)
	w.h.Notify, w.h.StopNotify, w.h.Raise = w.sigs.notify, w.sigs.stop, w.sigs.raise
	return w
}

// started is held with a screen shown, so the hold is taken.
func started(t *testing.T) world {
	t.Helper()
	w := held(t)
	if err := Screen(w.h); err != nil {
		t.Fatal(err)
	}
	return w
}

func TestTheFirstScreenTurnsOnlyTheEchoOffAndTheRunsEndPutsBackTheModeItFound(t *testing.T) {
	w := held(t)
	w.modes.Mode.Cc[unix.VMIN] = 1
	var echo, canonical, signals bool
	err := w.h.Held(func() {
		if err := Screen(w.h); err != nil {
			t.Error(err)
		}
		if err := Screen(w.h); err != nil {
			t.Error(err)
		}
		echo = w.modes.Echo()
		canonical, signals = w.modes.Mode.Lflag&unix.ICANON != 0, w.modes.Mode.Lflag&unix.ISIG != 0
	})
	if err != nil {
		t.Fatal(err)
	}
	if echo || !canonical || !signals {
		t.Fatalf("held: echo %v, line by line %v, signals %v", echo, canonical, signals)
	}
	if w.modes.Mode.Lflag != Cooked || w.modes.Mode.Cc[unix.VMIN] != 1 || w.modes.Sets != 2 {
		t.Fatalf("released mode %#x, want %#x; %d sets", w.modes.Mode.Lflag, Cooked, w.modes.Sets)
	}
	if err := w.h.release(); err != nil || w.modes.Sets != 2 {
		t.Fatalf("a second release sets the mode again: %v, %d sets", err, w.modes.Sets)
	}
	for _, fd := range w.modes.FDs {
		if fd != int(w.h.TTY.Fd()) {
			t.Fatalf("asked about descriptor %d, not the terminal's", fd)
		}
	}
}

// The review of round 1: a listing piped to a pager, or a job in the background, shows no
// screen, so the terminal is never touched (a pager would otherwise be put back in cooked mode
// under it, and a background job stopped by SIGTTOU).
func TestARunThatShowsNoScreenNeverTouchesTheTerminal(t *testing.T) {
	w := held(t)
	r := &recorder{modes: w.modes}
	err := w.h.Held(func() {
		for _, c := range []execx.Cmd{{Name: "less", Stdin: w.h.TTY}, {Name: "git"}} {
			if err := runOne(w.h.Runner(r), c); err != nil {
				t.Error(err)
			}
		}
		if err := w.h.Given(func() error { return nil }); err != nil {
			t.Error(err)
		}
	})
	if err != nil || w.modes.Sets != 0 || len(w.modes.FDs) != 0 || w.sigs.ch != nil {
		t.Fatalf("err %v, %d sets, %d reads, signals %v", err, w.modes.Sets, len(w.modes.FDs), w.sigs.asked)
	}
}

func TestNoTerminalNoHold(t *testing.T) {
	w := held(t)
	w.h.IsTerminal = func(any) bool { return false }
	if err := w.h.Held(func() {
		if err := Screen(w.h); err != nil {
			t.Error(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if w.modes.Sets != 0 || w.sigs.ch != nil {
		t.Fatalf("a hold on what is not a terminal set %d modes, signals %v", w.modes.Sets, w.sigs.asked)
	}
	var none *Hold
	ran := false
	if err := none.Held(func() { ran = true }); err != nil || !ran {
		t.Fatalf("err %v, ran %v", err, ran)
	}
	r := &recorder{}
	if none.Runner(r) != r {
		t.Fatal("no hold wraps the runner")
	}
}

func TestATerminalWhoseModeCannotBeReadOrSetFailsTheScreen(t *testing.T) {
	w := held(t)
	w.modes.GetErr = errors.New("EIO")
	if err := Screen(w.h); err == nil || err.Error() != "hold the terminal's echo off for a-screen: read the terminal's mode: EIO" {
		t.Fatalf("err %v", err)
	}
	w = held(t)
	w.modes.SetErr = errors.New("EIO")
	if err := Screen(w.h); err == nil || !strings.Contains(err.Error(), "turn the terminal's echo off: EIO") {
		t.Fatalf("err %v", err)
	}
	if w.sigs.ch != nil {
		t.Fatal("a hold never taken watches the signals")
	}
}

func TestANonTerminalCharacterDeviceHoldsNothing(t *testing.T) {
	w := held(t)
	w.modes.GetErr = unix.ENOTTY
	if err := Screen(w.h); err != nil {
		t.Fatal(err)
	}
	if w.modes.Sets != 0 || w.sigs.ch != nil {
		t.Fatalf("%d sets, signals %v", w.modes.Sets, w.sigs.asked)
	}
}

// A panic in the run puts the mode back before it goes on.
func TestAPanicInTheRunPutsTheModeBackFirst(t *testing.T) {
	w := held(t)
	var got any
	func() {
		defer func() { got = recover() }()
		if err := w.h.Held(func() {
			if err := Screen(w.h); err != nil {
				t.Error(err)
			}
			panic("boom")
		}); err != nil {
			t.Error(err)
		}
	}()
	if got != "boom" || !w.modes.Echo() {
		t.Fatalf("panic %v, echo after %v", got, w.modes.Echo())
	}
}

// recorder is a runner that notes whether the terminal echoed while each child ran.
type recorder struct {
	modes *FakeModes
	names []string
	echo  []bool
	err   error
	// inner, when set, is run from inside the next child, as a nested child.
	inner func()
}

func (r *recorder) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) {
	if ctx.Err() != nil {
		return execx.Result{}, ctx.Err()
	}
	r.names = append(r.names, c.Name)
	if r.modes != nil {
		r.echo = append(r.echo, r.modes.Echo())
	}
	if r.inner != nil {
		inner := r.inner
		r.inner = nil
		inner()
	}
	return execx.Result{}, r.err
}

// runOne runs c through run and hands back only its error.
func runOne(run execx.Runner, c execx.Cmd) error {
	res, err := run.Run(context.Background(), c)
	if err == nil && res.ExitCode != 0 {
		return fmt.Errorf("exit %d", res.ExitCode)
	}
	return err
}

func TestAChildGivenTheTerminalRunsInTheModeFoundAndTheEchoIsOffAgainAfter(t *testing.T) {
	w := started(t)
	r := &recorder{modes: w.modes, err: errors.New("exit 2")}
	if err := runOne(w.h.Runner(r), execx.Cmd{Name: "ansible-playbook", Stdin: w.h.TTY}); err == nil || err.Error() != "exit 2" {
		t.Fatalf("the child's error is not handed back: %v", err)
	}
	if len(r.echo) != 1 || !r.echo[0] || w.modes.Echo() {
		t.Fatalf("echo while it ran %v, after %v", r.echo, w.modes.Echo())
	}
}

func TestAScreenAndAChildWithoutTheTerminalRunWithTheEchoOff(t *testing.T) {
	w := started(t)
	r := &recorder{modes: w.modes}
	run := w.h.Runner(r)
	for _, c := range []execx.Cmd{
		{Name: "gum", Stdin: w.h.TTY, EchoHeld: true},
		{Name: "tput", Stdin: w.h.TTY, EchoHeld: true},
		{Name: "fzf", Stdin: strings.NewReader("rows\n"), EchoHeld: true},
		{Name: "git"},
	} {
		if err := runOne(run, c); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.echo) != 4 || r.echo[0] || r.echo[1] || r.echo[2] || r.echo[3] {
		t.Fatalf("echo while each ran: %v", r.echo)
	}
}

func TestTheEchoIsOffAgainOnlyWhenTheLastChildGivenTheTerminalEnds(t *testing.T) {
	w := started(t)
	r := &recorder{modes: w.modes}
	run := w.h.Runner(r)
	var afterInner bool
	r.inner = func() {
		if err := runOne(run, execx.Cmd{Name: "tmux", Stdin: w.h.TTY}); err != nil {
			t.Error(err)
		}
		afterInner = w.modes.Echo()
	}
	if err := runOne(run, execx.Cmd{Name: "ansible-playbook", Stdin: w.h.TTY}); err != nil {
		t.Fatal(err)
	}
	if !afterInner || w.modes.Echo() {
		t.Fatalf("echo after the inner child %v, after both %v", afterInner, w.modes.Echo())
	}
}

func TestBeforeAChildIsGivenTheTerminalAModeThatCannotBeSetIsAFailure(t *testing.T) {
	w := started(t)
	w.modes.SetErr = errors.New("EIO")
	r := &recorder{modes: w.modes}
	err := runOne(w.h.Runner(r), execx.Cmd{Name: "tmux", Stdin: w.h.TTY})
	if err == nil || !strings.Contains(err.Error(), "give the terminal's echo back for tmux: EIO") || len(r.names) != 0 {
		t.Fatalf("err %v, ran %v", err, r.names)
	}
}

func TestAfterAChildTheEchoThatCannotBeTurnedOffIsAFailure(t *testing.T) {
	w := started(t)
	r := &recorder{modes: w.modes}
	r.inner = func() { w.modes.SetErr = errors.New("EIO") }
	err := runOne(w.h.Runner(r), execx.Cmd{Name: "tmux", Stdin: w.h.TTY})
	if err == nil || !strings.Contains(err.Error(), "turn the terminal's echo off after tmux: EIO") {
		t.Fatalf("err %v", err)
	}
}

func TestTheModeThatCannotBePutBackAtTheRunsEndIsTheError(t *testing.T) {
	w := held(t)
	err := w.h.Held(func() {
		if err := Screen(w.h); err != nil {
			t.Error(err)
		}
		w.modes.SetErr = errors.New("EIO")
	})
	if err == nil || err.Error() != "put the terminal's mode back: EIO" {
		t.Fatalf("err %v", err)
	}
}

// execer records the replacement of the process and whether the terminal echoed then.
type execer struct {
	modes *FakeModes
	ran   []string
	echo  bool
	err   error
}

func (x *execer) Exec(c execx.Cmd) error {
	x.ran = append(x.ran, c.Name)
	x.echo = x.modes.Echo()
	return x.err
}

func TestExecPutsTheModeBackBeforeTheProcessIsReplaced(t *testing.T) {
	w := started(t)
	x := &execer{modes: w.modes, err: errors.New("no such file")}
	if err := w.h.Execer(x).Exec(execx.Cmd{Name: "/x/ccy"}); err == nil || !x.echo || len(x.ran) != 1 {
		t.Fatalf("err %v, echo at the exec %v, ran %v", err, x.echo, x.ran)
	}
	var none *Hold
	if none.Execer(x) != x {
		t.Fatal("no hold wraps the exec")
	}
}

func TestExecIsNotStartedWhenTheModeCannotBePutBack(t *testing.T) {
	w := started(t)
	w.modes.SetErr = errors.New("EIO")
	x := &execer{modes: w.modes}
	err := w.h.Execer(x).Exec(execx.Cmd{Name: "/x/ccy"})
	if err == nil || err.Error() != "before /x/ccy: put the terminal's mode back: EIO" || len(x.ran) != 0 {
		t.Fatalf("err %v, ran %v", err, x.ran)
	}
}

// A line read in this process (a numbered menu, a y/N) is typed with the echo on.
func TestALineReadInThisProcessIsTypedWithTheEchoOn(t *testing.T) {
	w := started(t)
	var echo bool
	err := w.h.Given(func() error {
		echo = w.modes.Echo()
		return errors.New("input ended")
	})
	if err == nil || err.Error() != "input ended" || !echo || w.modes.Echo() {
		t.Fatalf("err %v, echo while reading %v, after %v", err, echo, w.modes.Echo())
	}
	w.modes.SetErr = errors.New("EIO")
	ran := false
	err = w.h.Given(func() error {
		ran = true
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "give the terminal's echo back: EIO") || ran {
		t.Fatalf("err %v, ran %v", err, ran)
	}
	var none *Hold
	if err := none.Given(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestAfterALineReadTheEchoThatCannotBeTurnedOffIsAFailure(t *testing.T) {
	w := started(t)
	err := w.h.Given(func() error {
		w.modes.SetErr = errors.New("EIO")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "turn the terminal's echo off: EIO") {
		t.Fatalf("err %v", err)
	}
	w = started(t)
	err = w.h.Given(func() error {
		w.modes.SetErr = errors.New("EIO")
		return io.EOF
	})
	if errors.Is(err, io.EOF) || err == nil || err.Error() != "turn the terminal's echo off: EIO (after the read: EOF)" {
		t.Fatalf("an end of input is not taken for the answer when the terminal failed: %v", err)
	}
}

func TestASignalThatEndsTheProcessPutsTheModeBackFirst(t *testing.T) {
	w := started(t)
	if len(w.sigs.asked) != 2 || w.sigs.asked[0] != syscall.SIGTERM || w.sigs.asked[1] != syscall.SIGHUP {
		t.Fatalf("signals %v", w.sigs.asked)
	}
	w.sigs.ch <- syscall.SIGTERM
	<-w.h.done
	if !w.modes.Echo() || !w.sigs.stopped || len(w.sigs.raised) != 1 || w.sigs.raised[0] != syscall.SIGTERM {
		t.Fatalf("echo %v, stopped %v, raised %v", w.modes.Echo(), w.sigs.stopped, w.sigs.raised)
	}
}

// Under nohup SIGHUP is ignored; the hold leaves it so.
func TestAnIgnoredSignalIsNotWatched(t *testing.T) {
	w := held(t)
	w.h.Ignored = func(sig os.Signal) bool { return sig == syscall.SIGHUP }
	if err := Screen(w.h); err != nil {
		t.Fatal(err)
	}
	if len(w.sigs.asked) != 1 || w.sigs.asked[0] != syscall.SIGTERM {
		t.Fatalf("signals %v", w.sigs.asked)
	}
}

// Both ignored: nothing is watched (Notify with no signal would catch every one).
func TestWithBothSignalsIgnoredNothingIsWatched(t *testing.T) {
	w := held(t)
	w.h.Ignored = func(os.Signal) bool { return true }
	if err := Screen(w.h); err != nil {
		t.Fatal(err)
	}
	if w.sigs.ch != nil || w.h.done != nil {
		t.Fatalf("signals %v", w.sigs.asked)
	}
	if err := w.h.release(); err != nil || !w.modes.Echo() {
		t.Fatalf("release %v, echo %v", err, w.modes.Echo())
	}
}

func TestTheRunsEndStopsWatchingTheSignals(t *testing.T) {
	w := started(t)
	if err := w.h.release(); err != nil {
		t.Fatal(err)
	}
	<-w.h.done
	if !w.sigs.stopped || len(w.sigs.raised) != 0 {
		t.Fatalf("stopped %v, raised %v", w.sigs.stopped, w.sigs.raised)
	}
}

// Plan 00177 Task 12.3: a screen marked DrainInput discards what was typed before it, after the
// hold is taken; any other screen leaves the keys for itself to read.
func TestADrainingScreenDiscardsTheTypedKeysAndAnyOtherKeepsThem(t *testing.T) {
	w := held(t)
	run := w.h.Runner(noScreen{})
	if _, err := run.Run(t.Context(), execx.Cmd{Name: "gum", EchoHeld: true}); err != nil {
		t.Fatal(err)
	}
	if w.modes.Flushes != 0 {
		t.Fatalf("a screen that keeps the keys flushed %d times", w.modes.Flushes)
	}
	if _, err := run.Run(t.Context(), execx.Cmd{Name: "gum", EchoHeld: true, DrainInput: true}); err != nil {
		t.Fatal(err)
	}
	if w.modes.Flushes != 1 || w.modes.Echo() {
		t.Fatalf("flushes %d, echo %t", w.modes.Flushes, w.modes.Echo())
	}
}

func TestInputThatCannotBeDiscardedFailsTheScreen(t *testing.T) {
	w := held(t)
	w.modes.FlushErr = errors.New("TCFLSH: input/output error")
	f := &execx.Fake{}
	_, err := w.h.Runner(f).Run(t.Context(), execx.Cmd{Name: "gum", EchoHeld: true, DrainInput: true})
	if err == nil || len(f.Calls()) != 0 || !strings.Contains(err.Error(), "discard the keys typed before gum: TCFLSH: input/output error") {
		t.Fatalf("ran %v, err %v", f.Calls(), err)
	}
}

// On a real terminal: a line typed while held is still there for a screen that keeps it, and
// gone for a screen that drains; what is typed after the drain is read.
func TestOnARealTerminalADrainingScreenStartsOnNoKeyTypedBeforeIt(t *testing.T) {
	master, terminal := pty(t)
	h := New(terminal)
	press := func(s string) {
		if _, err := io.WriteString(master, s); err != nil {
			t.Fatal(err)
		}
	}
	err := h.Held(func() {
		if err := Screen(h); err != nil {
			t.Error(err)
			return
		}
		press("\x1b[B\n")
		// A whole line in the terminal's input: select blocks until the line discipline has it.
		fd := int(terminal.Fd())
		var readable unix.FdSet
		readable.Set(fd)
		if _, err := unix.Select(fd+1, &readable, nil, nil, nil); err != nil {
			t.Error(err)
			return
		}
		if _, err := h.Runner(noScreen{}).Run(t.Context(), execx.Cmd{Name: "gum", EchoHeld: true, DrainInput: true}); err != nil {
			t.Error(err)
			return
		}
		press("ok\n")
		if line := readTo(t, bufio.NewReader(terminal), '\n'); line != "ok\n" {
			t.Errorf("after the drain the program read %q", line)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNoTerminalNothingToDiscard(t *testing.T) {
	w := held(t)
	w.h.IsTerminal = func(any) bool { return false }
	if _, err := w.h.Runner(noScreen{}).Run(t.Context(), execx.Cmd{Name: "gum", EchoHeld: true, DrainInput: true}); err != nil {
		t.Fatal(err)
	}
	if w.modes.Flushes != 0 {
		t.Fatalf("flushed %d times with no terminal", w.modes.Flushes)
	}
}

// pty is a real pseudo-terminal: the master a person types into and reads the screen from,
// and the terminal the program holds.
func pty(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	master := os.NewFile(uintptr(fd), "/dev/ptmx")
	if err = unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	n, err := unix.IoctlGetUint32(fd, unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := errors.Join(terminal.Close(), master.Close()); err != nil {
			t.Error(err)
		}
	})
	return master, terminal
}

// readTo is what r gives up to and including the first mark.
func readTo(t *testing.T, r *bufio.Reader, mark byte) string {
	t.Helper()
	s, err := r.ReadString(mark)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// The host agent's report: Down pressed between two screens showed as ^[[B on the next one.
// Unheld, the line discipline echoes the keys; held, it does not, and the keys are still there
// for the next screen to read.
func TestOnARealTerminalKeysTypedBetweenScreensAreNotEchoedAndStillRead(t *testing.T) {
	master, terminal := pty(t)
	screen := bufio.NewReader(master)
	press := func(s string) {
		if _, err := io.WriteString(master, s); err != nil {
			t.Fatal(err)
		}
	}
	press("\x1b[BA")
	if got := readTo(t, screen, 'A'); got != "^[[BA" {
		t.Fatalf("unheld, the terminal showed %q", got)
	}
	h := New(terminal)
	err := h.Held(func() {
		if err := Screen(h); err != nil {
			t.Error(err)
			return
		}
		// The line discipline takes typed keys in on its own time; the program reading the line
		// is what shows they were taken in while held.
		press("\x1b[B\x1b[B\n")
		if line := readTo(t, bufio.NewReader(terminal), '\n'); line != "\x1b[BA\x1b[B\x1b[B\n" {
			t.Errorf("the program read %q", line)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	press("Z")
	if got := readTo(t, screen, 'Z'); got != "Z" {
		t.Fatalf("held, the terminal showed %q before the next key", got)
	}
}
