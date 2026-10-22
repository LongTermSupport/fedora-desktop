package guestroot

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// stalledRoot is a fixture root whose openat2 of /stalled waits until the returned func is
// called, with a short bound, and a channel of the descriptors it closes.
func stalledRoot(t *testing.T) (*Root, *recorder, func(), chan int) {
	t.Helper()
	g := newTree(t)
	g.file("/stalled", "x", 0o644)
	g.file("/ok", "fine", 0o644)
	rec := newRecorder()
	r, err := openDirWith(rec, g.dir)
	if err != nil {
		t.Fatal(err)
	}
	r.bound = 20 * time.Millisecond
	closes := make(chan int, 64)
	release := make(chan struct{})
	rec.mu.Lock()
	rec.stallPath, rec.stall, rec.closes = "/stalled", release, closes
	rec.mu.Unlock()
	return r, rec, func() { close(release) }, closes
}

// waitClosed waits until fd has been closed.
func waitClosed(t *testing.T, closes chan int, fd int) {
	t.Helper()
	for got := range closes {
		if got == fd {
			return
		}
	}
}

// B5: a read below a stalled mount ends at its bound as ErrStalled, which is ErrRefused (that
// member alone), while the next read of the same root still answers; the stalled read's
// descriptors, and at Close the root's own, are released once it ends.
func TestAStalledReadEndsAtItsBound(t *testing.T) {
	r, rec, release, closes := stalledRoot(t)
	rootFD := r.fd
	if got := readFile(r, "/stalled", 10); !errors.Is(got.err, ErrStalled) || !errors.Is(got.err, ErrRefused) {
		t.Fatalf("a stalled read: %v", got.err)
	}
	for name, err := range map[string]error{
		"Stat":      second(r.Stat("/stalled")),
		"ProbeLock": third(r.ProbeLock("/stalled")),
		"ReadDir":   third(r.ReadDir("/stalled", func(string) bool { return true }, 1)),
		"Dial":      second(r.DialSocket(t.Context(), "/stalled")),
	} {
		if !errors.Is(err, ErrStalled) {
			t.Errorf("%s: %v", name, err)
		}
	}
	r.bound = time.Minute
	if got := readFile(r, "/ok", 10); got.err != nil || string(got.data) != "fine" {
		t.Fatalf("a read beside stalled ones: %+v", got)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close with reads in flight: %v", err)
	}
	if !slices.ContainsFunc(rec.leaked(), func(l string) bool { return strings.HasPrefix(l, strconv.Itoa(rootFD)+" ") }) {
		t.Errorf("the root was released under a read in flight: %v", rec.leaked())
	}
	if _, err := r.Stat("/ok"); !errors.Is(err, errClosed) {
		t.Errorf("a read after Close: %v", err)
	}
	release()
	waitClosed(t, closes, rootFD)
	if l := rec.leaked(); len(l) != 0 {
		t.Errorf("leaked %v", l)
	}
}

// A dial that ends after its bound closes the connection it made, which no one holds.
func TestALateDialIsClosed(t *testing.T) {
	g := newTree(t)
	ln := g.socket(socketPath)
	rec := newRecorder()
	r := g.root(rec)
	r.bound = 20 * time.Millisecond
	release := make(chan struct{})
	rec.mu.Lock()
	rec.stallPath, rec.stall = socketPath, release
	rec.mu.Unlock()
	if _, err := r.DialSocket(t.Context(), socketPath); !errors.Is(err, ErrStalled) {
		t.Fatalf("a stalled dial: %v", err)
	}
	close(release)
	accepted, err := ln.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	if n, rerr := accepted.Read(make([]byte, 1)); n != 0 || rerr == nil {
		t.Errorf("the late connection was left open: %d %v", n, rerr)
	}
	if err := accepted.Close(); err != nil {
		t.Fatal(err)
	}
}

func second[T any](_ T, err error) error { return err }

func third[T, U any](_ T, _ U, err error) error { return err }
