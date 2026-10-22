package lock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/reexec"
)

var helpers = reexec.Helpers{
	// flock holds a lock on its own description of the file named by Args[0], shared or
	// exclusive by Args[1], until released or killed.
	"flock": func(h *reexec.Helper) int {
		f, err := os.OpenFile(h.Args[0], os.O_RDWR, 0)
		if err != nil {
			return 2
		}
		how := unix.LOCK_SH
		if h.Args[1] == "ex" {
			how = unix.LOCK_EX
		}
		if err := unix.Flock(int(f.Fd()), how|unix.LOCK_NB); err != nil {
			return 3
		}
		if err := h.Ready(); err != nil {
			return 4
		}
		if err := f.Close(); err != nil {
			return 5
		}
		return 0
	},
}

func TestMain(m *testing.M) { os.Exit(reexec.Main(m, helpers)) }

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// fakeClock records each sleep and runs onSleep, which may release a holder.
type fakeClock struct {
	sleeps  []time.Duration
	onSleep func()
}

func (c *fakeClock) Sleep(_ context.Context, d time.Duration) error {
	c.sleeps = append(c.sleeps, d)
	if c.onSleep != nil {
		c.onSleep()
		c.onSleep = nil
	}
	return nil
}

type world struct {
	root  *homefs.Root
	run   paths.Run
	seat  paths.Seat
	clock *fakeClock
	held  *KeeperLock
}

func newWorld(t *testing.T) *world {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	check(t, os.Mkdir(home, 0o700))
	root, err := homefs.Open(home, homefs.Private)
	check(t, err)
	t.Cleanup(func() { check(t, root.Close()) })
	layout, err := paths.HomeLayout(home, home+"/runtime")
	check(t, err)
	team, err := paths.ParseTeamID("alpha")
	check(t, err)
	runID, err := paths.ParseRunID("0123456789abcdef")
	check(t, err)
	w := &world{root: root, run: layout.Run(runID), seat: layout.Seat(team), clock: &fakeClock{}}
	check(t, root.MkdirAll(w.run.Dir()))
	check(t, root.MkdirAll(w.seat.Dir()))
	return w
}

// keeper is the world's one keeper, taken at its first use.
func (w *world) keeper(t *testing.T) *KeeperLock {
	t.Helper()
	if w.held != nil {
		return w.held
	}
	k, err := TakeKeeper(t.Context(), w.clock, w.root, w.run)
	check(t, err)
	t.Cleanup(func() {
		if err := k.Release(); err != nil && !errors.Is(err, ErrReleased) {
			t.Error(err)
		}
	})
	w.held = k
	return k
}

func inode(t *testing.T, path string) uint64 {
	t.Helper()
	var st unix.Stat_t
	check(t, unix.Lstat(path, &st))
	return st.Ino
}

func touch(t *testing.T, path string) {
	t.Helper()
	check(t, os.WriteFile(path, nil, 0o600))
}

// acquirers are the four ways to a lock, each run against a second process's hold.
func acquirers(w *world) map[string]struct {
	path string
	take func(t *testing.T) (func() error, error)
} {
	type acq = struct {
		path string
		take func(t *testing.T) (func() error, error)
	}
	return map[string]acq{
		"keeper": {w.run.KeeperLock(), func(t *testing.T) (func() error, error) {
			t.Helper()
			k, err := TakeKeeper(t.Context(), w.clock, w.root, w.run)
			if err != nil {
				return nil, err
			}
			return k.Release, nil
		}},
		"link": {w.run.LinkLock(), func(t *testing.T) (func() error, error) {
			t.Helper()
			l, err := TakeLink(t.Context(), w.clock, w.root, w.run)
			if err != nil {
				return nil, err
			}
			return l.Release, nil
		}},
		"seat": {w.seat.Lock(), func(t *testing.T) (func() error, error) {
			t.Helper()
			s, err := w.keeper(t).TakeSeat(t.Context(), w.root, w.seat)
			if err != nil {
				return nil, err
			}
			return s.Close, nil
		}},
		"hold": {w.seat.Lock(), func(t *testing.T) (func() error, error) {
			t.Helper()
			h, err := HoldSeat(t.Context(), w.clock, w.root, w.seat)
			if err != nil {
				return nil, err
			}
			return h.Release, nil
		}},
	}
}

