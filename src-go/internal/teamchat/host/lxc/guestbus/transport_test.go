package guestbus

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
)

// memConn is a connection whose peer's bytes are in, and whose own writes land in out.
type memConn struct {
	in     *bytes.Reader
	out    bytes.Buffer
	closed int
}

func (m *memConn) Read(p []byte) (int, error)  { return m.in.Read(p) }
func (m *memConn) Write(p []byte) (int, error) { return m.out.Write(p) }
func (m *memConn) Close() error {
	m.closed++
	return nil
}

func newMem(in []byte) (*memConn, *capped) {
	m := &memConn{in: bytes.NewReader(in)}
	return m, newCapped(m)
}

// header is a message's fixed 16 bytes.
func header(order, version byte, fields, body uint32) []byte {
	h := []byte{order, 2, 0, version}
	var bo binary.AppendByteOrder = binary.LittleEndian
	if order == 'B' {
		bo = binary.BigEndian
	}
	h = bo.AppendUint32(h, body)
	h = bo.AppendUint32(h, 7)
	return bo.AppendUint32(h, fields)
}

// message is a whole message of fields and body bytes, its fields padded to 8.
func message(order byte, fields, body uint32) []byte {
	m := header(order, 1, fields, body)
	m = append(m, make([]byte, fields)...)
	for len(m)%8 != 0 {
		m = append(m, 0)
	}
	return append(m, make([]byte, body)...)
}

func begin(t *testing.T, c *capped) {
	t.Helper()
	if _, err := c.Write([]byte("BEGIN\r\n")); err != nil {
		t.Fatal(err)
	}
}

// readAll reads c in small pieces, as a decoder does.
func readAll(c *capped, piece int) ([]byte, error) {
	var got []byte
	buf := make([]byte, piece)
	for {
		n, err := c.Read(buf)
		got = append(got, buf[:n]...)
		if err != nil {
			return got, err
		}
	}
}

func TestSASLLinesPassUnchanged(t *testing.T) {
	in := "REJECTED EXTERNAL\r\nOK 0123\r\n" + strings.Repeat("x", maxSASLLine-2) + "\r\n"
	_, c := newMem([]byte(in))
	got, err := readAll(c, 5)
	if !errors.Is(err, io.EOF) || string(got) != in {
		t.Errorf("%q %v", got, err)
	}
}

func TestAnOversizedSASLLineIsRefused(t *testing.T) {
	m, c := newMem([]byte("OK\r\n" + strings.Repeat("x", maxSASLLine+1)))
	got, err := readAll(c, 4096)
	if !errors.Is(err, ErrTooLarge) || m.closed != 1 {
		t.Fatalf("%q %v closed %d", got, err, m.closed)
	}
	if strings.Contains(string(got), "xxx") {
		t.Errorf("the long line was passed on: %q", got)
	}
	if n, err := c.Read(make([]byte, 8)); n != 0 || !errors.Is(err, ErrTooLarge) {
		t.Errorf("a later read: %d %v", n, err)
	}
}

func TestMessagesPassUnchangedAfterBegin(t *testing.T) {
	in := append(message('l', 5, 3), message('B', 16, 0)...)
	in = append(in, message('l', 0, 9)...)
	for _, piece := range []int{1, 7, 16, 4096} {
		_, c := newMem(in)
		begin(t, c)
		got, err := readAll(c, piece)
		if !errors.Is(err, io.EOF) || !bytes.Equal(got, in) {
			t.Errorf("piece %d: %q %v", piece, got, err)
		}
	}
}

// A read never runs past the message it is in, so the next header is always judged first.
func TestAReadStopsAtTheMessageEnd(t *testing.T) {
	first := message('l', 0, 8)
	_, c := newMem(append(first, header('l', 1, 0, MaxMessage)...))
	begin(t, c)
	buf := make([]byte, 4096)
	n, err := c.Read(buf)
	if err != nil || n != len(header('l', 1, 0, 0)) {
		t.Fatalf("header: %d %v", n, err)
	}
	n, err = c.Read(buf)
	if err != nil || n != len(first)-16 {
		t.Fatalf("body: %d %v", n, err)
	}
	if n, err = c.Read(buf); n != 0 || !errors.Is(err, ErrTooLarge) {
		t.Fatalf("next: %d %v", n, err)
	}
}

