// Package proc is the one place the QA module starts a process: every command a QA tool runs
// (git, the gates, the checked tools) goes through a Runner, so a test can substitute a fake
// and see the argv. The nested QA module cannot import the main module's execx (an internal
// package of another module), so this is its own seam, shared by qa/bashlimits and qa/changed.
package proc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// killGrace is how long a killed command's output pipes get to drain before Run gives up on them.
const killGrace = 2 * time.Second

// Cmd is one command to run.
type Cmd struct {
	Argv []string
	// Dir is the working directory ("" is the caller's).
	Dir string
	// Env is extra KEY=value entries added to the caller's environment.
	Env []string
	// Stdin, when set, is what the command reads on its standard input.
	Stdin io.Reader
	// Merge sends stderr into Stdout, in the order the command wrote them.
	Merge bool
	// StdoutTo and StderrTo, when set, receive that stream as the command writes it instead of
	// the Result capturing it: a long gate's output reaches the terminal live and in order.
	StdoutTo, StderrTo io.Writer
}

// Result is how a command ended.
type Result struct {
	Stdout, Stderr string
	// Code is the exit status; -1 when the command was killed by a signal.
	Code int
	// TimedOut is that the context's deadline passed before the command ended; the command's
	// process group was killed.
	TimedOut bool
	// Leftover is that the command ended on its own but a process it started kept its output
	// open past the grace period. Stdout and Stderr hold what was written; the leftover process
	// group has been killed. A caller treats it as a failure of the command.
	Leftover bool
}

// Runner runs a command. The error is for a command that could not be started at all; a
// command that ran and failed is a Result with its Code.
type Runner interface {
	Run(ctx context.Context, cmd Cmd) (Result, error)
}

// OS runs commands on this machine, each in its own process group so that ending the context
// ends the command and everything it started.
type OS struct{}

// Run implements Runner.
func (OS) Run(ctx context.Context, c Cmd) (Result, error) {
	if len(c.Argv) == 0 {
		return Result{}, errors.New("proc: empty argv")
	}
	cmd := exec.CommandContext(ctx, c.Argv[0], c.Argv[1:]...)
	cmd.Dir = c.Dir
	cmd.Stdin = c.Stdin
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = killGrace
	var stdout, stderr bytes.Buffer
	cmd.Stdout = streamOr(c.StdoutTo, &stdout)
	if c.Merge {
		cmd.Stderr = cmd.Stdout
	} else {
		cmd.Stderr = streamOr(c.StderrTo, &stderr)
	}
	runErr := cmd.Run()
	if cmd.ProcessState == nil {
		return Result{}, fmt.Errorf("proc %s: %w", c.Argv[0], runErr)
	}
	res := Result{Stdout: stdout.String(), Stderr: stderr.String(), Code: cmd.ProcessState.ExitCode()}
	if errors.Is(runErr, exec.ErrWaitDelay) && ctx.Err() == nil {
		// The command ended; a child of its group still holds the pipe. Keep what was written.
		res.Leftover = true
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return Result{}, fmt.Errorf("proc %s: cannot end its leftover child: %w", c.Argv[0], err)
		}
		runErr = nil
	}
	return res.finish(ctx, runErr, c.Argv[0])
}

// streamOr is the writer a stream goes to: the caller's when given, else the capture buffer.
func streamOr(to io.Writer, capture *bytes.Buffer) io.Writer {
	if to != nil {
		return to
	}
	return capture
}

// finish settles how the run ended: a context that ended first is a kill (a timeout when it was
// the deadline), a non-zero exit is a Result, anything else is a failure of the run itself.
func (r Result) finish(ctx context.Context, runErr error, program string) (Result, error) {
	r.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
	if err := runFailure(runErr, ctx.Err() != nil); err != nil {
		return Result{}, fmt.Errorf("proc %s: %w", program, err)
	}
	return r, nil
}

// runFailure is the run error that means the command did not run to an exit: not a kill by the
// context, and not merely a non-zero status.
func runFailure(runErr error, killed bool) error {
	var exitErr *exec.ExitError
	if killed || errors.As(runErr, &exitErr) {
		return nil
	}
	return runErr
}
