package watch

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

const (
	uid      = 1000
	pid      = 4242
	start    = "987654"
	logName  = "lightning-diagnostic.log"
	gibInKiB = 1 << 20
)

// base is the time every test starts at; no test reads the wall clock.
var base = time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

// world is a home holding one team's instance, a fake /proc and a fake runner.
type world struct {
	t         *testing.T
	instances string
	proc      fstest.MapFS
	run       *execx.Fake
	errOut    bytes.Buffer
	w         *Watcher
	owner     int // the uid of the instance lightning made last
}

func newWorld(t *testing.T) *world {
	t.Helper()
	instances := filepath.Join(t.TempDir(), ".var/app/org.lightning_matrix.Lightning/teams")
	if err := os.MkdirAll(filepath.Join(instances, "team-a", "cache"), 0o700); err != nil {
		t.Fatal(err)
	}
	w := &world{t: t, instances: instances, proc: fstest.MapFS{}, run: &execx.Fake{}}
	w.run.On([]string{"gdb"}, execx.Result{Stdout: []byte("Thread 1 (LWP 4242) \"lightning-matri\":\n#0 0x1 in QV4::Heap::alloc ()\n")})
	w.w = w.watcher()
	return w
}

// watcher is a fresh watchdog over the world, as after a restart of the service.
func (w *world) watcher() *Watcher {
	return &Watcher{Proc: w.proc, Run: w.run, Err: &w.errOut, UID: uid, Instances: w.instances, LogName: logName}
}

func (w *world) log() string { return filepath.Join(w.instances, "team-a", "cache", logName) }

func (w *world) record() string { return filepath.Join(w.instances, "team-a", "cache", RecordName) }

// lightning is the running instance pid: its comm, argv, owner, RSS, start time and threads.
// Its RSS starts at 1 GiB.
func (w *world) lightning(argv []string, owner int) {
	const p = pid
	dir := strconv.Itoa(p)
	w.owner = owner
	w.proc[dir+"/comm"] = &fstest.MapFile{Data: []byte("lightning-matri\n")}
	w.proc[dir+"/cmdline"] = &fstest.MapFile{Data: []byte(strings.Join(argv, "\x00") + "\x00")}
	w.proc[dir+"/stat"] = &fstest.MapFile{Data: []byte(stat(p, "lightning-matri", "R", 100, 5))}
	w.grow(gibInKiB)
	w.thread(p, p, "lightning-matri", "R", 90000, 300, "0")
	w.thread(p, p+1, "QSGRenderThread", "S", 120, 40, "futex_wait_queue")
}

// grow sets the instance's RSS.
func (w *world) grow(rssKiB int) {
	owner := strconv.Itoa(w.owner)
	w.proc[strconv.Itoa(pid)+"/status"] = &fstest.MapFile{Data: []byte(
		"Name:\tlightning-matri\nUid:\t" + owner + "\t" + owner + "\t" + owner + "\t" + owner +
			"\nVmRSS:\t " + strconv.Itoa(rssKiB) + " kB\n")}
}

func (w *world) thread(p, tid int, comm, state string, utime, stime int, wchan string) {
	dir := strconv.Itoa(p) + "/task/" + strconv.Itoa(tid)
	w.proc[dir+"/stat"] = &fstest.MapFile{Data: []byte(stat(tid, comm, state, utime, stime))}
	w.proc[dir+"/wchan"] = &fstest.MapFile{Data: []byte(wchan)}
}

// stat is a /proc stat line: its comm in parentheses (which may hold spaces and brackets), the
// state, utime and stime (fields 14 and 15) and the start time (field 22).
func stat(p int, comm, state string, utime, stime int) string {
	fields := []string{strconv.Itoa(p), "(" + comm + ")", state}
	for n := 4; n <= 13; n++ {
		fields = append(fields, "0")
	}
	fields = append(fields, strconv.Itoa(utime), strconv.Itoa(stime))
	for n := 16; n <= 21; n++ {
		fields = append(fields, "0")
	}
	fields = append(fields, start, "0", "0")
	return strings.Join(fields, " ") + "\n"
}

func (w *world) argv() []string {
	return []string{"/app/bin/lightning-matrix", "--log-file", w.log()}
}

// writeLog is Lightning writing its log at a moment.
func (w *world) writeLog(at time.Time) {
	w.t.Helper()
	f, err := os.OpenFile(w.log(), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		w.t.Fatal(err)
	}
	if _, err := f.WriteString("scroll-memory rssKiB=1\n"); err != nil {
		w.t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		w.t.Fatal(err)
	}
	if err := os.Chtimes(w.log(), at, at); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) tick(at time.Time) {
	w.t.Helper()
	if err := w.w.Tick(context.Background(), at); err != nil {
		w.t.Fatalf("tick at %s: %v", at, err)
	}
}

