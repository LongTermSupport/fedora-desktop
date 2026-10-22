// Package reexec runs a second real process for the teamchat packages' tests: the test binary
// itself, re-run as a named helper (SPEC-n3 section 9, gap 8). A lock held by another process,
// a holder ended by SIGKILL, a keeper and its link are each a real process that no in-process
// fake can stand in for: flock(2) belongs to an open file description, and the kernel frees it
// only when the last descriptor closes, which a process's death does.
//
// A package whose tests use Start declares
//
//	func TestMain(m *testing.M) { os.Exit(reexec.Main(m, helpers)) }
//
// with helpers naming each helper's body. A helper does its work (opens and locks a file), calls
// Ready, and returns its exit code once Ready returns. The process ends only in TestMain.
package reexec

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

const (
	// helperEnv names the helper a re-run test binary runs instead of its tests.
	helperEnv = "TEAMCHAT_REEXEC_HELPER"
	// argsMark ends the test binary's own flags; the helper's arguments follow it.
	argsMark = "--"
	// readyWord starts the line a helper prints when it is ready, followed by its pid.
	readyWord = "ready"
	// unknownHelper is the exit code of a child asked for a helper its package does not name.
	unknownHelper = 125
)

// Helpers maps a helper's name to its body, which returns the child's exit code.
type Helpers map[string]func(h *Helper) int

// Helper is the child's side: its arguments, and the line it is released by.
type Helper struct {
	// Args are the arguments Start was given after the helper's name, unchanged.
	Args []string
	in   io.Reader
	out  io.Writer
	pid  int
}

// Ready tells the parent the helper is ready (its lock taken, its file open), then waits until
// the parent releases it: a line on stdin, or stdin's end when the parent has gone.
func (h *Helper) Ready() error {
	if _, err := fmt.Fprintf(h.out, "%s %d\n", readyWord, h.pid); err != nil {
		return fmt.Errorf("reexec: tell the parent: %w", err)
	}
	_, err := bufio.NewReader(h.in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("reexec: wait for the release: %w", err)
	}
	return nil
}

// runner is what Main needs of testing.M.
type runner interface{ Run() int }

// Main is the TestMain body of a package whose tests use Start: the test binary runs its tests;
// a child started by Start runs the helper it was started for and returns its exit code.
func Main(m *testing.M, helpers Helpers) int {
	return mainWith(m, os.Getenv, helpers, os.Stdin, os.Stdout, os.Stderr, os.Args)
}

func mainWith(m runner, getenv func(string) string, helpers Helpers, in io.Reader, out, errOut io.Writer, argv []string) int {
	name := getenv(helperEnv)
	if name == "" {
		return m.Run()
	}
	body, ok := helpers[name]
	if !ok {
		if _, err := fmt.Fprintf(errOut, "reexec: no helper %q in this package\n", name); err != nil {
			return unknownHelper
		}
		return unknownHelper
	}
	return body(&Helper{Args: argsOf(argv), in: in, out: out, pid: os.Getpid()})
}

// argsOf is what follows the first argsMark in argv, nil when there is none.
func argsOf(argv []string) []string {
	i := slices.Index(argv, argsMark)
	if i < 0 {
		return nil
	}
	return append([]string{}, argv[i+1:]...)
}

// Process is a running helper, held until Release or Kill. Its test's cleanup kills it if
// neither was called, so no helper outlives the test that started it.
type Process struct {
	// Pid is the helper's process id.
	Pid int

	tb    testing.TB
	stdin *os.File // the write end of the helper's stdin, its release line
	done  chan struct{}
	once  sync.Once
	end   *exitInfo
}

// exitInfo is how the helper ended: by a signal, an exit code, or a failure to run at all.
type exitInfo struct {
	code   int
	signal syscall.Signal
	err    error
	stderr string
}

// Start runs helper name in a re-run of the test binary with args, and returns once it is
// ready. Its stderr is shown if it fails.
func Start(tb testing.TB, name string, args ...string) *Process {
	tb.Helper()
	p, err := start(name, args)
	if err != nil {
		tb.Fatal(err)
	}
	p.tb = tb
	tb.Cleanup(func() {
		if !p.ended() {
			if err := p.kill(); err != nil {
				tb.Error(err)
			}
		}
	})
	return p
}

