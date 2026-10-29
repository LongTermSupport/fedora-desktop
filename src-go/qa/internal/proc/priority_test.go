package proc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// fakePriority records what Ensure asked of the machine.
type fakePriority struct {
	nice, class int
	currentErr  error
	lowerErr    error
	execErr     error
	lowered     bool
	execPath    string
	execArgv    []string
	execEnv     []string
}

func (f *fakePriority) priority(env map[string]string) Priority {
	return Priority{
		Current: func() (int, int, error) { return f.nice, f.class, f.currentErr },
		Lower: func() error {
			f.lowered = true
			return f.lowerErr
		},
		Exec: func(path string, argv, environ []string) error {
			f.execPath, f.execArgv, f.execEnv = path, argv, environ
			if f.execErr != nil {
				return f.execErr
			}
			return errors.New("fake exec returned: a real one does not")
		},
		Executable: func() (string, error) { return "/cache/qa-all", nil },
		Args:       []string{"qa-all", "--no-wait"},
		Environ:    []string{"HOME=/home/x"},
		Getenv:     func(k string) string { return env[k] },
	}
}

func TestEnsureLowersAndReExecsAtNormalPriority(t *testing.T) {
	f := &fakePriority{nice: 0, class: 0}
	err := f.priority(map[string]string{}).Ensure()
	if err == nil || !strings.Contains(err.Error(), "fake exec returned") {
		t.Fatalf("err %v", err)
	}
	if !f.lowered || f.execPath != "/cache/qa-all" || !slices.Equal(f.execArgv, []string{"qa-all", "--no-wait"}) {
		t.Fatalf("fake %+v", f)
	}
	if !slices.Contains(f.execEnv, lowPriorityMarkEnv+"=1") || !slices.Contains(f.execEnv, "HOME=/home/x") {
		t.Fatalf("exec env %q", f.execEnv)
	}
}

func TestEnsureDoesNothingWhenAlreadyLow(t *testing.T) {
	f := &fakePriority{nice: lowNice, class: ioprioClassIdle}
	if err := f.priority(map[string]string{}).Ensure(); err != nil {
		t.Fatal(err)
	}
	if f.lowered || f.execPath != "" {
		t.Fatalf("an already low run was lowered again: %+v", f)
	}
}

func TestEnsureLowersWhenOnlyOneOfTheTwoIsLow(t *testing.T) {
	for _, f := range []*fakePriority{{nice: lowNice, class: 2}, {nice: 5, class: ioprioClassIdle}} {
		if err := f.priority(map[string]string{}).Ensure(); err == nil || !f.lowered {
			t.Fatalf("nice %d class %d: lowered=%v err=%v", f.nice, f.class, f.lowered, err)
		}
	}
}

func TestEnsureHonoursQAFullPriority(t *testing.T) {
	f := &fakePriority{currentErr: errors.New("must not be asked")}
	if err := f.priority(map[string]string{FullPriorityEnv: "1"}).Ensure(); err != nil {
		t.Fatal(err)
	}
	if f.lowered || f.execPath != "" {
		t.Fatalf("QA_FULL_PRIORITY=1 was lowered: %+v", f)
	}
}

func TestEnsureRefusesAReExecThatDidNotLower(t *testing.T) {
	f := &fakePriority{nice: 0, class: 0}
	err := f.priority(map[string]string{lowPriorityMarkEnv: "1"}).Ensure()
	if err == nil || !strings.Contains(err.Error(), FullPriorityEnv) {
		t.Fatalf("err %v", err)
	}
	if f.execPath != "" {
		t.Fatal("a second re-exec would loop")
	}
}

func TestEnsureReportsEachFailure(t *testing.T) {
	cases := map[string]*fakePriority{
		"current": {currentErr: errors.New("boom")},
		"lower":   {lowerErr: errors.New("EPERM")},
		"exec":    {execErr: errors.New("ENOEXEC")},
	}
	for name, f := range cases {
		err := f.priority(map[string]string{}).Ensure()
		if err == nil || !strings.Contains(err.Error(), FullPriorityEnv) {
			t.Fatalf("%s: err %v, want a failure naming the opt-out", name, err)
		}
	}
}

func TestEnsureReportsAnUnknownExecutable(t *testing.T) {
	f := &fakePriority{}
	p := f.priority(map[string]string{})
	p.Executable = func() (string, error) { return "", errors.New("no /proc") }
	if err := p.Ensure(); err == nil || f.execPath != "" {
		t.Fatalf("err %v exec %q", err, f.execPath)
	}
}

