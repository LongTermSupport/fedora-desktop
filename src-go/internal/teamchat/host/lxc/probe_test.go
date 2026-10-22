package lxc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestroot"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxccmd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

var _ health.Guests = Guests{}

const lockRecord = `{"v":1,"run":"` + runID + `","started_ms":1}`

// guests is the probe over the world's host, containers and pins.
func (w *world) guests() Guests {
	return Guests{Root: w.host, LXC: w.lxc, Pin: w.pin}
}

// probe is the probe of handle in t1.
func (w *world) probe(handle string) (health.GuestProbe, error) {
	w.t.Helper()
	return w.guests().Probe(context.Background(), mustTeam(w.t, "t1"), handle)
}

// wantProbe asserts the probe of firstHandle in t1.
func wantProbe(t *testing.T, w *world, want health.GuestProbe) {
	t.Helper()
	got, err := w.probe(firstHandle)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stopped != want.Stopped || got.Held != want.Held || string(got.Record) != string(want.Record) {
		t.Fatalf("probe %+v (record %q), want %+v (record %q)", got, got.Record, want, want.Record)
	}
}

func TestProbeHeldSeat(t *testing.T) {
	w := placed(t)
	holdSeat(t, w.lxc.boxes["c1"], lockRecord)
	wantProbe(t, w, health.GuestProbe{Held: true, Record: []byte(lockRecord)})
}

func TestProbeFreeSeatKeepsItsRecord(t *testing.T) {
	w := placed(t)
	release := holdSeat(t, w.lxc.boxes["c1"], lockRecord)
	release()
	wantProbe(t, w, health.GuestProbe{Record: []byte(lockRecord)})
}

func TestProbeTornRecordIsPassedUp(t *testing.T) {
	w := placed(t)
	holdSeat(t, w.lxc.boxes["c1"], `{"v":1,"ru`)
	wantProbe(t, w, health.GuestProbe{Held: true, Record: []byte(`{"v":1,"ru`)})
}

func TestProbeSeatWithNoLockYetIsFree(t *testing.T) {
	w := placed(t)
	wantProbe(t, w, health.GuestProbe{})
}

