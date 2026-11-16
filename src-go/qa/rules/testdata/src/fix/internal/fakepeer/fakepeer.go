// Package fakepeer is a non-test helper only tests run: its reads are judged.
package fakepeer

import (
	"net"
	"testing"
)

// Serve answers one line on each connection ln accepts.
func Serve(ln *net.UnixListener) error {
	c, err := ln.AcceptUnix() // want `unbounded connection read`
	if err != nil {
		return err
	}
	_, err = c.Read(make([]byte, 1)) // want `unbounded connection read`
	return err
}

// Server is a fake whose Start registers its Close with the test's cleanups: its accept loop is
// bounded by the test's own life.
type Server struct{ l *net.UnixListener }

// Start serves on l until the test ends.
func Start(tb testing.TB, l *net.UnixListener) *Server {
	s := &Server{l: l}
	tb.Cleanup(func() {
		if err := s.Close(); err != nil {
			tb.Errorf("close: %v", err)
		}
	})
	go s.accept()
	return s
}

// Close ends the accept loop.
func (s *Server) Close() error { return s.l.Close() }

func (s *Server) accept() {
	for {
		if _, err := s.l.AcceptUnix(); err != nil {
			return
		}
	}
}

// Unbounded is the same fake with no cleanup registered: nothing ends its accept loop.
type Unbounded struct{ l *net.UnixListener }

// StartUnbounded serves on l with no bound.
func StartUnbounded(l *net.UnixListener) *Unbounded {
	u := &Unbounded{l: l}
	go u.accept()
	return u
}

// Close ends the accept loop, if anyone calls it.
func (u *Unbounded) Close() error { return u.l.Close() }

func (u *Unbounded) accept() {
	for {
		if _, err := u.l.AcceptUnix(); err != nil { // want `unbounded connection read`
			return
		}
	}
}

// Reader closes its conn in a registered cleanup, but a read is not an accept: the test body
// that calls Next blocks before any cleanup runs.
type Reader struct{ c net.Conn }

// NewReader reads c until the test ends.
func NewReader(tb testing.TB, c net.Conn) *Reader {
	r := &Reader{c: c}
	tb.Cleanup(func() {
		if err := r.Close(); err != nil {
			tb.Errorf("close: %v", err)
		}
	})
	return r
}

// Close closes the conn.
func (r *Reader) Close() error { return r.c.Close() }

// Next reads one byte.
func (r *Reader) Next() error {
	_, err := r.c.Read(make([]byte, 1)) // want `unbounded connection read`
	return err
}
