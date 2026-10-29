package proc

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
)

// FullPriorityEnv set to 1 keeps a QA run at the priority it was started with (CI, a machine
// with nothing else to do).
const FullPriorityEnv = "QA_FULL_PRIORITY"

// lowPriorityMarkEnv marks the run Ensure re-executed, so a re-exec that did not lower it is an
// error rather than a loop.
const lowPriorityMarkEnv = "QA_LOW_PRIORITY_EXEC"

// lowNice is the nice value of a QA run, the lowest priority there is.
const lowNice = 19

// ioprio_set(2) and ioprio_get(2): who is a thread, and the class sits above the data bits.
const (
	ioprioWhoProcess = 1
	ioprioClassShift = 13
	ioprioClassIdle  = 3
)

// Priority is how a QA command makes itself a background job: nice 19 and the idle I/O class,
// for every thread and every process it starts. Both are per thread on Linux and a thread
// inherits them from the thread that made it, so lowering one thread of a running Go program
// leaves the others as they were. Ensure lowers the calling thread and re-executes the command
// from it: execve keeps that thread's values and makes it the only thread, so everything the
// new program runs starts low.
type Priority struct {
	// Current is the calling thread's nice value and I/O class.
	Current func() (nice, ioClass int, err error)
	// Lower sets the calling thread to nice 19, I/O class idle.
	Lower func() error
	// Exec replaces the program; it returns only on failure.
	Exec       func(path string, argv, environ []string) error
	Executable func() (string, error)
	Args       []string
	Environ    []string
	Getenv     func(string) string
}

// OSPriority is the real one, for this process.
func OSPriority() Priority {
	return Priority{
		Current:    currentPriority,
		Lower:      lowerPriority,
		Exec:       syscall.Exec,
		Executable: os.Executable,
		Args:       os.Args,
		Environ:    os.Environ(),
		Getenv:     os.Getenv,
	}
}

// Ensure makes the run low priority, unless QA_FULL_PRIORITY=1 or it already is. When it
// lowers, it re-executes the program and does not return; an error is a run that cannot be made
// low, and names the opt-out.
func (p Priority) Ensure() error {
	if p.Getenv(FullPriorityEnv) == "1" {
		return nil
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	nice, class, err := p.Current()
	if err != nil {
		return fail("cannot read this run's priority", err)
	}
	if nice == lowNice && class == ioprioClassIdle {
		return nil
	}
	if p.Getenv(lowPriorityMarkEnv) == "1" {
		return fail("the re-executed run is still not low priority", fmt.Errorf("nice %d, I/O class %d", nice, class))
	}
	path, err := p.Executable()
	if err != nil {
		return fail("cannot find this program to re-execute it", err)
	}
	if err := p.Lower(); err != nil {
		return fail("cannot lower this run's priority", err)
	}
	environ := append(append([]string{}, p.Environ...), lowPriorityMarkEnv+"=1")
	return fail("cannot re-execute "+path+" at low priority", p.Exec(path, p.Args, environ))
}

func fail(what string, err error) error {
	return fmt.Errorf("%s: %w (set %s=1 to run at full priority)", what, err, FullPriorityEnv)
}

// currentPriority reads the calling thread's values. getpriority(2) returns 20 - nice.
func currentPriority() (int, int, error) {
	const niceBase = 20
	prio, err := syscall.Getpriority(syscall.PRIO_PROCESS, 0)
	if err != nil {
		return 0, 0, fmt.Errorf("getpriority: %w", err)
	}
	ioprio, err := ioprioCall(syscall.SYS_IOPRIO_GET, 0)
	if err != nil {
		return 0, 0, fmt.Errorf("ioprio_get: %w", err)
	}
	return niceBase - prio, int(ioprio >> ioprioClassShift), nil
}

// lowerPriority sets the calling thread to nice 19 and the idle I/O class; neither needs a
// privilege, since both only lower.
func lowerPriority() error {
	if err := syscall.Setpriority(syscall.PRIO_PROCESS, 0, lowNice); err != nil {
		return fmt.Errorf("setpriority: %w", err)
	}
	if _, err := ioprioCall(syscall.SYS_IOPRIO_SET, ioprioClassIdle<<ioprioClassShift); err != nil {
		return fmt.Errorf("ioprio_set: %w", err)
	}
	return nil
}

// ioprioCall is ioprio_get or ioprio_set for the calling thread (who 0).
func ioprioCall(op, value uintptr) (uintptr, error) {
	r1, _, errno := syscall.Syscall(op, ioprioWhoProcess, 0, value)
	if errno != 0 {
		return 0, errno
	}
	return r1, nil
}
