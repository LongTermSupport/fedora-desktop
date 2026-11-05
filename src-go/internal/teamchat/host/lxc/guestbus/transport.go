package guestbus

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

const (
	// maxSASLLine is a SASL line's cap, its CR LF included.
	maxSASLLine = 512
	// fixedHeader is a message's fixed part: endianness, type, flags, version, the body's
	// length, the serial and the length of the header-field array.
	fixedHeader = 16
	saslBegin   = "BEGIN\r\n"
)

// capped is the connection godbus reads through (n3-D25). Until the client's BEGIN has gone
// out it passes SASL lines of at most maxSASLLine bytes; after it, it reads each message's
// fixed header ahead and passes the message on only when its whole length is within
// MaxMessage, never reading past the message it is in. It never changes a byte. A refusal
// closes the connection, and every later Read returns it.
type capped struct {
	rw io.ReadWriteCloser

	begun atomic.Bool

	// The read side, used by one reader at a time.
	saslLine  int
	header    [fixedHeader]byte
	held      int
	remaining int64

	refusedMu sync.Mutex
	refused   error

	// The write side: the SASL line being written.
	outMu sync.Mutex
	out   []byte

	closeOnce sync.Once
	closeErr  error
}

func newCapped(rw io.ReadWriteCloser) *capped { return &capped{rw: rw} }

func (c *capped) Read(p []byte) (int, error) {
	if err := c.refusal(); err != nil {
		return 0, err
	}
	if !c.begun.Load() {
		return c.readSASL(p)
	}
	if c.held == 0 && c.remaining == 0 {
		if err := c.nextHeader(); err != nil {
			return 0, err
		}
	}
	if c.held > 0 {
		n := copy(p, c.header[fixedHeader-c.held:])
		c.held -= n
		return n, nil
	}
	n, err := c.rw.Read(p[:min(int64(len(p)), c.remaining)])
	c.remaining -= int64(n)
	return n, err
}

// Write passes p on, and notes the client's BEGIN, after which reads are messages.
func (c *capped) Write(p []byte) (int, error) {
	if !c.begun.Load() {
		c.noteSASL(p)
	}
	return c.rw.Write(p)
}

// Close closes the connection once; a second Close answers the first's error.
func (c *capped) Close() error {
	c.closeOnce.Do(func() { c.closeErr = c.rw.Close() })
	return c.closeErr
}

// readSASL passes a read on only when every line in it is within its cap.
func (c *capped) readSASL(p []byte) (int, error) {
	n, err := c.rw.Read(p)
	for _, b := range p[:n] {
		c.saslLine++
		if c.saslLine > maxSASLLine {
			return 0, c.refuse(fmt.Errorf("%w: a SASL line over %d bytes", ErrTooLarge, maxSASLLine))
		}
		if b == '\n' {
			c.saslLine = 0
		}
	}
	return n, err
}

// nextHeader reads a message's fixed header and judges the message by it: its fields padded
// to 8, then its body.
func (c *capped) nextHeader() error {
	if _, err := io.ReadFull(c.rw, c.header[:]); err != nil {
		return err
	}
	var order binary.ByteOrder
	switch c.header[0] {
	case 'l':
		order = binary.LittleEndian
	case 'B':
		order = binary.BigEndian
	default:
		return c.refuse(fmt.Errorf("%w: a message with the endianness byte %q", ErrMalformed, c.header[0]))
	}
	if c.header[3] != 1 {
		return c.refuse(fmt.Errorf("%w: a message of protocol version %d", ErrMalformed, c.header[3]))
	}
	body := int64(order.Uint32(c.header[4:8]))
	fields := int64(order.Uint32(c.header[12:16]))
	total := (fixedHeader+fields+7)&^7 + body
	if total > MaxMessage {
		return c.refuse(fmt.Errorf("%w: a message of %d bytes, over %d", ErrTooLarge, total, MaxMessage))
	}
	c.held, c.remaining = fixedHeader, total-fixedHeader
	return nil
}

func (c *capped) refusal() error {
	c.refusedMu.Lock()
	defer c.refusedMu.Unlock()
	return c.refused
}

// refuse closes the connection and makes err every later Read's answer.
func (c *capped) refuse(err error) error {
	c.refusedMu.Lock()
	c.refused = err
	c.refusedMu.Unlock()
	if cerr := c.Close(); cerr != nil {
		return fmt.Errorf("%w (and closing: %w)", err, cerr)
	}
	return err
}

func (c *capped) noteSASL(p []byte) {
	c.outMu.Lock()
	defer c.outMu.Unlock()
	for _, b := range p {
		c.out = append(c.out, b)
		if b != '\n' {
			continue
		}
		if bytes.Equal(c.out, []byte(saslBegin)) {
			c.begun.Store(true)
		}
		c.out = c.out[:0]
	}
}