func (w *world) recordText() string {
	w.t.Helper()
	data, err := os.ReadFile(w.record())
	if err != nil {
		w.t.Fatal(err)
	}
	return string(data)
}

// stuck is the 25 Nov shape: the log written at base, then silent while RSS climbs.
func (w *world) stuck() {
	w.lightning(w.argv(), uid)
	w.writeLog(base)
	w.tick(base.Add(5 * time.Second))
	w.grow(2 * gibInKiB)
	w.tick(base.Add(75 * time.Second))
}

// TestW1ASilentLogWithClimbingMemoryIsRecordedOnce holds the record: beside the log, once per
// process, with each thread's CPU and state and a backtrace of every thread.
func TestW1ASilentLogWithClimbingMemoryIsRecordedOnce(t *testing.T) {
	w := newWorld(t)
	w.stuck()
	text := w.recordText()
	for _, want := range []string{
		"pid=4242 start=987654 ",
		"log silent for 75 s",
		"RSS 2097152 KiB, up 1048576 KiB since the log last moved",
		"4242 lightning-matri R 90000 300 0",
		"4243 QSGRenderThread S 120 40 futex_wait_queue",
		"backtrace (gdb):",
		"QV4::Heap::alloc",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the record lacks %q:\n%s", want, text)
		}
	}
	w.grow(3 * gibInKiB)
	w.tick(base.Add(90 * time.Second))
	if got := strings.Count(w.recordText(), "pid=4242 start=987654 "); got != 1 {
		t.Fatalf("recorded %d times", got)
	}
	if n := len(w.run.Calls()); n != 1 {
		t.Fatalf("the backtrace ran %d times", n)
	}
	if !strings.Contains(w.errOut.String(), w.record()) {
		t.Fatalf("no line names the record: %q", w.errOut.String())
	}
}

// TestW2NothingIsRecordedUnlessBothHold holds that a silent log with flat memory, or climbing
// memory with a moving log, records nothing and runs nothing.
func TestW2NothingIsRecordedUnlessBothHold(t *testing.T) {
	t.Run("silent and flat", func(t *testing.T) {
		w := newWorld(t)
		w.lightning(w.argv(), uid)
		w.writeLog(base)
		w.tick(base.Add(5 * time.Second))
		w.tick(base.Add(10 * time.Minute))
		w.assertNothing()
	})
	t.Run("climbing and moving", func(t *testing.T) {
		w := newWorld(t)
		w.lightning(w.argv(), uid)
		for step := range 10 {
			at := base.Add(time.Duration(step) * 15 * time.Second)
			w.writeLog(at)
			w.grow(gibInKiB + step*gibInKiB)
			w.tick(at.Add(time.Second))
		}
		w.assertNothing()
	})
}

func (w *world) assertNothing() {
	w.t.Helper()
	if _, err := os.Lstat(w.record()); err == nil {
		w.t.Fatal("a record was written")
	}
	if len(w.run.Calls()) != 0 {
		w.t.Fatalf("ran %v", w.run.Calls())
	}
}

// TestW3TheGrowthIsCountedFromTheLogsLastWrite holds that the baseline RSS moves with the log:
// growth while the log was still written does not count.
func TestW3TheGrowthIsCountedFromTheLogsLastWrite(t *testing.T) {
	w := newWorld(t)
	w.lightning(w.argv(), uid)
	w.writeLog(base)
	w.tick(base.Add(5 * time.Second))
	w.writeLog(base.Add(80 * time.Second))
	w.grow(2 * gibInKiB)
	w.tick(base.Add(85 * time.Second))
	w.grow(2*gibInKiB + 100*1024)
	w.tick(base.Add(150 * time.Second))
	w.assertNothing()
	w.grow(3 * gibInKiB)
	w.tick(base.Add(165 * time.Second))
	if !strings.Contains(w.recordText(), "up 1048576 KiB") {
		t.Fatalf("growth not counted from the log's last write:\n%s", w.recordText())
	}
}