func TestAMessageOverItsCapIsRefusedByItsHeader(t *testing.T) {
	cases := map[string][]byte{
		"body":       header('l', 1, 0, MaxMessage),
		"fields":     header('B', 1, MaxMessage, 0),
		"padding":    header('l', 1, MaxMessage-16-7, 1),
		"uint32 sum": header('l', 1, 0xffffffff, 0xffffffff),
	}
	for name, h := range cases {
		rest := bytes.Repeat([]byte{'b'}, 64)
		m, c := newMem(append(h, rest...))
		begin(t, c)
		got, err := readAll(c, 4096)
		if !errors.Is(err, ErrTooLarge) || len(got) != 0 || m.closed != 1 {
			t.Errorf("%s: %q %v closed %d", name, got, err, m.closed)
		}
		if m.in.Len() != len(rest) {
			t.Errorf("%s: %d bytes past the header were read", name, len(rest)-m.in.Len())
		}
		if n, err := c.Read(make([]byte, 8)); n != 0 || !errors.Is(err, ErrTooLarge) {
			t.Errorf("%s: a later read: %d %v", name, n, err)
		}
	}
}

func TestAMessageAtItsCapPasses(t *testing.T) {
	// 16 header bytes, 8 field bytes (already aligned), and the body to the cap exactly.
	in := message('l', 8, MaxMessage-24)
	_, c := newMem(in)
	begin(t, c)
	got, err := readAll(c, 1<<16)
	if !errors.Is(err, io.EOF) || len(got) != MaxMessage {
		t.Errorf("%d %v", len(got), err)
	}
}

func TestAHeaderThatIsNotDBusIsMalformed(t *testing.T) {
	for name, h := range map[string][]byte{
		"endianness": header('x', 1, 0, 0),
		"version":    header('l', 2, 0, 0),
	} {
		m, c := newMem(h)
		begin(t, c)
		got, err := readAll(c, 4096)
		if !errors.Is(err, ErrMalformed) || len(got) != 0 || m.closed != 1 {
			t.Errorf("%s: %q %v closed %d", name, got, err, m.closed)
		}
	}
}

func TestATruncatedHeaderIsTheReadError(t *testing.T) {
	_, c := newMem([]byte("l\x02\x00\x01"))
	begin(t, c)
	if n, err := c.Read(make([]byte, 64)); n != 0 || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("%d %v", n, err)
	}
	_, c = newMem(nil)
	begin(t, c)
	if n, err := c.Read(make([]byte, 64)); n != 0 || !errors.Is(err, io.EOF) {
		t.Errorf("%d %v", n, err)
	}
}

// Messages are judged only once the client's own BEGIN has gone out, however it was written.
func TestBeginIsSeenAcrossWrites(t *testing.T) {
	m, c := newMem(header('l', 1, 0, MaxMessage))
	for _, w := range []string{"\x00AUTH EXTERNAL 30\r\nBE", "GI", "N\r\n"} {
		if _, err := c.Write([]byte(w)); err != nil {
			t.Fatal(err)
		}
	}
	if m.out.String() != "\x00AUTH EXTERNAL 30\r\nBEGIN\r\n" {
		t.Errorf("wrote %q", m.out.String())
	}
	if _, err := c.Read(make([]byte, 64)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("%v", err)
	}
}

func TestAnotherLineIsNotBegin(t *testing.T) {
	_, c := newMem([]byte(strings.Repeat("y", 40)))
	if _, err := c.Write([]byte("NOT BEGIN\r\nBEGINNING\r\n")); err != nil {
		t.Fatal(err)
	}
	// Still SASL: 40 bytes with no newline is a line within its cap, passed on as it is.
	got, err := readAll(c, 64)
	if !errors.Is(err, io.EOF) || len(got) != 40 {
		t.Errorf("%q %v", got, err)
	}
}

func TestCloseClosesOnce(t *testing.T) {
	m, c := newMem(nil)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil || m.closed != 1 {
		t.Errorf("%v closed %d", err, m.closed)
	}
}
