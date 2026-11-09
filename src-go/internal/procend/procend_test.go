package procend

import (
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/reexec"
)

// spinners is how many busy threads the locking helper runs: a killed process with many threads
// is the one whose leader reads Z while the rest are still exiting (issue #182).
const spinners = 16

var helpers = reexec.Helpers{
	// lockspin takes an exclusive flock on its argument and keeps spinners threads busy until it
	// is killed.
	"lockspin": func(h *reexec.Helper) int {
		f, err := os.OpenFile(h.Args[0], os.O_RDWR|os.O_CREATE, 0o600)
		if err != nil {
			return 3
		}
		if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
			return 4
		}
		var work atomic.Int64
		for range spinners {
			go func() {
				runtime.LockOSThread()
				for {
					work.Add(1) // busy until the kill: a thread the kernel must end
				}
			}()
		}
		if err := h.Ready(); err != nil {
			return 2
		}
		return 0
	},
}

func TestMain(m *testing.M) { os.Exit(reexec.Main(m, helpers)) }

func TestStatusParse(t *testing.T) {
	for _, c := range []struct {
		name, status string
		ended        bool
	}{
		{"running", "Name:\tx\nState:\tR (running)\nThreads:\t1\n", false},
		{"sleeping", "State:\tS (sleeping)\nThreads:\t9\n", false},
		{"leader gone, threads still exiting", "State:\tZ (zombie)\nThreads:\t3\n", false},
		{"the whole group gone", "State:\tZ (zombie)\nThreads:\t1\n", true},
		{"dead", "State:\tX (dead)\nThreads:\t1\n", true},
	} {
		got, err := endedIn([]byte(c.status))
		if err != nil || got != c.ended {
			t.Errorf("%s: endedIn = %v, %v, want %v", c.name, got, err, c.ended)
		}
	}
	for _, bad := range []string{"", "State:\tZ (zombie)\n", "Threads:\t1\n", "State:\nThreads:\t1\n", "State:\tZ\nThreads:\tx\n"} {
		if _, err := endedIn([]byte(bad)); err == nil {
			t.Errorf("endedIn(%q) is no error", bad)
		}
	}
}

// A process reaped while its status was being read has ended; a refused read is no answer.
func TestAProcessReapedMidReadHasVanished(t *testing.T) {
	for _, err := range []error{
		&fs.PathError{Op: "open", Path: "1/status", Err: unix.ENOENT},
		&fs.PathError{Op: "read", Path: "1/status", Err: unix.ESRCH},
	} {
		if !vanished(err) {
			t.Errorf("vanished(%v) = false", err)
		}
	}
	if vanished(&fs.PathError{Op: "read", Path: "1/status", Err: unix.EACCES}) {
		t.Error("a refused read was taken for a process that ended")
	}
}

func TestThisProcessHasNotEnded(t *testing.T) {
	if ended, err := Ended(os.Getpid()); err != nil || ended {
		t.Fatalf("Ended(self) = %v, %v", ended, err)
	}
}

func TestAProcessThatDoesNotExistHasEnded(t *testing.T) {
	if ended, err := Ended(math.MaxInt32); err != nil || !ended {
		t.Fatalf("Ended(no such pid) = %v, %v", ended, err)
	}
	if _, err := Ended(0); err == nil {
		t.Fatal("pid 0 is no process, and no error")
	}
}

// The reproduction of issue #182: a killed process holding a flock with many threads. Once Ended
// says it has ended, the lock must be free. Judged by the leader's Z alone, the lock was still
// held at that moment in about a third of the rounds, so sixty rounds fail it with near
// certainty.
func TestAKilledProcessHasLetGoOfItsLockOnceEnded(t *testing.T) {
	const rounds = 60
	path := filepath.Join(t.TempDir(), "lock")
	for round := range rounds {
		p := reexec.Start(t, "lockspin", path)
		if err := unix.Kill(p.Pid, unix.SIGKILL); err != nil {
			t.Fatal(err)
		}
		waitEnded(t, p.Pid)
		if held(t, path) {
			t.Fatalf("round %d: Ended(%d) is true while its flock is still held", round, p.Pid)
		}
	}
}

// waitEnded polls Ended every millisecond (a coarser poll would miss the window in which the
// leader is a zombie and its threads are not yet gone) and fails the test after ten seconds.
func waitEnded(t *testing.T, pid int) {
	t.Helper()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	bound := time.NewTimer(10 * time.Second)
	defer bound.Stop()
	for {
		ended, err := Ended(pid)
		if err != nil {
			t.Fatal(err)
		}
		if ended {
			return
		}
		select {
		case <-tick.C:
		case <-bound.C:
			t.Fatalf("process %d did not end within 10 s of SIGKILL", pid)
		}
	}
}

func held(t *testing.T, path string) bool {
	t.Helper()
	f, err := os.OpenFile(filepath.Clean(path), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return true
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	return false
}
