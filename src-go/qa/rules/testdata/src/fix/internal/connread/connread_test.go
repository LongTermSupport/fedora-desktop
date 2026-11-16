package connread

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"fix/internal/testconn"
)

// closed is the originating shape (fedora-desktop#300): a read that ends only when the peer
// closes, so a peer that never closes hangs the test to its timeout.
func closed(c net.Conn) bool {
	_, err := c.Read(make([]byte, 1)) // want `unbounded connection read \(project_test_conn_read\)`
	return errors.Is(err, io.EOF) || err != nil
}

func TestAccept(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c, err := ln.Accept() // want `unbounded connection read \(project_test_conn_read\)`
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !closed(c) {
		t.Fatal("open")
	}
}

// The ways round: a concrete conn type, a read through the standard wrappers, and a reader
// built on the conn.
func TestWrappers(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	var tcp *net.TCPConn
	_, _ = tcp.Read(nil)          // want `unbounded connection read`
	_, _ = io.ReadAll(a)          // want `unbounded connection read`
	_, _ = io.ReadFull(a, nil)    // want `unbounded connection read`
	_, _ = io.Copy(io.Discard, b) // want `unbounded connection read`
	r := bufio.NewReader(a)       // want `unbounded connection read`
	_, _ = r.ReadString('\n')
	s := bufio.NewScanner(b) // want `unbounded connection read`
	s.Scan()
	var v any
	_ = json.NewDecoder(a).Decode(&v) // want `unbounded connection read`
}

// Clean: a deadline set first, on the conn or the listener; a close armed by a timer; the read
// in a goroutine the test waits for with a timer; a write; the conn handed to a function that
// takes a conn (its own reads are judged where they are).
func TestBounded(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	if err := a.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	_, _ = a.Read(make([]byte, 1))
	_, _ = io.ReadAll(a)
	stop := time.AfterFunc(time.Second, func() { _ = b.Close() })
	defer stop.Stop()
	_, _ = b.Read(make([]byte, 1))
	ln, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if err := ln.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	_, _ = ln.Accept()
	_, d := net.Pipe()
	testconn.Bound(t, d)
	_, _ = io.ReadAll(d)
	_, _ = d.Write([]byte("x"))
	_ = Echo(d)
}

// awaitClose is the #300 fix: the read in a goroutine, a timer case that closes the conn.
func awaitClose(t *testing.T, c net.Conn) bool {
	ended := make(chan error, 1)
	go func() {
		_, err := c.Read(make([]byte, 1))
		ended <- err
	}()
	limit := time.NewTimer(time.Second)
	defer limit.Stop()
	select {
	case err := <-ended:
		return err != nil
	case <-limit.C:
		_ = c.Close()
		<-ended
		return false
	}
}

// A goroutine's read the test then waits for with no timer hangs the test as well.
func TestAGoroutineReadAwaitedBare(t *testing.T) {
	c, _ := net.Pipe()
	done := make(chan struct{})
	go func() {
		_, _ = c.Read(make([]byte, 1)) // want `unbounded connection read`
		close(done)
	}()
	<-done
	_ = awaitClose(t, c)
}

// An accept loop the cleanup ends is bounded; an echo it then waits for is not.
func TestAnAcceptLoopTheCleanupEnds(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go io.Copy(c, c) // want `unbounded connection read`
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})
}

// A deadline set on another conn, or after the read, bounds nothing.
func TestTheWrongDeadline(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	if err := b.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	_, _ = a.Read(make([]byte, 1)) // want `unbounded connection read`
	_ = a.SetDeadline(time.Now().Add(time.Second))
}