// LK1: a probe's momentary shared lock (here held until the first wait) does not refuse an
// acquire, which tries again 50 ms later.
func TestAMomentarySharedHoldIsWaitedOut(t *testing.T) {
	for name := range acquirers(newWorld(t)) {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			a := acquirers(w)[name]
			touch(t, a.path)
			holder := reexec.Start(t, "flock", a.path, "sh")
			w.clock.onSleep = holder.Release
			release, err := a.take(t)
			check(t, err)
			if !slices.Equal(w.clock.sleeps, []time.Duration{50 * time.Millisecond}) {
				t.Errorf("sleeps %v", w.clock.sleeps)
			}
			check(t, release())
		})
	}
}

// LK1: held past all three tries is Busy, and nothing is left held.
func TestAHoldPastThreeTriesIsBusy(t *testing.T) {
	for name := range acquirers(newWorld(t)) {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			a := acquirers(w)[name]
			touch(t, a.path)
			before := inode(t, a.path)
			holder := reexec.Start(t, "flock", a.path, "sh")
			_, err := a.take(t)
			busy, ok := errors.AsType[*BusyError](err)
			if !errors.Is(err, ErrBusy) || !ok || busy.Path != a.path {
				t.Fatalf("got %v", err)
			}
			if !slices.Equal(w.clock.sleeps, []time.Duration{50 * time.Millisecond, 50 * time.Millisecond}) {
				t.Errorf("sleeps %v", w.clock.sleeps)
			}
			holder.Release()
			if inode(t, a.path) != before {
				t.Error("the lock file was replaced")
			}
			release, err := a.take(t)
			check(t, err)
			check(t, release())
		})
	}
}

// LK3: a holder ended by SIGKILL frees the lock, and the lock file stays the same inode.
func TestASIGKILLedHolderFreesTheLock(t *testing.T) {
	for name := range acquirers(newWorld(t)) {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			a := acquirers(w)[name]
			touch(t, a.path)
			before := inode(t, a.path)
			reexec.Start(t, "flock", a.path, "ex").Kill()
			release, err := a.take(t)
			check(t, err)
			if len(w.clock.sleeps) != 0 {
				t.Errorf("sleeps %v", w.clock.sleeps)
			}
			check(t, release())
			if inode(t, a.path) != before {
				t.Error("the lock file was replaced")
			}
		})
	}
}

// LK1: keeper and link are released by LOCK_UN then close, so a second take succeeds at once,
// and a second release is refused.
func TestKeeperAndLinkReleaseAndRetake(t *testing.T) {
	w := newWorld(t)
	k, err := TakeKeeper(t.Context(), w.clock, w.root, w.run)
	check(t, err)
	if _, err = TakeKeeper(t.Context(), w.clock, w.root, w.run); !errors.Is(err, ErrBusy) {
		t.Fatalf("a second keeper: %v", err)
	}
	check(t, k.Release())
	if err = k.Release(); !errors.Is(err, ErrReleased) {
		t.Errorf("released twice: %v", err)
	}
	l, err := TakeLink(t.Context(), w.clock, w.root, w.run)
	check(t, err)
	if _, err = TakeLink(t.Context(), w.clock, w.root, w.run); !errors.Is(err, ErrBusy) {
		t.Fatalf("a second link: %v", err)
	}
	check(t, l.Release())
	if err = l.Release(); !errors.Is(err, ErrReleased) {
		t.Errorf("released twice: %v", err)
	}
	k, err = TakeKeeper(t.Context(), w.clock, w.root, w.run)
	check(t, err)
	check(t, k.Release())
}

// LK2: a released keeper takes no seat.
func TestAReleasedKeeperTakesNoSeat(t *testing.T) {
	w := newWorld(t)
	k := w.keeper(t)
	check(t, k.Release())
	if _, err := k.TakeSeat(t.Context(), w.root, w.seat); !errors.Is(err, ErrReleased) {
		t.Fatalf("got %v", err)
	}
}

