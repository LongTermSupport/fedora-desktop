package vmtest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// The attended-run driver (Plan 00228): a run.bash that refuses headless is driven at a
// terminal the guest's sshd allocates, its questions answered as their prompts appear.

var testPrompts = []prompt{
	{text: "Pick one: ", reply: "\n"},
	{text: "[sudo] password for ", refuse: true},
}

func newAttendant(out, keys io.Writer, stops *int) *attendant {
	return &attendant{out: out, keys: keys, prompts: testPrompts, stop: func() { *stops++ }, wake: make(chan struct{}, 1)}
}

// A line still being printed reads as a question by how it ends, blanks after it allowed;
// progress and finished lines do not.
func TestPromptShaped(t *testing.T) {
	for _, c := range []struct {
		line string
		want bool
	}{
		{":", true}, // Ansible's pause ends its multi-line prompt so
		{"Enter a colour: ", true},
		{"Continue? ", true},
		{"Proceed [y/N] ", true},
		{"Overwrite (yes/no)", true},
		{"> ", true},
		{"Downloading 45%", false},
		{"TASK [dnf] ***", false},
		{"", false},
		{"   ", false},
		{"Working on it ...", false},
		{"key: value, and more  ", false},
	} {
		if got := promptShaped([]byte(c.line)); got != c.want {
			t.Errorf("promptShaped(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

// Only output that stops at a prompt-shaped line with no answer wakes the watch: not a
// finished line, not progress, not a prompt the attendant answered or refused.
func TestAttendantWakesOnlyAtAnUnansweredPromptShape(t *testing.T) {
	for _, c := range []struct {
		output string
		want   bool
	}{
		{"Enter Prompt Colour. Must be one of:\r\n  red / blue\r\n\r\n:", true},
		{"Continue? ", true},
		{"TASK [dnf] *****\r\n", false},
		{"Downloading 45%", false},
		{"Pick one: ", false},
		{"Pick one: \r\n", false},
		{"[sudo] password for fedora: ", false},
		{"Asked: \r\nPick one: ", false},
		{"Pick one: \r\nnext question? ", true},
	} {
		var keys bytes.Buffer
		stops := 0
		a := newAttendant(io.Discard, &keys, &stops)
		if _, err := a.Write([]byte(c.output)); err != nil {
			t.Fatal(err)
		}
		if got := len(a.wake) == 1; got != c.want {
			t.Errorf("after %q the watch woke: %v, want %v", c.output, got, c.want)
		}
	}
}

// heldSleep is a sleep the test ends: each call announces itself on asleep and returns when
// the test sends on release, or with its context.
type heldSleep struct {
	asleep  chan time.Duration
	release chan struct{}
}

func newHeldSleep() heldSleep {
	return heldSleep{asleep: make(chan time.Duration), release: make(chan struct{})}
}

func (h heldSleep) sleep(ctx context.Context, d time.Duration) error {
	select {
	case h.asleep <- d:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-h.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// begun waits for the watch to start its sleep, which must be promptIdle.
func (h heldSleep) begun(t *testing.T) {
	t.Helper()
	select {
	case d := <-h.asleep:
		if d != promptIdle {
			t.Fatalf("the watch slept %s, not promptIdle", d)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the watch never began to wait")
	}
}

// watching starts the watch on a, and returns the function that ends it and waits for it.
func watching(t *testing.T, a *attendant, h heldSleep) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.watch(ctx, h.sleep)
		close(done)
	}()
	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("the watch did not end with its context")
		}
	}
}

func (a *attendant) halted() halt {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.halt
}

// A run that stops at a question no prompt answers is ended once it has printed nothing for
// promptIdle, and the end of its output is what it is reported by.
func TestAttendantEndsARunStoppedAtAnUnknownQuestion(t *testing.T) {
	var keys bytes.Buffer
	stops := 0
	a := newAttendant(io.Discard, &keys, &stops)
	h := newHeldSleep()
	end := watching(t, a, h)
	if _, err := a.Write([]byte("TASK [colour]\r\nEnter a colour:\r\n  red / blue\r\n\r\n:")); err != nil {
		t.Fatal(err)
	}
	h.begun(t)
	h.release <- struct{}{}
	end()
	if stops != 1 || a.halted().unanswered != "Enter a colour:\n  red / blue\n:" || keys.Len() != 0 {
		t.Fatalf("stops %d unanswered %q typed %q", stops, a.halted().unanswered, keys.String())
	}
}

// Output during the wait is the run still working: a finished line ends the wait, and a new
// prompt-shaped line waits the bound again from the start.
func TestAttendantWaitsAgainWhenOutputFollows(t *testing.T) {
	stops := 0
	a := newAttendant(io.Discard, io.Discard, &stops)
	h := newHeldSleep()
	end := watching(t, a, h)
	if _, err := a.Write([]byte("Building: ")); err != nil {
		t.Fatal(err)
	}
	h.begun(t)
	if _, err := a.Write([]byte("done\r\nSecond question? ")); err != nil {
		t.Fatal(err)
	}
	h.release <- struct{}{}
	h.begun(t)
	if stops != 0 {
		t.Fatal("a run that printed during the wait was ended")
	}
	h.release <- struct{}{}
	end()
	if stops != 1 || a.halted().unanswered != "Building: done\nSecond question?" {
		t.Fatalf("stops %d unanswered %q", stops, a.halted().unanswered)
	}
}

// A long silent step after a finished line is not a question: the watch never waits on it.
func TestAttendantLetsASilentStepRun(t *testing.T) {
	stops := 0
	a := newAttendant(io.Discard, io.Discard, &stops)
	h := newHeldSleep()
	end := watching(t, a, h)
	if _, err := a.Write([]byte("TASK [dnf install] ****\r\n")); err != nil {
		t.Fatal(err)
	}
	end()
	if stops != 0 || len(h.asleep) != 0 || a.halted().ended() {
		t.Fatalf("stops %d halt %+v", stops, a.halted())
	}
}

// A run the attendant already ended for a refused prompt is not ended again, nor reported as
// an unanswered one.
func TestAttendantWatchLeavesARefusedRun(t *testing.T) {
	stops := 0
	a := newAttendant(io.Discard, io.Discard, &stops)
	h := newHeldSleep()
	end := watching(t, a, h)
	if _, err := a.Write([]byte("Which one? ")); err != nil {
		t.Fatal(err)
	}
	h.begun(t)
	if _, err := a.Write([]byte("\r\n[sudo] password for fedora: ")); err != nil {
		t.Fatal(err)
	}
	h.release <- struct{}{}
	end()
	if stops != 1 || a.halted().refused != "[sudo] password for " || a.halted().unanswered != "" {
		t.Fatalf("stops %d halt %+v", stops, a.halted())
	}
}

// A prompt is answered once each time it appears, and the terminal's carriage returns are
// dropped from what the transcript gets.
func TestAttendantAnswersEachPrompt(t *testing.T) {
	var out, keys bytes.Buffer
	stops := 0
	a := newAttendant(&out, &keys, &stops)
	for _, chunk := range []string{"step 1\r\n", "Pick one: ", "\r\nstep 2\r\nPick one: \r\n"} {
		n, err := a.Write([]byte(chunk))
		if err != nil || n != len(chunk) {
			t.Fatalf("Write(%q) = %d, %v", chunk, n, err)
		}
	}
	if keys.String() != "\n\n" {
		t.Fatalf("typed %q, want two Enters", keys.String())
	}
	if out.String() != "step 1\nPick one: \nstep 2\nPick one: \n" {
		t.Fatalf("transcript %q", out.String())
	}
	if stops != 0 || a.halt.ended() {
		t.Fatal("an answerable prompt ended the run")
	}
}

// A prompt split across two writes is still seen, and seen once.
func TestAttendantSeesASplitPrompt(t *testing.T) {
	var out, keys bytes.Buffer
	stops := 0
	a := newAttendant(&out, &keys, &stops)
	for _, chunk := range []string{"Pick o", "ne: ", "more output"} {
		if _, err := a.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if keys.String() != "\n" {
		t.Fatalf("typed %q", keys.String())
	}
}

// Output that only resembles a prompt is not answered: the whole prompt text must appear.
func TestAttendantAnswersOnlyWholePrompts(t *testing.T) {
	var out, keys bytes.Buffer
	stops := 0
	a := newAttendant(&out, &keys, &stops)
	if _, err := a.Write([]byte("Pick one of these later\nPick one:\n")); err != nil {
		t.Fatal(err)
	}
	if keys.Len() != 0 {
		t.Fatalf("typed %q for no prompt", keys.String())
	}
}

// A question the run must never ask ends it at once, typing nothing, and says which.
func TestAttendantStopsOnARefusedPrompt(t *testing.T) {
	var out, keys bytes.Buffer
	stops := 0
	a := newAttendant(&out, &keys, &stops)
	if _, err := a.Write([]byte("[sudo] password for fedora: Pick one: ")); err != nil {
		t.Fatal(err)
	}
	if stops != 1 || a.halt.refused != "[sudo] password for " || keys.Len() != 0 {
		t.Fatalf("stops %d refused %q typed %q", stops, a.halt.refused, keys.String())
	}
	if _, err := a.Write([]byte("Pick one: ")); err != nil {
		t.Fatal(err)
	}
	if keys.Len() != 0 || stops != 1 {
		t.Fatal("a run already refused was answered or stopped again")
	}
}

// A transcript that cannot be written is the write's error, not a silent drop; so is an
// answer that cannot be typed.
func TestAttendantReportsWhatItCannotWrite(t *testing.T) {
	var keys bytes.Buffer
	stops := 0
	if _, err := newAttendant(failingWriter{}, &keys, &stops).Write([]byte("x")); err == nil {
		t.Fatal("an unwritable transcript was accepted")
	}
	if _, err := newAttendant(io.Discard, failingWriter{}, &stops).Write([]byte("Pick one: ")); err == nil {
		t.Fatal("an answer that could not be typed was accepted")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// attendedKit is a desktop guest whose attended commands answer as attended says.
func attendedKit(t *testing.T, attended func(remote string) execx.Result) (*kit, *running) {
	t.Helper()
	k := stepKit(t, seat0)
	k.fake.OnFunc([]string{"ssh", "-tt"}, func(c execx.Cmd) (execx.Result, error) {
		if _, ok := c.Stdin.(*os.File); !ok {
			t.Errorf("the terminal's input is %T, not a pipe the child holds itself", c.Stdin)
		}
		return attended(c.Args[len(c.Args)-1]), nil
	})
	return k, newRunning(t, k, profileDesktop)
}

// seat0 is a guest whose autologin session is up: every other command succeeds silently.
func seat0(remote string) execx.Result {
	if strings.HasPrefix(remote, "loginctl list-sessions --no-legend") {
		return execx.Result{Stdout: []byte("   2 1000 fedora  seat0 tty2\n")}
	}
	return execx.Result{}
}

func printing(out string, code int) func(string) execx.Result {
	return func(string) execx.Result { return execx.Result{Stdout: []byte(out), ExitCode: code} }
}

// The attended command runs over a forced terminal with no escape character and keepalives,
// its output (carriage returns dropped) into the writer, and its status returned.
func TestGuestAttended(t *testing.T) {
	k, r := attendedKit(t, printing("hello\r\nPick one: \r\nbye\r\n", 3))
	var w bytes.Buffer
	rc, h, err := r.g.attended(context.Background(), "the command", &w, testPrompts)
	if err != nil || rc != 3 || h.ended() {
		t.Fatalf("got %d %+v %v", rc, h, err)
	}
	line := k.mustCall("the command").Line()
	for _, want := range []string{"ssh -tt -e none -o ServerAliveInterval=30 ", "-o BatchMode=yes", " fedora@127.0.0.1 the command"} {
		if !strings.Contains(line, want) {
			t.Fatalf("%q lacks %q", line, want)
		}
	}
	if w.String() != "hello\nPick one: \nbye\n" {
		t.Fatalf("output %q", w.String())
	}
}

// A refused prompt is reported as the run's own answer, whatever status ssh then had.
func TestGuestAttendedRefused(t *testing.T) {
	_, r := attendedKit(t, printing("[sudo] password for fedora: ", 255))
	rc, h, err := r.g.attended(context.Background(), "the command", io.Discard, testPrompts)
	if err != nil || h.refused != "[sudo] password for " || h.unanswered != "" || rc != 0 {
		t.Fatalf("got %d %+v %v", rc, h, err)
	}
}

// waitingTerminal is the guest's terminal for a run that stops at a question: the attended
// command prints what the fake gives it and then waits, as a real run would, until the run is
// ended. Every other command is the fake's.
type waitingTerminal struct {
	t     *testing.T
	inner execx.Runner
}

func (w waitingTerminal) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) {
	res, err := w.inner.Run(ctx, c)
	if err != nil || len(c.Args) == 0 || c.Args[0] != "-tt" {
		return res, err
	}
	select {
	case <-ctx.Done():
		return execx.Result{ExitCode: 255}, &execx.ExitError{Name: c.Name, Code: 255}
	case <-time.After(10 * time.Second):
		w.t.Error("the run waiting at a question was never ended")
		return res, nil
	}
}

// waitingKit is attendedKit for a run that stops at the end of what out prints.
func waitingKit(t *testing.T, out string) (*kit, *running) {
	t.Helper()
	k, r := attendedKit(t, printing(out, 0))
	r.c.env.Runner = waitingTerminal{t: t, inner: r.c.env.Runner}
	return k, r
}

// A run that stops at a question no prompt answers is ended after promptIdle, on the run's
// own clock, and reported by the end of its output, not left to the provisioning limit.
func TestGuestAttendedUnansweredQuestion(t *testing.T) {
	k, r := waitingKit(t, "TASK [colour]\r\nEnter a colour:\r\n  red / blue\r\n:")
	rc, h, err := r.g.attended(context.Background(), "the command", io.Discard, testPrompts)
	if err != nil || rc != 0 || h.refused != "" || h.unanswered != "Enter a colour:\n  red / blue\n:" {
		t.Fatalf("got %d %+v %v", rc, h, err)
	}
	if !slices.Contains(k.slept, promptIdle) {
		t.Fatalf("slept %v, not promptIdle", k.slept)
	}
}
