// Package badraw waits on descriptors through a RawConn, with callbacks that test the
// descriptor beside ones that answer without it.
package badraw

import (
	"math"
	"os"
	"syscall"
)

// Watch is the originating instance: the first call answers "not ready" untested, so a
// process that exited before the wait began, whose readiness Read has already reset, is
// never seen.
func Watch(rc syscall.RawConn) error {
	armed := false
	return rc.Read(func(uintptr) bool { // want `RawConn callback answers without testing its descriptor`
		was := armed
		armed = true
		return was
	})
}

// Blank names its descriptor blank and answers ready untested.
func Blank(rc syscall.RawConn) error {
	return rc.Read(func(_ uintptr) bool { return true }) // want `RawConn callback answers without testing its descriptor`
}

// Unused names its descriptor and never reads it.
func Unused(rc syscall.RawConn) error {
	return rc.Write(func(fd uintptr) bool { return false }) // want `RawConn callback answers without testing its descriptor`
}

// Value is a callback this package cannot see the body of.
func Value(rc syscall.RawConn, ready func(uintptr) bool) error {
	return rc.Read(ready) // want `RawConn callback answers without testing its descriptor`
}

// Ignores passes a same-package function that never reads its descriptor.
func Ignores(rc syscall.RawConn) error {
	return rc.Read(ignore) // want `RawConn callback answers without testing its descriptor`
}

func ignore(uintptr) bool { return true }

// File is judged too: any type of RawConn's shape, here an interface over a file's.
func File(f *os.File) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var ok conn = rc
	return ok.Read(func(uintptr) bool { return true }) // want `RawConn callback answers without testing its descriptor`
}

type conn interface {
	Read(ready func(uintptr) bool) error
}

// Polls tests its descriptor on every call.
func Polls(rc syscall.RawConn) error {
	return rc.Read(func(fd uintptr) bool { return fd <= math.MaxInt32 && probe(fd) })
}

// Named passes a same-package function that tests the descriptor it is handed.
func Named(rc syscall.RawConn) error {
	return rc.Read(probe)
}

// Control is no readiness wait: it runs once on the descriptor and decides nothing.
func Control(rc syscall.RawConn) error {
	return rc.Control(func(uintptr) {})
}

// Reader has a Read of another shape, which is no RawConn.
type Reader struct{}

func (Reader) Read(func(uintptr)) error { return nil }

// OtherShape calls a Read of another shape.
func OtherShape(r Reader) error {
	return r.Read(func(uintptr) {})
}

func probe(fd uintptr) bool {
	var buf [1]byte
	n, err := syscall.Read(int(fd), buf[:])
	return err != syscall.EAGAIN || n > 0
}
