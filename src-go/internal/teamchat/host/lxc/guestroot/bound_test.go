package guestroot

import (
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
)

// stall is a root's bound on a fake clock, moved only once a call is in its stall.
type stall struct {
	clk     *clock.Fake
	inStall chan struct{}
}

// stalling puts r's bound on a fake clock and has rec say each time a call reaches its stall.
func stalling(r *Root, rec *recorder) stall {
	st := stall{clk: clock.NewFake(0, 0), inStall: make(chan struct{}, 64)}
	r.hold = st.clk.Hold
	rec.mu.Lock()
	rec.inStall = st.inStall
	rec.mu.Unlock()
	return st
}

// past runs call, which stalls: once it is in its stall and has armed its bound, the clock
// passes the bound, and call's error is what it answered.
func (st stall) past(t *testing.T, call func() error) error {
	t.Helper()
	errc := make(chan error, 1)
	go func() { errc <- call() }()
	<-st.inStall
	if err := st.clk.AwaitDue(t.Context(), callBound); err != nil {
		t.Fatal(err)
	}
	st.clk.Advance(callBound)
	return <-errc
}

// stalledRoot is a fixture root whose openat2 of /stalled waits until the returned func is
// called, its bound on a fake clock, and a channel of the descriptors it closes.
func stalledRoot(t *testing.T) (*Root, *recorder, stall, func(), chan int) {
	t.Helper()
	g := newTree(t)
	g.file("/stalled", "x", 0o644)
	g.file("/ok", "fine", 0o644)
	rec := newRecorder()
	r, err := openDirWith(rec, g.dir)
	if err != nil {
		t.Fatal(err)
	}
	st := stalling(r, rec)
	closes := make(chan int, 64)
	release := make(chan struct{})
	rec.mu.Lock()
	rec.stallPath, rec.stall, rec.closes = "/stalled", release, closes
	rec.mu.Unlock()
	return r, rec, st, func() { close(release) }, closes
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

// B5: a read below a stalled mount ends at its bound as ErrStalled, which is ErrGuest (that
// member alone) and never a TB1 refusal (R2-2), while the next read of the same root still answers; the stalled read's
// descriptors, and at Close the root's own, are released once it ends.
func TestAStalledReadEndsAtItsBound(t *testing.T) {
	r, rec, st, release, closes := stalledRoot(t)
	rootFD := r.fd
	if err := st.past(t, func() error { return readFile(r, "/stalled", 10).err }); !errors.Is(err, ErrStalled) || !errors.Is(err, ErrGuest) || errors.Is(err, ErrRefused) {
		t.Fatalf("a stalled read: %v", err)
	}
	for name, call := range map[string]func() error{
		"Stat":      func() error { return second(r.Stat("/stalled")) },
		"ProbeLock": func() error { return third(r.ProbeLock("/stalled")) },
		"ReadDir":   func() error { return third(r.ReadDir("/stalled", func(string) bool { return true }, 1)) },
		"Dial":      func() error { return second(r.DialSocket(t.Context(), "/stalled")) },
	} {
		if err := st.past(t, call); !errors.Is(err, ErrStalled) {
			t.Errorf("%s: %v", name, err)
		}
	}
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

// Every write below a stalled directory ends at the same bound as a read, as ErrStalled (that
// member alone), while a write beside it still lands; nothing it opened outlives it, and the
// root is released only once the last stalled write ends.
func TestAStalledWriteEndsAtItsBound(t *testing.T) {
	g := newTree(t)
	g.mkdir("/w/tree")
	g.file("/w/f", "x", 0o644)
	rec := newRecorder()
	r, err := openDirWith(rec, g.dir)
	if err != nil {
		t.Fatal(err)
	}
	st := stalling(r, rec)
	rootFD := r.fd
	closes := make(chan int, 64)
	stall := make(chan struct{})
	rec.mu.Lock()
	rec.stallPath, rec.stall, rec.closes = "w", stall, closes
	rec.mu.Unlock()
	w := r.Writer()
	for name, call := range map[string]func() error{
		"MkdirAll":   func() error { return second(w.MkdirAll("/w/new")) },
		"WriteFile":  func() error { return second(w.WriteFile("/w", "g", []byte("y"), 0o644)) },
		"PlaceDir":   func() error { return second(w.PlaceDir("/w", "kit", fillKit)) },
		"Symlink":    func() error { return second(w.Symlink("/w", "l", "f")) },
		"Remove":     func() error { return second(w.Remove("/w", "f")) },
		"RemoveTree": func() error { return second(w.RemoveTree("/w", "tree")) },
	} {
		if err := st.past(t, call); !errors.Is(err, ErrStalled) || !errors.Is(err, ErrGuest) || errors.Is(err, ErrRefused) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if changed, err := w.WriteFile("/", "beside", []byte("ok"), 0o644); err != nil || !changed {
		t.Fatalf("a write beside stalled ones: %v %v", changed, err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close with writes in flight: %v", err)
	}
	if !slices.ContainsFunc(rec.leaked(), func(l string) bool { return strings.HasPrefix(l, strconv.Itoa(rootFD)+" ") }) {
		t.Errorf("the root was released under a write in flight: %v", rec.leaked())
	}
	close(stall)
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
	st := stalling(r, rec)
	release := make(chan struct{})
	rec.mu.Lock()
	rec.stallPath, rec.stall = socketPath, release
	rec.mu.Unlock()
	if err := st.past(t, func() error { return second(r.DialSocket(t.Context(), socketPath)) }); !errors.Is(err, ErrStalled) {
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

// R2-1, C1: an errno from any call below the root is the guest's, whatever it is: ErrGuest, the
// errno kept. A FUSE mount a guest user made chooses the errno of every reply, so EMFILE, ENFILE
// and ENOMEM from there are no proof the host is out of descriptors or memory.
func TestAnErrnoBelowTheRootIsTheGuests(t *testing.T) {
	calls := map[string]func(*Root) error{
		"Stat":      func(r *Root) error { return second(r.Stat("/f")) },
		"ReadFile":  func(r *Root) error { return readFileErr(r, "/f") },
		"ReadDir":   func(r *Root) error { return readDirErr(r, "/d") },
		"ProbeLock": func(r *Root) error { return probeLockErr(r, "/f") },
		"WriteFile": func(r *Root) error { return second(r.Writer().WriteFile("/d", "n", []byte("x"), 0o644)) },
		"MkdirAll":  func(r *Root) error { return second(r.Writer().MkdirAll("/d/e")) },
	}
	ops := map[string][]string{
		"Stat":      {"openat2", "fstat", "close"},
		"ReadFile":  {"openat2", "fstat", "openat", "read", "close"},
		"ReadDir":   {"openat2", "getdents", "close"},
		"ProbeLock": {"openat2", "openat", "flock", "read"},
		"WriteFile": {"openat2", "fstat", "write", "fchmod", "fsync", "renameat2"},
		"MkdirAll":  {"openat2", "mkdirat"},
	}
	for name, call := range calls {
		for _, op := range ops[name] {
			for _, errno := range []unix.Errno{unix.EACCES, unix.EIO, unix.ENXIO, unix.EMFILE, unix.ENFILE, unix.ENOMEM} {
				g := newTree(t)
				g.file("/f", "x", 0o644)
				g.mkdir("/d")
				rec := newRecorder()
				r := g.root(rec)
				rec.mu.Lock()
				rec.failOp, rec.failErr = op, errno
				rec.mu.Unlock()
				err := call(r)
				rec.mu.Lock()
				rec.failOp = ""
				rec.mu.Unlock()
				if !errors.Is(err, errno) || !errors.Is(err, ErrGuest) {
					t.Errorf("%s with %s failing %v: %v", name, op, errno, err)
				}
				if errors.Is(err, ErrRefused) {
					t.Errorf("%s with %s failing %v is a TB1 refusal: %v", name, op, errno, err)
				}
			}
		}
	}
}

// A path not there is a fact, not a fault: it stays fs.ErrNotExist, never ErrGuest.
func TestAMissingPathIsNotAGuestFault(t *testing.T) {
	r := newTree(t).root(newRecorder())
	if _, err := r.Stat("/none"); !errors.Is(err, fs.ErrNotExist) || errors.Is(err, ErrGuest) {
		t.Fatalf("Stat of a missing path: %v", err)
	}
}

// R2-3: once a call through a root scoped to a memo stalls, every later call through that memo,
// a read or a write of any path, answers ErrStalled at once, with no call made below the root;
// a root scoped to another memo, and the root itself, still answer.
func TestAStallIsPaidOncePerMemo(t *testing.T) {
	r, rec, st, release, closes := stalledRoot(t)
	rootFD := r.fd
	memo := &Memo{}
	member := r.Scoped(memo)
	if err := st.past(t, func() error { return readFile(member, "/stalled", 10).err }); !errors.Is(err, ErrStalled) {
		t.Fatalf("a stalled read: %v", err)
	}
	before := rec.count()
	for name, err := range map[string]error{
		"ReadFile":  readFileErr(member, "/ok"),
		"Stat":      second(member.Stat("/ok")),
		"ReadDir":   readDirErr(member, "/"),
		"ProbeLock": probeLockErr(member, "/ok"),
		"Dial":      second(member.DialSocket(t.Context(), "/ok")),
		"WriteFile": second(member.Writer().WriteFile("/", "n", []byte("x"), 0o644)),
	} {
		if !errors.Is(err, ErrStalled) || !errors.Is(err, ErrGuest) || !strings.Contains(err.Error(), "/stalled") {
			t.Errorf("%s after a stall: %v", name, err)
		}
	}
	if n := rec.count() - before; n != 0 {
		t.Fatalf("calls after the stall made %d system calls", n)
	}
	for name, root := range map[string]*Root{"another memo": r.Scoped(&Memo{}), "the root": r} {
		if got := readFile(root, "/ok", 10); got.err != nil || string(got.data) != "fine" {
			t.Errorf("%s: %+v", name, got)
		}
	}
	if err := member.Close(); err != nil {
		t.Fatal(err)
	}
	release()
	waitClosed(t, closes, rootFD)
}

// A guest socket nothing listens on is a connect its caller reads as the bus down: never
// ErrGuest, which would make it a wrong fact.
func TestAConnectNoOneAnswersIsNotAGuestFault(t *testing.T) {
	g := newTree(t)
	g.mkdir(filepath.Dir(socketPath))
	short := t.TempDir() + "/s"
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: short, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ln.SetUnlinkOnClose(false)
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(short, g.host(socketPath)); err != nil {
		t.Fatal(err)
	}
	r := g.root(newRecorder())
	if _, err := r.DialSocket(t.Context(), socketPath); !errors.Is(err, unix.ECONNREFUSED) || errors.Is(err, ErrGuest) {
		t.Fatalf("a dial of a socket with no listener: %v", err)
	}
}

func second[T any](_ T, err error) error { return err }

func third[T, U any](_ T, _ U, err error) error { return err }
