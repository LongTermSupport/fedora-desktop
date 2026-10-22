package guestroot

import (
	"context"
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/reexec"
)

var helpers = reexec.Helpers{
	// idle is a process to pin: it waits until released or killed.
	"idle": func(h *reexec.Helper) int {
		if err := h.Ready(); err != nil {
			return 2
		}
		return 0
	},
	// flock holds LOCK_EX on the file named by Args[0] until released or killed.
	"flock": func(h *reexec.Helper) int {
		f, err := os.OpenFile(h.Args[0], os.O_RDWR, 0)
		if err != nil {
			return 2
		}
		if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
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

// pids answers each call with the next pid in turn, the last one again after that.
func pids(list ...int) func(context.Context) (int, error) {
	i := 0
	return func(context.Context) (int, error) {
		p := list[min(i, len(list)-1)]
		i++
		return p, nil
	}
}

// killing is the real kernel, with the pinned process killed just before the alive check.
type killing struct {
	LinuxKernel

	p *reexec.Process
}

func (k killing) PidfdAlive(fd int) error {
	k.p.Kill()
	return k.LinuxKernel.PidfdAlive(fd)
}

// kernelErr is a kernel whose pidfd_open fails.
type kernelErr struct{ err error }

func (k kernelErr) PidfdOpen(int) (int, error) { return -1, k.err }
func (kernelErr) PidfdAlive(int) error         { return nil }

func host() Pinner { return Pinner{Proc: "/proc", Kernel: LinuxKernel{}} }

func TestPinHoldsALiveInit(t *testing.T) {
	p := reexec.Start(t, "idle")
	r, err := host().Pin(t.Context(), pids(p.Pid))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cerr := r.Close(); cerr != nil {
			t.Error(cerr)
		}
	}()
	if r.PID() != p.Pid || r.Alive() != nil {
		t.Fatalf("pid %d, alive %v", r.PID(), r.Alive())
	}
	if _, serr := r.Stat("/etc"); serr != nil {
		t.Fatalf("Stat through a live root: %v", serr)
	}
	stat, found, err := r.ReadProcStat(p.Pid)
	if err != nil || !found || len(stat) == 0 {
		t.Fatalf("ReadProcStat: %q %v %v", stat, found, err)
	}
	checkInitExeIsTheTestBinary(t, r)
	p.Kill()
	if err := r.Alive(); !errors.Is(err, ErrGone) {
		t.Errorf("Alive after the init died: %v", err)
	}
	if _, err := r.Stat("/etc"); !errors.Is(err, ErrGone) {
		t.Errorf("Stat after the init died: %v", err)
	}
	if _, err := r.InitExe(); !errors.Is(err, ErrGone) {
		t.Errorf("InitExe after the init died: %v", err)
	}
}

// checkInitExeIsTheTestBinary checks InitExe against the test binary, which is the pinned
// process's executable.
func checkInitExeIsTheTestBinary(t *testing.T, r *Root) {
	t.Helper()
	exe, err := r.InitExe()
	if err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var st unix.Stat_t
	if serr := unix.Stat(self, &st); serr != nil {
		t.Fatal(serr)
	}
	if exe.Dev != st.Dev || exe.Ino != st.Ino {
		t.Errorf("InitExe is %d:%d, the test binary %d:%d", exe.Dev, exe.Ino, st.Dev, st.Ino)
	}
}

func TestPinRefusesAnInitThatDiesOrChanges(t *testing.T) {
	t.Run("killed between the root open and the check", func(t *testing.T) {
		p := reexec.Start(t, "idle")
		pin := Pinner{Proc: "/proc", Kernel: killing{p: p}}
		if _, err := pin.Pin(t.Context(), pids(p.Pid)); !errors.Is(err, ErrMoved) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("a different second pid", func(t *testing.T) {
		p := reexec.Start(t, "idle")
		if _, err := host().Pin(t.Context(), pids(p.Pid, p.Pid+1)); !errors.Is(err, ErrMoved) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("a pid with no process", func(t *testing.T) {
		pin := Pinner{Proc: "/proc", Kernel: kernelErr{err: unix.ESRCH}}
		if _, err := pin.Pin(t.Context(), pids(4194000)); !errors.Is(err, ErrMoved) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestPinRefusesWhatItCannotPin(t *testing.T) {
	failing := func(context.Context) (int, error) { return 0, unix.EIO }
	p := reexec.Start(t, "idle")
	second := 0
	failsSecond := func(context.Context) (int, error) {
		second++
		if second > 1 {
			return 0, unix.EIO
		}
		return p.Pid, nil
	}
	cases := []struct {
		name string
		pin  Pinner
		pid  func(context.Context) (int, error)
	}{
		{"the pid cannot be read", host(), failing},
		{"the second pid cannot be read", host(), failsSecond},
		{"pid 1 is the host's init", host(), pids(1)},
		{"pid 0", host(), pids(0)},
		{"a relative proc", Pinner{Proc: "proc", Kernel: LinuxKernel{}}, pids(p.Pid)},
		{"no kernel", Pinner{Proc: "/proc"}, pids(p.Pid)},
		{"pidfd_open fails", Pinner{Proc: "/proc", Kernel: kernelErr{err: unix.EMFILE}}, pids(p.Pid)},
		{"no such proc", Pinner{Proc: t.TempDir(), Kernel: LinuxKernel{}}, pids(p.Pid)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := c.pin.Pin(t.Context(), c.pid)
			if err == nil {
				t.Fatal("pinned")
			}
			if r != nil {
				t.Fatal("a root beside an error")
			}
		})
	}
}

// openFDs is this process's open descriptors.
func openFDs(t *testing.T) int {
	t.Helper()
	es, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	return len(es)
}

// Each failure after the pidfd is open closes it: the root's open, and each later refusal.
func TestPinClosesWhatItOpenedOnFailure(t *testing.T) {
	p := reexec.Start(t, "idle")
	cases := map[string]func(*recorder) func(context.Context) (int, error){
		"the root cannot be opened": func(rec *recorder) func(context.Context) (int, error) {
			rec.arm(0, unix.EIO)
			return pids(p.Pid)
		},
		"a different second pid": func(*recorder) func(context.Context) (int, error) { return pids(p.Pid, p.Pid+1) },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			rec := newRecorder()
			pid := setup(rec)
			before := openFDs(t)
			if _, err := host().pinWith(t.Context(), rec, pid); err == nil {
				t.Fatal("pinned")
			}
			if after := openFDs(t); after != before {
				t.Errorf("%d descriptors open before, %d after", before, after)
			}
		})
	}
}

func TestLinuxKernelPidfd(t *testing.T) {
	p := reexec.Start(t, "idle")
	k := LinuxKernel{}
	fd, err := k.PidfdOpen(p.Pid)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := unix.Close(fd); err != nil {
			t.Error(err)
		}
	}()
	if err := k.PidfdAlive(fd); err != nil {
		t.Fatalf("a live process: %v", err)
	}
	p.Kill()
	if err := k.PidfdAlive(fd); !errors.Is(err, unix.ESRCH) {
		t.Fatalf("a dead process: %v", err)
	}
	if err := k.PidfdAlive(-1); err == nil {
		t.Fatal("a bad fd is alive")
	}
}

func TestInitExeOfAFixtureRoot(t *testing.T) {
	g := newTree(t)
	if _, err := g.root(newRecorder()).InitExe(); err == nil {
		t.Fatal("a fixture root has an init")
	}
}

func TestReadProcStatTakesAPid(t *testing.T) {
	g := newTree(t)
	g.file("/proc/42/stat", "42 (x) S 1", 0o444)
	r := g.root(newRecorder())
	data, found, err := r.ReadProcStat(42)
	if err != nil || !found || string(data) != "42 (x) S 1" {
		t.Fatalf("%q %v %v", data, found, err)
	}
	if _, found, err := r.ReadProcStat(43); err != nil || found {
		t.Fatalf("a pid with no stat: %v %v", found, err)
	}
	for _, pid := range []int{0, -1} {
		if _, _, err := r.ReadProcStat(pid); !errors.Is(err, ErrRefused) {
			t.Errorf("pid %d: %v", pid, err)
		}
	}
	g.file("/proc/44/stat", string(make([]byte, statCap+1)), 0o444)
	if _, _, err := r.ReadProcStat(44); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a stat over its cap: %v", err)
	}
}