// TestW4OnlyThisUsersLightningWithItsOwnLogIsWatched holds that a process is looked at only when
// it is lightning-matrix, this user's, with a --log-file that is a team instance's diagnostic
// log, and that any other process's argv is never read.
func TestW4OnlyThisUsersLightningWithItsOwnLogIsWatched(t *testing.T) {
	cases := map[string]func(w *world){
		"no log file":     func(w *world) { w.lightning([]string{"/app/bin/lightning-matrix"}, uid) },
		"another user":    func(w *world) { w.lightning(w.argv(), uid+1) },
		"another program": func(w *world) { w.lightning([]string{"/usr/bin/other", "--log-file", w.log()}, uid) },
		"a log elsewhere": func(w *world) {
			w.lightning([]string{"lightning-matrix", "--log-file", "/tmp/cache/" + logName}, uid)
		},
		"a log not ours": func(w *world) {
			w.lightning([]string{"lightning-matrix", "--log-file", filepath.Join(filepath.Dir(w.log()), "other.log")}, uid)
		},
		"a parent step": func(w *world) {
			w.lightning([]string{"lightning-matrix", "--log-file", w.instances + "/team-a/../team-a/cache/" + logName}, uid)
		},
		"a log not in cache": func(w *world) {
			w.lightning([]string{"lightning-matrix", "--log-file", filepath.Join(w.instances, "team-a", "data", logName)}, uid)
		},
	}
	for name, make := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			make(w)
			w.writeLog(base)
			w.tick(base.Add(5 * time.Second))
			w.grow(3 * gibInKiB)
			w.tick(base.Add(5 * time.Minute))
			w.assertNothing()
		})
	}
	t.Run("another comm", func(t *testing.T) {
		w := newWorld(t)
		w.proc["77/comm"] = &fstest.MapFile{Data: []byte("bash\n")}
		w.proc["77/cmdline"] = &fstest.MapFile{Data: []byte("bash\x00--token\x00secret\x00")}
		opened := &openLog{FS: w.proc}
		w.w.Proc = opened
		w.tick(base)
		w.assertNothing()
		if slices.Contains(opened.names, "77/cmdline") {
			t.Fatalf("another program's argv was read: %v", opened.names)
		}
	})
}

// openLog is a file system that names each file opened in it.
type openLog struct {
	fs.FS
	names []string
}

func (o *openLog) Open(name string) (fs.File, error) {
	o.names = append(o.names, name)
	return o.FS.Open(name)
}

// TestW4TheLogFileFormIsRead holds --log-file=<path> as well as --log-file <path>.
func TestW4TheLogFileFormIsRead(t *testing.T) {
	w := newWorld(t)
	w.lightning([]string{"lightning-matrix", "--log-file=" + w.log()}, uid)
	w.writeLog(base)
	w.tick(base.Add(5 * time.Second))
	w.grow(2 * gibInKiB)
	w.tick(base.Add(75 * time.Second))
	if !strings.Contains(w.recordText(), "pid=4242") {
		t.Fatal("not recorded")
	}
}

// TestW5WithoutGdbEuStackIsUsedAndWithNeitherNothingIsInstalled holds the backtrace's fallbacks.
func TestW5WithoutGdbEuStackIsUsedAndWithNeitherNothingIsInstalled(t *testing.T) {
	w := newWorld(t)
	w.run = &execx.Fake{}
	w.run.OnError([]string{"gdb"}, &execx.NotInstalledError{Name: "gdb"})
	w.run.On([]string{"eu-stack"}, execx.Result{Stdout: []byte("TID 4242:\n#0 0x1 QV4::Heap::alloc\n")})
	w.w = w.watcher()
	w.stuck()
	if text := w.recordText(); !strings.Contains(text, "backtrace (eu-stack):") || !strings.Contains(text, "TID 4242") {
		t.Fatalf("%s", text)
	}

	neither := newWorld(t)
	neither.run = &execx.Fake{}
	neither.run.OnError([]string{"gdb"}, &execx.NotInstalledError{Name: "gdb"})
	neither.run.OnError([]string{"eu-stack"}, &execx.NotInstalledError{Name: "eu-stack"})
	neither.w = neither.watcher()
	neither.stuck()
	text := neither.recordText()
	if !strings.Contains(text, "backtrace: neither gdb nor eu-stack is installed") || !strings.Contains(text, "4242 lightning-matri R") {
		t.Fatalf("%s", text)
	}
	for _, c := range neither.run.Calls() {
		if c.Name != "gdb" && c.Name != "eu-stack" {
			t.Fatalf("ran %s", c.Line())
		}
	}
}

// TestW5AFailedBacktraceIsRecordedAsSuch holds a tool that ran and failed in the record, with
// what it said, and the threads still there.
func TestW5AFailedBacktraceIsRecordedAsSuch(t *testing.T) {
	w := newWorld(t)
	w.run = &execx.Fake{}
	w.run.On([]string{"gdb"}, execx.Result{ExitCode: 1, Stderr: []byte("ptrace: Operation not permitted.\n")})
	w.w = w.watcher()
	w.stuck()
	text := w.recordText()
	if !strings.Contains(text, "backtrace (gdb) failed") || !strings.Contains(text, "Operation not permitted") ||
		!strings.Contains(text, "4243 QSGRenderThread") {
		t.Fatalf("%s", text)
	}
}