// TestLowerPriorityLowersTheCallingThread lowers a thread of its own, locked to a goroutine that
// ends still holding it, so the Go runtime retires the thread rather than reuse it at nice 19.
func TestLowerPriorityLowersTheCallingThread(t *testing.T) {
	type reading struct {
		nice, class int
		err         error
	}
	got := make(chan reading, 1)
	go func() {
		runtime.LockOSThread()
		if err := lowerPriority(); err != nil {
			got <- reading{err: err}
			return
		}
		nice, class, err := currentPriority()
		got <- reading{nice: nice, class: class, err: err}
	}()
	r := <-got
	if r.err != nil {
		t.Fatal(r.err)
	}
	if r.nice != lowNice || r.class != ioprioClassIdle {
		t.Fatalf("after lowerPriority: nice %d, I/O class %d", r.nice, r.class)
	}
}

func TestOSPriorityIsThisProcess(t *testing.T) {
	p := OSPriority()
	if !slices.Equal(p.Args, os.Args) || p.Getenv("PATH") != os.Getenv("PATH") {
		t.Fatalf("%+v", p)
	}
	if _, _, err := p.Current(); err != nil {
		t.Fatal(err)
	}
}

// priorityChildEnv marks the re-run of this test binary that lowers itself for real.
const priorityChildEnv = "QA_PROC_PRIORITY_CHILD"

// TestEnsureLowersEveryThreadAndChildForReal runs the real Ensure in a child of the test
// binary: the child re-execs itself low, then reports the nice value and I/O class of every
// thread it has and of a process it starts.
func TestEnsureLowersEveryThreadAndChildForReal(t *testing.T) {
	if os.Getenv(priorityChildEnv) != "" {
		reportPriorities(t)
		return
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	res, err := OS{}.Run(context.Background(), Cmd{
		Argv: []string{self, "-test.run=^TestEnsureLowersEveryThreadAndChildForReal$"},
		Env:  []string{priorityChildEnv + "=1", FullPriorityEnv + "="},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != 0 || !strings.Contains(res.Stdout, "PRIORITY-LOW threads=") {
		t.Fatalf("child exit %d\nstdout: %s\nstderr: %s", res.Code, res.Stdout, res.Stderr)
	}
}

func reportPriorities(t *testing.T) {
	t.Helper()
	if err := OSPriority().Ensure(); err != nil {
		t.Fatal(err)
	}
	// Under a QA run the test binary starts low already and Ensure has nothing to do; run by
	// hand it re-executed. Either way every thread must now be low.
	tasks, err := os.ReadDir("/proc/self/task")
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		tid, convErr := strconv.Atoi(task.Name())
		if convErr != nil {
			t.Fatal(convErr)
		}
		nice, class, prioErr := taskPriority(tid)
		if prioErr != nil {
			t.Fatal(prioErr)
		}
		if nice != lowNice || class != ioprioClassIdle {
			t.Fatalf("thread %d: nice %d, I/O class %d", tid, nice, class)
		}
	}
	// A child started now: its own stat says its nice value, and ionice reads its class.
	res, err := OS{}.Run(context.Background(), Cmd{Argv: []string{"cat", "/proc/self/stat"}})
	if err != nil || res.Code != 0 {
		t.Fatalf("cat: %v %+v", err, res)
	}
	if childNice := statNice(t, res.Stdout); childNice != lowNice {
		t.Fatalf("a child runs at nice %d", childNice)
	}
	res, err = OS{}.Run(context.Background(), Cmd{Argv: []string{"sh", "-c", "exec ionice -p $$"}})
	if err != nil || res.Code != 0 || strings.TrimSpace(res.Stdout) != "idle" {
		t.Fatalf("a child's I/O class: %v %+v, want idle", err, res)
	}
	fmt.Printf("PRIORITY-LOW threads=%d\n", len(tasks))
}

// taskPriority is a thread's nice value (from its stat) and I/O class (ioprio_get).
func taskPriority(tid int) (int, int, error) {
	raw, err := os.ReadFile(filepath.Join("/proc/self/task", strconv.Itoa(tid), "stat"))
	if err != nil {
		return 0, 0, err
	}
	nice, err := niceOfStat(string(raw))
	if err != nil {
		return 0, 0, err
	}
	prio, _, errno := syscall.Syscall(syscall.SYS_IOPRIO_GET, ioprioWhoProcess, uintptr(tid), 0)
	if errno != 0 {
		return 0, 0, errno
	}
	return nice, int(prio >> ioprioClassShift), nil
}

func statNice(t *testing.T, stat string) int {
	t.Helper()
	nice, err := niceOfStat(stat)
	if err != nil {
		t.Fatal(err)
	}
	return nice
}

// niceOfStat is field 19 of a stat line, counted after the command name's closing bracket.
func niceOfStat(stat string) (int, error) {
	cut := strings.LastIndex(stat, ") ")
	if cut < 0 {
		return 0, errors.New("unreadable stat")
	}
	const niceField = 16
	fields := strings.Fields(stat[cut+2:])
	if len(fields) <= niceField {
		return 0, errors.New("short stat")
	}
	return strconv.Atoi(fields[niceField])
}
