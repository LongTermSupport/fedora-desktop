package ccylaunch

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The fake machine: a runtime directory holding the lock files, and a /proc of fake processes.
// The boot time is 1000; a process started at tick t started at 1000 + t/100 seconds.
const (
	bootTime    = 1000
	holderPID   = 4242
	childPID    = 4243
	grandPID    = 4244
	strangerPID = 5000
)

type lockRig struct {
	run    *run
	runDir string
	proc   string
	held   map[string]*os.File // lock file name -> the descriptor holding it
	sent   []string            // "<pid> <signal>", in order
	dies   map[syscall.Signal]bool
	pauses int
}

func newLockRig(t *testing.T, answer string, args ...string) *lockRig {
	t.Helper()
	r := &lockRig{runDir: t.TempDir(), proc: t.TempDir(), held: map[string]*os.File{}, dies: map[syscall.Signal]bool{syscall.SIGTERM: true}}
	if len(args) == 0 {
		args = []string{"--build-lock"}
	}
	r.run = newRun(install(t), args...).env("XDG_RUNTIME_DIR=" + r.runDir)
	f := &r.run.front
	f.Stdin = strings.NewReader(answer)
	f.ProcRoot = r.proc
	f.Clock = fixedClock{now: time.Unix(1660, 0)}
	f.Pause = func(time.Duration) { r.pauses++ }
	f.Signal = r.signal
	writeTree(t, r.proc, map[string]string{"stat": "cpu 1 2 3\nbtime " + strconv.Itoa(bootTime) + "\nprocesses 9\n"})
	t.Cleanup(func() {
		for _, h := range r.held {
			if err := h.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	return r
}

// process places a fake process: its parent and the tick it started at.
func (r *lockRig) process(t *testing.T, pid, ppid int, startTick int64) {
	t.Helper()
	fields := make([]string, 20)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0], fields[1], fields[19] = "S", strconv.Itoa(ppid), strconv.FormatInt(startTick, 10)
	line := strconv.Itoa(pid) + " (a (strange) name) " + strings.Join(fields, " ") + " 0 0\n"
	writeTree(t, r.proc, map[string]string{strconv.Itoa(pid) + "/stat": line})
}

// lock places a lock file with a record; held, the rig holds its flock as a holder would.
func (r *lockRig) lock(t *testing.T, name, record string, held bool) {
	t.Helper()
	dir := filepath.Join(r.runDir, "ccy", "image-builds")
	writeTree(t, dir, map[string]string{name: record})
	if !held {
		return
	}
	h, err := os.OpenFile(filepath.Join(dir, name), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(h.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	r.held[name] = h
}

// signal is the fake kill: it records the signal, and a signal the process dies of removes it;
// the holder's death lets every lock go.
func (r *lockRig) signal(pid int, sig syscall.Signal) error {
	r.sent = append(r.sent, strconv.Itoa(pid)+" "+sig.String())
	if !r.dies[sig] {
		return nil
	}
	if err := os.RemoveAll(filepath.Join(r.proc, strconv.Itoa(pid))); err != nil {
		return err
	}
	if pid == holderPID {
		for name, h := range r.held {
			delete(r.held, name)
			if err := h.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *lockRig) start(t *testing.T) (term.ExitCode, string) {
	t.Helper()
	code := r.run.start(t)
	return code, r.run.stdout.String()
}

func (r *lockRig) stderr() string { return r.run.stderr.String() }

func mustContain(t *testing.T, what, text string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(text, w) {
			t.Errorf("%s lacks %q:\n%s", what, w, text)
		}
	}
}

// A live build: the holder started at 1500 (tick 50000), its build at 1600, with a child and a
// grandchild, and an unrelated process beside them.
func liveBuild(t *testing.T, answer string) *lockRig {
	t.Helper()
	r := newLockRig(t, answer)
	r.process(t, holderPID, 1, 50000)
	r.process(t, childPID, holderPID, 60000)
	r.process(t, grandPID, childPID, 60001)
	r.process(t, strangerPID, 1, 60002)
	r.lock(t, "claude-yolo_latest.lock", "4242 1600 claude-yolo:latest\n", true)
	return r
}

// F14: no lock directory, or nothing held, is no build.
func TestBuildLockWithNoBuild(t *testing.T) {
	r := newLockRig(t, "")
	code, out := r.start(t)
	if code != term.ExitOK || out != "No image build is running.\n" {
		t.Fatalf("exit %d, stdout %q", code, out)
	}
	if len(r.run.execer.Execs()) != 0 {
		t.Fatal("handed over to the bash")
	}
}

// F14: a record in a lock nobody holds is stale, said so, and ignored.
func TestBuildLockNamesAStaleRecordAndIgnoresIt(t *testing.T) {
	r := newLockRig(t, "")
	r.lock(t, "claude-yolo_latest.lock", "4242 1600 claude-yolo:latest\n", false)
	r.lock(t, "claude-yolo_proj.lock", "", false)
	code, out := r.start(t)
	if code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	mustContain(t, "stdout", out, "claude-yolo:latest", "process 4242", "stale", "ignored", "No image build is running.")
	if strings.Contains(out, "claude-yolo:proj") {
		t.Errorf("an empty record was reported:\n%s", out)
	}
	if len(r.sent) != 0 {
		t.Errorf("signalled %q", r.sent)
	}
}

// F14: a held lock whose record names a process that has gone, or a later process given the
// same pid, or no record yet, is reported and never offered to be stopped.
func TestBuildLockNeverOffersToStopAProcessItCannotVouchFor(t *testing.T) {
	for _, tc := range []struct {
		name, record, want string
		holder             bool // a process with the record's pid runs
	}{
		{"gone", "4242 1600 claude-yolo:latest\n", "has gone", false},
		{"pid used again", "4242 1400 claude-yolo:latest\n", "started after", true},
		{"no record yet", "", "not written its record yet", false},
		{"unreadable record", "nonsense\n", "not written its record yet", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newLockRig(t, "y\n")
			if tc.holder {
				r.process(t, holderPID, 1, 50000)
			}
			r.lock(t, "claude-yolo_latest.lock", tc.record, true)
			code, out := r.start(t)
			if code != term.ExitOK {
				t.Fatalf("exit %d", code)
			}
			mustContain(t, "stdout", out, "claude-yolo:latest", tc.want, "nothing to stop")
			if len(r.sent) != 0 || strings.Contains(r.stderr(), "[y/N]") {
				t.Errorf("offered or sent a signal: %q, stderr %q", r.sent, r.stderr())
			}
		})
	}
}

// F14: a live build is shown with its holder and age, and is left alone on no.
func TestBuildLockShowsALiveBuildAndLeavesItOnNo(t *testing.T) {
	r := liveBuild(t, "n\n")
	code, out := r.start(t)
	if code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	mustContain(t, "stdout", out, "claude-yolo:latest", "process 4242", "1 minute", "Nothing was stopped.")
	mustContain(t, "stderr", r.stderr(), "[y/N]")
	if len(r.sent) != 0 {
		t.Errorf("signalled %q", r.sent)
	}
}

// F14: on yes the holder and everything it started get SIGTERM, holder first; the run waits for
// the lock to be let go and says so.
func TestBuildLockStopsTheHolderAndItsChildrenOnYes(t *testing.T) {
	r := liveBuild(t, "y\n")
	code, out := r.start(t)
	if code != term.ExitOK {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, r.stderr())
	}
	want := []string{"4242 terminated", "4243 terminated", "4244 terminated"}
	if !slices.Equal(r.sent, want) {
		t.Fatalf("signals %q, want %q (never the unrelated 5000)", r.sent, want)
	}
	mustContain(t, "stdout", out, "The build of claude-yolo:latest by process 4242 was stopped.")
}

// F14: a holder that ignores SIGTERM gets SIGKILL after the wait; one that survives that too is
// a failure that says so.
func TestBuildLockKillsAHolderThatIgnoresTerm(t *testing.T) {
	r := liveBuild(t, "y\n")
	r.dies = map[syscall.Signal]bool{syscall.SIGKILL: true}
	code, out := r.start(t)
	if code != term.ExitOK {
		t.Fatalf("exit %d, stdout %q", code, out)
	}
	if !slices.Contains(r.sent, "4242 killed") || r.pauses == 0 {
		t.Fatalf("signals %q after %d pauses, want a SIGKILL after waiting", r.sent, r.pauses)
	}
	mustContain(t, "stdout", out, "was stopped")

	r = liveBuild(t, "y\n")
	r.dies = map[syscall.Signal]bool{}
	code, out = r.start(t)
	if code != term.ExitFailure {
		t.Fatalf("exit %d, want 1 for a build that would not stop", code)
	}
	mustContain(t, "stderr", r.stderr(), "could not stop", "process 4242")
	if strings.Contains(out, "was stopped") {
		t.Errorf("claimed a stop: %q", out)
	}
}

// F14: a failed signal is the run's failure, named.
func TestBuildLockReportsAFailedSignal(t *testing.T) {
	r := liveBuild(t, "y\n")
	r.run.front.Signal = func(int, syscall.Signal) error { return errors.New("operation not permitted") }
	if code, out := r.start(t); code != term.ExitFailure {
		t.Fatalf("exit %d, stdout %q", code, out)
	}
	mustContain(t, "stderr", r.stderr(), "operation not permitted")
}

// F14: the input ending before an answer cancels; nothing is stopped.
func TestBuildLockEndOfInputCancels(t *testing.T) {
	r := liveBuild(t, "")
	code, out := r.start(t)
	if code != term.ExitCancelled || len(r.sent) != 0 {
		t.Fatalf("exit %d, signals %q", code, r.sent)
	}
	mustContain(t, "stdout", out, "Nothing was stopped.")
}

// F14: --build-lock takes no other argument; CCY_LEGACY sends it to the bash, which refuses it.
func TestBuildLockArguments(t *testing.T) {
	r := newLockRig(t, "", "--build-lock", "extra")
	if code, out := r.start(t); code != term.ExitUsage {
		t.Fatalf("exit %d, stdout %q", code, out)
	}
	mustContain(t, "stderr", r.stderr(), "--build-lock takes no other argument")

	r = newLockRig(t, "")
	r.run.env(LegacyEnv + "=1")
	r.start(t)
	if len(r.run.execer.Execs()) != 1 {
		t.Fatal("CCY_LEGACY did not hand --build-lock over")
	}
}

// F14: a lock that is not a regular file is an error named, never a silent "no build".
func TestBuildLockRefusesWhatIsNotALockFile(t *testing.T) {
	r := newLockRig(t, "")
	writeTree(t, r.runDir, map[string]string{"ccy/image-builds/claude-yolo_latest.lock/inside": "x"})
	if code, out := r.start(t); code != term.ExitFailure {
		t.Fatalf("exit %d, stdout %q", code, out)
	}
	mustContain(t, "stderr", r.stderr(), "claude-yolo_latest.lock")
}

func TestParseBuildRecord(t *testing.T) {
	for _, tc := range []struct {
		text string
		ok   bool
	}{
		{"4242 1600 claude-yolo:latest\n", true},
		{"4242 1600 claude-yolo:latest", false},
		{"0 1600 claude-yolo:latest\n", false},
		{"x 1600 claude-yolo:latest\n", false},
		{"4242 1600\n", false},
		{"4242 1600 a b\n", false},
		{"", false},
	} {
		rec, ok := parseBuildRecord(tc.text)
		if ok != tc.ok || (ok && (rec.pid != 4242 || rec.began != 1600 || rec.image != "claude-yolo:latest")) {
			t.Errorf("%q: %+v %v, want ok=%v", tc.text, rec, ok, tc.ok)
		}
	}
}