// TestW6TheBacktraceShowsNoArgumentsAndAsksNoServer holds the tools' argv: gdb prints no frame
// argument (an argument could be a message's text) and asks no debuginfod server.
func TestW6TheBacktraceShowsNoArgumentsAndAsksNoServer(t *testing.T) {
	w := newWorld(t)
	w.stuck()
	calls := w.run.Calls()
	if len(calls) != 1 {
		t.Fatalf("%v", calls)
	}
	gdb := calls[0]
	line := strings.Join(gdb.Args, " ")
	for _, want := range []string{"-batch", "-nx", "-p 4242", "set print frame-arguments none", "thread apply all bt"} {
		if !strings.Contains(line, want) {
			t.Errorf("gdb argv %q lacks %q", line, want)
		}
	}
	if strings.Contains(line, "bt full") || strings.Contains(line, "info locals") {
		t.Errorf("gdb argv %q prints locals", line)
	}
	if !slices.Contains(gdb.Unset, "DEBUGINFOD_URLS") {
		t.Errorf("gdb may ask a debuginfod server: unset %v", gdb.Unset)
	}
}

// TestW7TheRecordIsCapped holds the record file to RecordCap, rolled once to RecordName.1.
func TestW7TheRecordIsCapped(t *testing.T) {
	w := newWorld(t)
	full := strings.Repeat("x", RecordCap-10) + "\n"
	if err := os.WriteFile(w.record(), []byte(full), 0o600); err != nil {
		t.Fatal(err)
	}
	w.stuck()
	old, err := os.ReadFile(w.record() + ".1")
	if err != nil || string(old) != full {
		t.Fatalf("the full record was not rolled to .1: %v", err)
	}
	if !strings.Contains(w.recordText(), "pid=4242") {
		t.Fatal("the new record is not in the fresh file")
	}

	huge := newWorld(t)
	huge.run = &execx.Fake{}
	huge.run.On([]string{"gdb"}, execx.Result{Stdout: []byte(strings.Repeat("#0 frame\n", RecordCap))})
	huge.w = huge.watcher()
	huge.stuck()
	info, err := os.Stat(huge.record())
	if err != nil || info.Size() > RecordCap {
		t.Fatalf("a record over the cap: %v %v", info, err)
	}
	if !strings.Contains(huge.recordText(), "(cut at the cap)") {
		t.Fatal("a cut record does not say so")
	}
}

// TestW8ALinkAtTheRecordIsRefused holds that the record is never written through a link.
func TestW8ALinkAtTheRecordIsRefused(t *testing.T) {
	w := newWorld(t)
	target := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.Symlink(target, w.record()); err != nil {
		t.Fatal(err)
	}
	w.lightning(w.argv(), uid)
	w.writeLog(base)
	w.tick(base.Add(5 * time.Second))
	w.grow(2 * gibInKiB)
	if err := w.w.Tick(context.Background(), base.Add(75*time.Second)); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Lstat(target); err == nil {
		t.Fatal("written through the link")
	}
}

// TestW9AProcessGoneMidScanIsSkipped holds a process whose files vanish between reads as gone,
// not an error.
func TestW9AProcessGoneMidScanIsSkipped(t *testing.T) {
	w := newWorld(t)
	w.lightning(w.argv(), uid)
	delete(w.proc, strconv.Itoa(pid)+"/status")
	w.writeLog(base)
	w.tick(base.Add(5 * time.Second))
	w.assertNothing()
}

// TestW10ARestartDoesNotRecordAgain holds once per process across a restart of the watchdog: the
// record's own header says the process was recorded.
func TestW10ARestartDoesNotRecordAgain(t *testing.T) {
	w := newWorld(t)
	w.stuck()
	w.w = w.watcher()
	w.tick(base.Add(80 * time.Second))
	w.grow(4 * gibInKiB)
	w.tick(base.Add(95 * time.Second))
	if got := strings.Count(w.recordText(), "pid=4242 start=987654 "); got != 1 {
		t.Fatalf("recorded %d times", got)
	}
}

// TestW11LightningsFilesAreOnlyRead holds that the log is read for its time alone and the
// instance's folder gains only the record.
func TestW11LightningsFilesAreOnlyRead(t *testing.T) {
	w := newWorld(t)
	w.stuck()
	data, err := os.ReadFile(w.log())
	if err != nil || string(data) != "scroll-memory rssKiB=1\n" {
		t.Fatalf("the log changed: %q %v", data, err)
	}
	entries, err := os.ReadDir(filepath.Dir(w.log()))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{logName, RecordName}) {
		t.Fatalf("the cache folder holds %v", names)
	}
	info, err := os.Stat(w.record())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the record's mode: %v %v", info, err)
	}
}