func TestProbeRefusedLockIsFreeWithNoRecord(t *testing.T) {
	for _, tc := range []struct {
		name  string
		plant func(t *testing.T, dir, lock string)
	}{
		{"a link", func(t *testing.T, dir, lock string) {
			t.Helper()
			writeTree(t, dir, map[string]string{"tmp/elsewhere": lockRecord, lock[1:]: "->/tmp/elsewhere"})
		}},
		{"a FIFO", func(t *testing.T, dir, lock string) {
			t.Helper()
			writeTree(t, dir, map[string]string{filepath.Dir(lock)[1:] + "/x": ""})
			if err := syscall.Mkfifo(filepath.Join(dir, lock), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"a directory", func(t *testing.T, dir, lock string) {
			t.Helper()
			if err := os.MkdirAll(filepath.Join(dir, lock), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"a link on the path", func(t *testing.T, dir, lock string) {
			t.Helper()
			seats := filepath.Dir(filepath.Dir(lock))
			writeTree(t, dir, map[string]string{"tmp/seats/t1/seat.lock": lockRecord, seats[1:]: "->/tmp/seats"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := placed(t)
			tc.plant(t, w.lxc.boxes["c1"].dir, seatLock(t, "t1"))
			wantProbe(t, w, health.GuestProbe{})
		})
	}
}

func TestProbeOfflineContainerIsStopped(t *testing.T) {
	for _, tc := range []struct {
		name  string
		plant func(w *world)
	}{
		{"stopped", func(w *world) { w.lxc.boxes["c1"].state = lxccmd.Stopped }},
		{"frozen", func(w *world) { w.lxc.boxes["c1"].state = lxccmd.Frozen }},
		{"stopping", func(w *world) { w.lxc.boxes["c1"].state = lxccmd.Stopping }},
		{"missing", func(w *world) { delete(w.lxc.boxes, "c1") }},
		{"no state", func(w *world) {
			w.lxc.onState = func(string) { delete(w.lxc.boxes, "c1") }
		}},
		{"stopped before the pin", func(w *world) { w.pinErr = guestroot.ErrMoved }},
		{"init gone at the pin", func(w *world) { w.pinErr = guestroot.ErrGone }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := placed(t)
			holdSeat(t, w.lxc.boxes["c1"], lockRecord)
			tc.plant(w)
			wantProbe(t, w, health.GuestProbe{Stopped: true})
		})
	}
}

func TestProbeHandleTheRecordDoesNotNameIsStopped(t *testing.T) {
	w := placed(t)
	got, err := w.probe("other.1+local.lxc")
	if err != nil || !got.Stopped || got.Held {
		t.Fatalf("probe %+v %v", got, err)
	}
	if got, err := w.guests().Probe(context.Background(), mustTeam(t, "t2"), firstHandle); err != nil || !got.Stopped {
		t.Fatalf("a team with no record: %+v %v", got, err)
	}
}

func TestProbeHandleOnlyALeftEntryHoldsIsFree(t *testing.T) {
	w := placed(t)
	holdSeat(t, w.lxc.boxes["c1"], lockRecord)
	w.setRecord(func(e *lxcstate.Entry) { e.Status = lxcstate.StatusLeft })
	w.lxc.states = 0
	wantProbe(t, w, health.GuestProbe{})
	if w.lxc.states != 0 {
		t.Fatalf("a left seat's container was asked its state %d times", w.lxc.states)
	}
}

func TestProbeEntryKeyedPerSeat(t *testing.T) {
	w := newWorld(t)
	b := w.addBox("c1")
	w.declare([]string{"t1"}, w.memberAt("t1", "c1", 1), w.memberAt("t1", "c1", 2))
	if _, err := w.apply(PhasePresent); err != nil {
		t.Fatal(err)
	}
	k := seatAt(t, "t1", 2).Key()
	l, err := paths.LXCLayout("/srv/agent1", paths.GuestRuntimeDir(k), k)
	if err != nil {
		t.Fatal(err)
	}
	writeTree(t, b.dir, map[string]string{l.Seat(mustTeam(t, "t1")).Lock()[1:]: lockRecord})
	holdFile(t, filepath.Join(b.dir, l.Seat(mustTeam(t, "t1")).Lock()))
	rec := w.record()
	for _, e := range rec.Members {
		got, err := w.probe(e.Handle)
		if err != nil {
			t.Fatal(err)
		}
		if got.Held != (e.Seat == 2) || got.Stopped {
			t.Fatalf("seat %d (%s): %+v", e.Seat, e.Handle, got)
		}
	}
}

// holdFile holds p LOCK_EX until the test ends.
func holdFile(t *testing.T, p string) {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
}

func TestProbeGuestUserGoneIsFree(t *testing.T) {
	for _, passwd := range []string{
		"root:x:0:0:root:/root:/bin/bash\n",
		"agent1:x:1001:1001::relative:/bin/bash\n",
		"agent1:x:1001:1001::/srv/agent1:/bin/bash\nagent1:x:1001:1001::/srv/agent1:/bin/bash\n",
		"not a passwd line\n",
	} {
		w := placed(t)
		holdSeat(t, w.lxc.boxes["c1"], lockRecord)
		writeTree(t, w.lxc.boxes["c1"].dir, map[string]string{"etc/passwd.new": passwd})
		if err := os.Rename(filepath.Join(w.lxc.boxes["c1"].dir, "etc/passwd.new"), filepath.Join(w.lxc.boxes["c1"].dir, "etc/passwd")); err != nil {
			t.Fatal(err)
		}
		wantProbe(t, w, health.GuestProbe{})
	}
}

func TestProbeGuestWithNoPasswdIsFree(t *testing.T) {
	w := placed(t)
	if err := os.Remove(filepath.Join(w.lxc.boxes["c1"].dir, "etc/passwd")); err != nil {
		t.Fatal(err)
	}
	wantProbe(t, w, health.GuestProbe{})
}

// faultyRoot is a pinned root whose reads fail, or block until the test's context ends.
type faultyRoot struct {
	Root

	readErr, lockErr error
	entered          chan struct{}
	release          chan struct{}
	closed           *int
}

func (r faultyRoot) ReadFile(p string, limit int64) ([]byte, guestroot.Info, bool, error) {
	if r.readErr != nil {
		return nil, guestroot.Info{}, false, r.readErr
	}
	return r.Root.ReadFile(p, limit)
}

func (r faultyRoot) ProbeLock(p string) (bool, []byte, error) {
	if r.entered != nil {
		r.entered <- struct{}{}
		<-r.release
	}
	if r.lockErr != nil {
		return false, nil, r.lockErr
	}
	return r.Root.ProbeLock(p)
}

func (r faultyRoot) Close() error {
	if r.closed != nil {
		*r.closed++
	}
	return r.Root.Close()
}

// faulty is the world's probe with each pinned root wrapped by wrap.
func (w *world) faulty(wrap func(Root) Root) Guests {
	g := w.guests()
	g.Pin = func(ctx context.Context, c string) (Root, error) {
		r, err := w.pin(ctx, c)
		if err != nil {
			return nil, err
		}
		return wrap(r), nil
	}
	return g
}

func TestProbeContainerStoppingMidProbeIsStopped(t *testing.T) {
	for _, tc := range []struct {
		name  string
		root  faultyRoot
		state lxccmd.State
	}{
		{"root gone at the passwd", faultyRoot{readErr: guestroot.ErrGone}, lxccmd.Running},
		{"pin moved at the lock", faultyRoot{lockErr: guestroot.ErrMoved}, lxccmd.Running},
		{"a read fault once stopped", faultyRoot{readErr: errFault}, lxccmd.Stopping},
		{"a lock fault once stopped", faultyRoot{lockErr: errFault}, lxccmd.Stopped},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := placed(t)
			closed := 0
			g := w.faulty(func(r Root) Root {
				fr := tc.root
				fr.Root, fr.closed = r, &closed
				w.lxc.boxes["c1"].state = tc.state
				return fr
			})
			got, err := g.Probe(context.Background(), mustTeam(t, "t1"), firstHandle)
			if err != nil || !got.Stopped || got.Held {
				t.Fatalf("probe %+v %v", got, err)
			}
			if closed != 1 {
				t.Fatalf("root closed %d times", closed)
			}
		})
	}
}

func TestProbeHostFaultsAreErrors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		plant func(w *world) Guests
	}{
		{"an unreadable record", func(w *world) Guests {
			writeTree(w.t, w.host, map[string]string{paths.LXCRecord(mustTeam(w.t, "t1"))[1:] + ".new": "{}"})
			if err := os.Rename(filepath.Join(w.host, paths.LXCRecord(mustTeam(w.t, "t1"))+".new"), filepath.Join(w.host, paths.LXCRecord(mustTeam(w.t, "t1")))); err != nil {
				w.t.Fatal(err)
			}
			return w.guests()
		}},
		{"the container list", func(w *world) Guests {
			w.lxc.listErr = errFault
			return w.guests()
		}},
		{"the pin", func(w *world) Guests {
			w.pinErr = errFault
			return w.guests()
		}},
		{"a read of a running guest", func(w *world) Guests {
			return w.faulty(func(r Root) Root { return faultyRoot{Root: r, readErr: errFault} })
		}},
		{"a lock probe of a running guest", func(w *world) Guests {
			return w.faulty(func(r Root) Root { return faultyRoot{Root: r, lockErr: errFault} })
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := placed(t)
			g := tc.plant(w)
			if got, err := g.Probe(context.Background(), mustTeam(t, "t1"), firstHandle); err == nil {
				t.Fatalf("probe %+v, want an error", got)
			}
		})
	}
}

func TestProbeIsBounded(t *testing.T) {
	w := placed(t)
	var bounded bool
	g := w.guests()
	g.Pin = func(ctx context.Context, c string) (Root, error) {
		_, bounded = ctx.Deadline()
		return w.pin(ctx, c)
	}
	if _, err := g.Probe(context.Background(), mustTeam(t, "t1"), firstHandle); err != nil || !bounded {
		t.Fatalf("bounded %v, err %v", bounded, err)
	}
	if ProbeBound.Seconds() != 5 || g.bound() != ProbeBound {
		t.Fatalf("bound %v, the default %v", ProbeBound, g.bound())
	}
}

// LP1 with one and with many hanging fakes and no cancellation: each probe ends at its own
// bound with the deadline's error, all of them hanging at once.
func TestProbesThatHangEndAtTheirOwnBound(t *testing.T) {
	for _, n := range []int{1, 4} {
		w := placed(t)
		entered, release := make(chan struct{}, n), make(chan struct{})
		t.Cleanup(func() { close(release) })
		g := w.faulty(func(r Root) Root { return faultyRoot{Root: r, entered: entered, release: release} })
		g.Bound = 20 * time.Millisecond
		errs := make(chan error, n)
		for range n {
			go func() {
				_, err := g.Probe(context.Background(), mustTeam(t, "t1"), firstHandle)
				errs <- err
			}()
		}
		for range n {
			if err := <-errs; !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("%d hanging: err %v", n, err)
			}
		}
		if len(entered) != n {
			t.Fatalf("%d of %d probes hung at once", len(entered), n)
		}
	}
}

func TestProbesThatHangEndAtTheBound(t *testing.T) {
	for _, n := range []int{1, 4} {
		w := placed(t)
		entered, release := make(chan struct{}, n), make(chan struct{})
		t.Cleanup(func() { close(release) })
		g := w.faulty(func(r Root) Root { return faultyRoot{Root: r, entered: entered, release: release} })
		ctx, cancel := context.WithCancel(context.Background())
		errs := make(chan error, n)
		for range n {
			go func() {
				_, err := g.Probe(ctx, mustTeam(t, "t1"), firstHandle)
				errs <- err
			}()
		}
		for range n {
			<-entered
		}
		cancel()
		for range n {
			if err := <-errs; !errors.Is(err, context.Canceled) {
				t.Fatalf("%d hanging: err %v", n, err)
			}
		}
	}
}
