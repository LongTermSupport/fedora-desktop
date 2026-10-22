package execx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// runSession is Run for a Cmd with NewSession. It has the shape of Python's
// Popen(start_new_session=True) followed by communicate, which the status line's inner command
// relies on (Plan 00213, SPEC-status-line.md S9 to S12):
//
//   - The child starts as the leader of a new session, so its pid is its process group id.
//   - The run lasts until the child has exited and its stdout has reached end of file, so a
//     grandchild that still holds stdout keeps it going, as communicate waits for it.
//   - When ctx ends, by a cancel or a deadline, the whole group is killed with SIGKILL at once,
//     whether or not the child itself has exited. The run then waits at most Grace for the output
//     to close (a process that left the group can still hold it) and returns ctx's cause.
//   - Stdin is written by the run and closed; a child that ends without reading all of it is not
//     an error (communicate ignores the broken pipe).
//
// An *os.File given as Stdin, Stdout or Stderr is handed to the child as it is and is not waited
// on; any other reader or writer goes through a pipe this run owns, and is waited on like stdout.
func (o OS) runSession(ctx context.Context, c Cmd) (Result, error) {
	pipes := &sessionPipes{}
	res, err := o.session(ctx, c, pipes)
	return res, errors.Join(err, pipes.closeReaders())
}

// session is runSession's run, over pipes whose output ends runSession closes.
func (o OS) session(ctx context.Context, c Cmd, pipes *sessionPipes) (Result, error) {
	if ctx.Err() != nil {
		return Result{}, fmt.Errorf("run %s: %w", c.Name, context.Cause(ctx))
	}
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	// A cancel before the child exits kills the group from exec's own watch too; one after it
	// exited (a grandchild holding stdout) is the await below.
	cmd.Cancel = func() error { return killGroup(cmd.Process.Pid) }
	cmd.SysProcAttr = c.sysProcAttr(true)
	cmd.Dir = c.Dir
	cmd.Env = c.environ()
	cmd.ExtraFiles = c.ExtraFiles
	var out, errOut bytes.Buffer
	if err := pipes.wire(cmd, c, &out, &errOut); err != nil {
		return Result{}, errors.Join(err, pipes.closeUnstarted())
	}
	if err := cmd.Start(); err != nil {
		return Result{}, errors.Join(startError(c.Name, err), pipes.closeUnstarted())
	}
	run := &sessionRun{
		waiting: true, copying: true,
		exited: make(chan error, 1), drained: make(chan error, 1), fed: make(chan error, 1),
	}
	closeErr := pipes.closeChildEnds()
	go func() { run.exited <- cmd.Wait() }()
	go func() { run.drained <- pipes.copyAll() }()
	go func() { run.fed <- pipes.feed() }()
	if run.await(ctx.Done()) {
		res, err := sessionStatus(c, Result{Stdout: out.Bytes(), Stderr: errOut.Bytes()}, run.waitErr)
		return res, errors.Join(err, run.copyErr, pipes.stopFeeding(run.fed), closeErr)
	}
	killErr := killGroup(cmd.Process.Pid)
	grace, stop := context.WithTimeout(context.Background(), o.grace())
	defer stop()
	var abandonErr error
	if !run.await(grace.Done()) && run.copying {
		abandonErr = pipes.abandon()
		<-run.drained
	}
	feedErr := pipes.stopFeeding(run.fed)
	res := Result{Stdout: out.Bytes(), Stderr: errOut.Bytes(), ExitCode: -1}
	return res, errors.Join(fmt.Errorf("run %s: %w", c.Name, context.Cause(ctx)), killErr, abandonErr, feedErr, closeErr)
}

func (o OS) grace() time.Duration {
	if o.Grace > 0 {
		return o.Grace
	}
	return DefaultGrace
}

// killGroup kills every process of the group; a group already gone is not an error.
func killGroup(pgid int) error {
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill process group %d: %w", pgid, err)
	}
	return nil
}

func startError(name string, err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		return &NotInstalledError{Name: name}
	}
	return fmt.Errorf("run %s: %w", name, err)
}

// sessionStatus is the child's exit as Run reports it: nil, an *ExitError, or why Wait failed.
func sessionStatus(c Cmd, res Result, waitErr error) (Result, error) {
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) {
		if waitErr != nil {
			return res, fmt.Errorf("run %s: %w", c.Name, waitErr)
		}
		return res, nil
	}
	res.ExitCode = exitErr.ExitCode()
	e := &ExitError{Name: c.Name, Code: res.ExitCode, Stderr: string(res.Stderr)}
	if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		e.Signal = status.Signal()
	}
	return res, e
}

// sessionRun is what a started session run is waiting for: the child's exit and the end of its
// output.
type sessionRun struct {
	exited, drained, fed chan error
	waiting, copying     bool
	waitErr, copyErr     error
}

// await takes the exit and the end of the output as they come, until both are in (true) or stop
// is closed first (false).
func (r *sessionRun) await(stop <-chan struct{}) bool {
	for r.waiting || r.copying {
		select {
		case r.waitErr = <-r.exited:
			r.waiting = false
		case r.copyErr = <-r.drained:
			r.copying = false
		case <-stop:
			return false
		}
	}
	return true
}