// LK2, B 2.5: the seat lock's description is shared with the link and released by close only,
// so the seat stays held until both copies are closed.
func TestTheSeatStaysHeldUntilEveryCopyIsClosed(t *testing.T) {
	w := newWorld(t)
	s, err := w.keeper(t).TakeSeat(t.Context(), w.root, w.seat)
	check(t, err)
	dup, err := s.Dup()
	check(t, err)
	check(t, s.Held())
	check(t, s.Close())
	if err = s.Held(); !errors.Is(err, ErrReleased) {
		t.Errorf("held after close: %v", err)
	}
	if err = s.Close(); !errors.Is(err, ErrReleased) {
		t.Errorf("closed twice: %v", err)
	}
	if _, err = s.Dup(); !errors.Is(err, ErrReleased) {
		t.Errorf("dup after close: %v", err)
	}
	if _, err = HoldSeat(t.Context(), w.clock, w.root, w.seat); !errors.Is(err, ErrBusy) {
		t.Fatalf("held by the link's copy: %v", err)
	}
	link, err := AdoptSeat(dup, w.seat)
	check(t, err)
	if link.Seat() != w.seat {
		t.Errorf("seat %v", link.Seat())
	}
	check(t, link.Close())
	h, err := HoldSeat(t.Context(), w.clock, w.root, w.seat)
	check(t, err)
	check(t, h.Release())
	if err = h.Release(); !errors.Is(err, ErrReleased) {
		t.Errorf("released twice: %v", err)
	}
}

// AdoptSeat takes only a description that holds the seat: one another description holds is
// refused.
func TestAdoptSeatRefusesADescriptionThatDoesNotHoldTheSeat(t *testing.T) {
	w := newWorld(t)
	s, err := w.keeper(t).TakeSeat(t.Context(), w.root, w.seat)
	check(t, err)
	t.Cleanup(func() { check(t, s.Close()) })
	other, err := os.OpenFile(w.seat.Lock(), os.O_RDWR, 0)
	check(t, err)
	if _, err = AdoptSeat(other, w.seat); !errors.Is(err, ErrBusy) {
		t.Fatalf("got %v", err)
	}
	if err = other.Close(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("a refused description is closed: %v", err)
	}
}

// LK4: the record is written through the held description and replaced whole.
func TestWriteRecordReplacesTheLockFilesContentWhileHeld(t *testing.T) {
	w := newWorld(t)
	s, err := w.keeper(t).TakeSeat(t.Context(), w.root, w.seat)
	check(t, err)
	before := inode(t, w.seat.Lock())
	check(t, s.WriteRecord([]byte(`{"v":1,"run":"0123456789abcdef","started_ms":12345678}`)))
	check(t, s.WriteRecord([]byte(`{"v":1}`)))
	got, err := os.ReadFile(w.seat.Lock())
	check(t, err)
	if string(got) != `{"v":1}` || inode(t, w.seat.Lock()) != before {
		t.Errorf("record %q", got)
	}
	if _, err = HoldSeat(t.Context(), w.clock, w.root, w.seat); !errors.Is(err, ErrBusy) {
		t.Errorf("the seat was freed by the write: %v", err)
	}
	check(t, s.Close())
	if err = s.WriteRecord(nil); !errors.Is(err, ErrReleased) {
		t.Errorf("write after close: %v", err)
	}
}

func TestALockFileThatIsNotTheMembersIsRefused(t *testing.T) {
	w := newWorld(t)
	check(t, os.Symlink(w.run.Dir()+"/elsewhere", w.run.KeeperLock()))
	if _, err := TakeKeeper(t.Context(), w.clock, w.root, w.run); !errors.Is(err, homefs.ErrConfig) {
		t.Fatalf("a link: %v", err)
	}
	check(t, unix.Mkfifo(w.seat.Lock(), 0o600))
	if _, err := HoldSeat(t.Context(), w.clock, w.root, w.seat); !errors.Is(err, homefs.ErrConfig) {
		t.Fatalf("a FIFO: %v", err)
	}
}

func TestACancelledWaitStopsTheTries(t *testing.T) {
	w := newWorld(t)
	k := w.keeper(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := TakeKeeper(ctx, SystemClock(), w.root, w.run); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	check(t, k.Release())
}

func TestSystemClockSleeps(t *testing.T) {
	check(t, SystemClock().Sleep(t.Context(), time.Nanosecond))
}

func TestBusyErrorNamesThePath(t *testing.T) {
	err := &BusyError{Path: "/x/seat.lock"}
	if err.Error() != "lock: /x/seat.lock is held by another process" {
		t.Errorf("%q", err.Error())
	}
}