func start(name string, args []string) (*Process, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("reexec: find the test binary: %w", err)
	}
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("reexec: %w", err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		return nil, errors.Join(fmt.Errorf("reexec: %w", err), inR.Close(), inW.Close())
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &Process{stdin: inW, done: make(chan struct{})}
	var errBuf strings.Builder
	cmd := execx.Cmd{
		Name: self,
		Args: append([]string{"-test.run=^$", "-test.count=1", argsMark}, args...),
		// A -race binary sleeps a second at exit unless told not to; the parent's own race
		// options are kept.
		Env:    []string{helperEnv + "=" + name, "GORACE=" + strings.TrimSpace(os.Getenv("GORACE")+" atexit_sleep_ms=0")},
		Stdin:  inR,
		Stdout: outW,
		Stderr: &errBuf,
	}
	go func() {
		defer cancel()
		res, runErr := execx.OS{}.Run(ctx, cmd)
		p.end = endOf(res, runErr, errBuf.String())
		// The child's ends close here, so a reader of its stdout sees the end even if it never
		// printed its ready line.
		p.end.err = errors.Join(p.end.err, inR.Close(), outW.Close())
		close(p.done)
	}()
	line, err := bufio.NewReader(outR).ReadString('\n')
	closeErr := outR.Close()
	if err != nil {
		<-p.done
		return nil, errors.Join(fmt.Errorf("reexec: helper %q ended before it was ready: %s", name, describe(p.end)), inW.Close())
	}
	if p.Pid, err = parseReady(line); err == nil {
		err = childOf(p.Pid)
	}
	if err != nil {
		// Its pid is not known, so it is ended by the runner's cancel, never by a signal sent
		// to a pid it claimed.
		cancel()
		closeErr = errors.Join(closeErr, inW.Close())
		<-p.done
		return nil, errors.Join(err, closeErr)
	}
	return p, closeErr
}

// childOf is nil when pid is a child of this process, the only process a helper's Kill may
// signal: 0 and -1 name whole groups, and a pid a helper printed is only its claim.
func childOf(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("reexec: pid %d is not a process", pid)
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return fmt.Errorf("reexec: read the helper's status: %w", err)
	}
	parent, err := statParent(string(stat))
	if err != nil {
		return err
	}
	if parent != os.Getpid() {
		return fmt.Errorf("reexec: pid %d is not a child of this test", pid)
	}
	return nil
}

// statParent is the parent pid in a /proc/<pid>/stat line: the field after the state, which
// follows the last ")" (the command name may hold one).
func statParent(stat string) (int, error) {
	i := strings.LastIndexByte(stat, ')')
	if i < 0 {
		return 0, errors.New("reexec: no command name in the helper's status")
	}
	fields := strings.Fields(stat[i+1:])
	if len(fields) < 2 {
		return 0, errors.New("reexec: no parent in the helper's status")
	}
	parent, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, fmt.Errorf("reexec: the helper's parent: %w", err)
	}
	return parent, nil
}

// endOf is how a run of the child ended.
func endOf(res execx.Result, err error, stderr string) *exitInfo {
	end := &exitInfo{code: res.ExitCode, stderr: stderr}
	if exitErr, ok := errors.AsType[*execx.ExitError](err); ok {
		end.signal = exitErr.Signal
		return end
	}
	end.err = err
	return end
}

func parseReady(line string) (int, error) {
	rest, ok := strings.CutPrefix(line, readyWord+" ")
	if ok {
		rest, ok = strings.CutSuffix(rest, "\n")
	}
	pid, err := strconv.Atoi(rest)
	if !ok || err != nil || pid <= 0 {
		return 0, fmt.Errorf("reexec: the helper's first line is %q, not its ready line", line)
	}
	return pid, nil
}

func describe(end *exitInfo) string {
	switch {
	case end.err != nil:
		return fmt.Sprintf("%v; stderr: %s", end.err, end.stderr)
	case end.signal != 0:
		return fmt.Sprintf("killed by %s; stderr: %s", unix.SignalName(end.signal), end.stderr)
	default:
		return fmt.Sprintf("exited %d; stderr: %s", end.code, end.stderr)
	}
}

// Release lets the helper finish and waits for it; an exit other than 0 fails the test.
func (p *Process) Release() {
	p.tb.Helper()
	if err := p.release(); err != nil {
		p.tb.Error(err)
	}
}

// Kill ends the helper with SIGKILL, as a crash would, and waits for it.
func (p *Process) Kill() {
	p.tb.Helper()
	if err := p.kill(); err != nil {
		p.tb.Error(err)
	}
}

func (p *Process) ended() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

func (p *Process) release() error {
	var err error
	p.once.Do(func() {
		_, err = p.stdin.WriteString("go\n")
		err = errors.Join(err, p.stdin.Close())
	})
	<-p.done
	if err != nil {
		return fmt.Errorf("reexec: release the helper: %w", err)
	}
	if p.end.err != nil || p.end.signal != 0 || p.end.code != 0 {
		return fmt.Errorf("reexec: the helper %s", describe(p.end))
	}
	return nil
}

func (p *Process) kill() error {
	if p.ended() {
		return fmt.Errorf("reexec: the helper already ended: %s", describe(p.end))
	}
	err := unix.Kill(p.Pid, unix.SIGKILL)
	p.once.Do(func() { err = errors.Join(err, p.stdin.Close()) })
	<-p.done
	if err != nil {
		return fmt.Errorf("reexec: kill the helper: %w", err)
	}
	if p.end.signal != unix.SIGKILL {
		return fmt.Errorf("reexec: the helper was not killed: %s", describe(p.end))
	}
	return nil
}