// sessionPipes are the pipes a session run owns: the child's ends, closed here once it has
// started, this process's output ends, and the stdin it writes.
type sessionPipes struct {
	childEnds []*os.File
	readers   []*os.File
	copies    []func() error
	stdin     *os.File
	source    io.Reader
	// mu orders feed's close of stdin before stopFeeding's deadline on it: a deadline on a
	// closed file is an error that names no exported sentinel.
	mu          sync.Mutex
	stdinClosed bool
}

// wire gives the child its stdin, stdout and stderr.
func (p *sessionPipes) wire(cmd *exec.Cmd, c Cmd, out, errOut *bytes.Buffer) error {
	switch in := c.Stdin.(type) {
	case nil:
	case *os.File:
		cmd.Stdin = in
	default:
		r, w, err := os.Pipe()
		if err != nil {
			return fmt.Errorf("make the stdin pipe for %s: %w", c.Name, err)
		}
		p.childEnds = append(p.childEnds, r)
		p.stdin, p.source = w, in
		cmd.Stdin = r
	}
	stdout, err := p.output(c.Stdout, out)
	if err != nil {
		return fmt.Errorf("make the stdout pipe for %s: %w", c.Name, err)
	}
	cmd.Stdout = stdout
	stderr, err := p.output(c.Stderr, errOut)
	if err != nil {
		return fmt.Errorf("make the stderr pipe for %s: %w", c.Name, err)
	}
	cmd.Stderr = stderr
	return nil
}

// output is the child's end of one output stream: a given file as it is, else a pipe copied to
// the given writer or, with none, to the capture.
func (p *sessionPipes) output(given io.Writer, capture *bytes.Buffer) (*os.File, error) {
	if f, ok := given.(*os.File); ok {
		return f, nil
	}
	dst := pick(given, capture)
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	p.childEnds = append(p.childEnds, w)
	p.readers = append(p.readers, r)
	p.copies = append(p.copies, func() error {
		if _, err := io.Copy(dst, r); err != nil && !errors.Is(err, os.ErrDeadlineExceeded) {
			return fmt.Errorf("read the output: %w", err)
		}
		return nil
	})
	return w, nil
}

// closeChildEnds closes this process's copies of the child's pipe ends, so the child's exit (and
// its children's) is what ends the output.
func (p *sessionPipes) closeChildEnds() error {
	var errs []error
	for _, f := range p.childEnds {
		if err := f.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close a pipe: %w", err))
		}
	}
	p.childEnds = nil
	return errors.Join(errs...)
}

// closeUnstarted closes the child's ends and the stdin write end of a run that never started,
// which no feed will ever close.
func (p *sessionPipes) closeUnstarted() error {
	err := p.closeChildEnds()
	if p.stdin == nil {
		return err
	}
	if closeErr := p.stdin.Close(); closeErr != nil {
		err = errors.Join(err, fmt.Errorf("close the stdin pipe: %w", closeErr))
	}
	p.stdinClosed = true
	return err
}

// closeReaders closes the output ends once nothing reads them.
func (p *sessionPipes) closeReaders() error {
	var errs []error
	for _, r := range p.readers {
		if err := r.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close an output pipe: %w", err))
		}
	}
	return errors.Join(errs...)
}

// copyAll copies every output stream until each reaches end of file.
func (p *sessionPipes) copyAll() error {
	errs := make([]error, len(p.copies))
	var wg sync.WaitGroup
	for i, copyOne := range p.copies {
		wg.Go(func() { errs[i] = copyOne() })
	}
	wg.Wait()
	return errors.Join(errs...)
}

// abandon ends the copies that still wait on output a process outside the group holds.
func (p *sessionPipes) abandon() error {
	var errs []error
	for _, r := range p.readers {
		if err := r.SetReadDeadline(time.Now()); err != nil {
			errs = append(errs, fmt.Errorf("stop reading an output pipe: %w", err))
		}
	}
	return errors.Join(errs...)
}

// feed writes the stdin source to the child and closes it, so the child sees its end. A child
// that ended without reading it all is not an error, and neither is stopFeeding's deadline.
func (p *sessionPipes) feed() error {
	if p.stdin == nil {
		return nil
	}
	_, err := io.Copy(p.stdin, p.source)
	p.mu.Lock()
	closeErr := p.stdin.Close()
	p.stdinClosed = true
	p.mu.Unlock()
	if err != nil && !errors.Is(err, syscall.EPIPE) && !errors.Is(err, os.ErrDeadlineExceeded) {
		return fmt.Errorf("write the stdin: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close the stdin: %w", closeErr)
	}
	return nil
}

// stopFeeding ends a stdin write still blocked on a pipe nobody reads, and waits for the feed.
func (p *sessionPipes) stopFeeding(fed <-chan error) error {
	p.mu.Lock()
	var err error
	if p.stdin != nil && !p.stdinClosed {
		err = p.stdin.SetWriteDeadline(time.Now())
	}
	p.mu.Unlock()
	if err != nil {
		return errors.Join(fmt.Errorf("stop the stdin: %w", err), <-fed)
	}
	return <-fed
}
