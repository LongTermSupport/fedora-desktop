package term

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// On a terminal a secret is written only between entering the alternate screen and leaving it,
// so the normal screen, and its scrollback, never receive it (issue 255).
func TestSecretScreenShowsTheSecretOnlyOnTheAlternateScreen(t *testing.T) {
	var out bytes.Buffer
	s := &SecretScreen{W: &out, Terminal: true}
	s.Show("Password hunter2\n")
	s.Show("again\n")
	if !s.Showing() {
		t.Fatal("the screen is open until Close")
	}
	s.Close()
	want := AltScreenOn + "Password hunter2\nagain\n" + AltScreenOff
	if out.String() != want || s.Err() != nil {
		t.Fatalf("wrote %q (err %v), want %q", out.String(), s.Err(), want)
	}
	if s.Showing() {
		t.Fatal("closed")
	}
	before, after, found := strings.Cut(out.String(), "hunter2")
	if !found || !strings.Contains(before, AltScreenOn) || !strings.Contains(after, AltScreenOff) {
		t.Fatalf("the secret is not inside the alternate screen: %q", out.String())
	}
}

func TestSecretScreenOpensOnceAndCloseWithoutOpenWritesNothing(t *testing.T) {
	var out bytes.Buffer
	s := &SecretScreen{W: &out, Terminal: true}
	s.Close()
	if out.Len() != 0 {
		t.Fatalf("a screen never opened wrote %q", out.String())
	}
	s.Open()
	s.Open()
	n, err := s.Write([]byte("child output\n"))
	if err != nil || n != len("child output\n") {
		t.Fatalf("write %d %v", n, err)
	}
	s.Close()
	s.Close()
	if want := AltScreenOn + "child output\n" + AltScreenOff; out.String() != want {
		t.Fatalf("wrote %q, want %q", out.String(), want)
	}
}

// A stream that is not a terminal has no screen to leave: the secret is the payload its reader
// asked for, written plainly, with no escape codes.
func TestSecretScreenOnAPipeWritesThePayloadPlainly(t *testing.T) {
	var out bytes.Buffer
	s := &SecretScreen{W: &out}
	s.Show("token\n")
	s.Close()
	if out.String() != "token\n" || s.Showing() {
		t.Fatalf("wrote %q", out.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestSecretScreenKeepsTheFirstWriteError(t *testing.T) {
	s := &SecretScreen{W: failingWriter{}, Terminal: true}
	s.Show("x")
	if _, err := s.Write([]byte("y")); err == nil {
		t.Fatal("Write reports the failure")
	}
	s.Close()
	if s.Err() == nil || s.Err().Error() != "closed" {
		t.Fatalf("err %v", s.Err())
	}
}
