package vmtest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// The attended-run driver (Plan 00228): a run.bash that refuses headless is driven at a
// terminal the guest's sshd allocates, its questions answered as their prompts appear.

var testPrompts = []prompt{
	{text: "Pick one: ", reply: "\n"},
	{text: "[sudo] password for ", refuse: true},
}

func newAttendant(out, keys io.Writer, stops *int) *attendant {
	return &attendant{out: out, keys: keys, prompts: testPrompts, stop: func() { *stops++ }}
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
	if stops != 0 || a.refused != "" {
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
	if stops != 1 || a.refused != "[sudo] password for " || keys.Len() != 0 {
		t.Fatalf("stops %d refused %q typed %q", stops, a.refused, keys.String())
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
	rc, refused, err := r.g.attended(context.Background(), provisionLimit, "the command", &w, testPrompts)
	if err != nil || rc != 3 || refused != "" {
		t.Fatalf("got %d %q %v", rc, refused, err)
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
	rc, refused, err := r.g.attended(context.Background(), provisionLimit, "the command", io.Discard, testPrompts)
	if err != nil || refused != "[sudo] password for " || rc != 0 {
		t.Fatalf("got %d %q %v", rc, refused, err)
	}
}
