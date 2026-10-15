package ftpcamera

import (
	"bytes"
	"errors"
	"sync"
	"testing"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestLockedWriterKeepsTheFirstFailure(t *testing.T) {
	var mu sync.Mutex
	var first error
	w := &lockedWriter{mu: &mu, w: failingWriter{}, failed: &first}
	if _, err := w.Write([]byte("a")); err == nil {
		t.Fatal("want the write error")
	}
	if first == nil {
		t.Fatal("the failure was not kept")
	}
	var buf bytes.Buffer
	ok := &lockedWriter{mu: &mu, w: &buf, failed: &first}
	s := stream{palette: colours(true), w: ok}
	s.f("%sx%s\n", s.b, s.n)
	if buf.String() != "\x1b[1mx\x1b[0m\n" {
		t.Errorf("got %q", buf.String())
	}
}

// E10: no colour, the same text.
func TestPlainPaletteIsEmpty(t *testing.T) {
	if (colours(false) != palette{}) {
		t.Error("plain palette carries codes")
	}
}
