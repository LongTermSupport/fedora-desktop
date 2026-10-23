package execx

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"sync"
	"syscall"
)

// ExitStatus is how a started child ended: its status as a shell reports it (128+n for a child
// killed by signal n, Signal set).
type ExitStatus struct {
	Code   int
	Signal syscall.Signal
}

// Started is a child that runs beside its parent until it is waited for: a supervisor's child,
// which the parent signals and whose end it learns by wait4. Go's os.Process holds a pidfd for
// it, so neither Signal nor Wait can reach a process that later took the same pid.
type Started interface {
	Pid() int
	// Wait waits for the child once and is its status from then on.
	Wait() (ExitStatus, error)
	// Signal sends sig to the child; a child that has already ended needs none and is nil.
	Signal(sig syscall.Signal) error
}

// Starter starts a child and returns at once.
type Starter interface {
	Start(c Cmd) (Started, error)
}

// Start implements Starter. A nil stream is /dev/null, never this process's: a child that is
// to share the terminal is given it. NewSession runs the child in a session of its own
// (setsid), so no terminal signal of this process's job reaches it.
func (OS) Start(c Cmd) (Started, error) {
	if err := errors.Join(c.check(), c.checkSecrets(), c.refuseFeed()); err != nil {
		return nil, err
	}
	cmd := exec.Command(c.Name, c.Args...)
	cmd.SysProcAttr = c.sysProcAttr(c.NewSession)
	cmd.Dir = c.Dir
	if env, ok := c.childEnv(); ok {
		cmd.Env = env
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = c.Stdin, c.Stdout, c.Stderr
	cmd.ExtraFiles = c.ExtraFiles
	if err := cmd.Start(); err != nil {
		return nil, startError(c.Name, err)
	}
	return &started{cmd: cmd}, nil
}

type started struct {
	cmd    *exec.Cmd
	once   sync.Once
	status ExitStatus
	err    error
}

func (s *started) Pid() int { return s.cmd.Process.Pid }

func (s *started) Wait() (ExitStatus, error) {
	s.once.Do(func() {
		waitErr := s.cmd.Wait()
		if waitErr == nil {
			return
		}
		exitErr, ok := errors.AsType[*exec.ExitError](waitErr)
		if !ok {
			s.err = fmt.Errorf("wait for %s: %w", s.cmd.Path, waitErr)
			return
		}
		s.status.Code = exitErr.ExitCode()
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			s.status = ExitStatus{Code: 128 + int(ws.Signal()), Signal: ws.Signal()}
		}
	})
	return s.status, s.err
}

func (s *started) Signal(sig syscall.Signal) error {
	if err := s.cmd.Process.Signal(sig); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("signal %s: %w", s.cmd.Path, err)
	}
	return nil
}

type startRule struct {
	prefix []string
	fn     func(Cmd) (Started, error)
}

// OnStart answers every Start whose argv starts with prefix by calling fn. Later rules win over
// earlier ones. fn runs with the Fake locked, so it must not call back into it.
func (f *Fake) OnStart(prefix []string, fn func(Cmd) (Started, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startRules = append(f.startRules, startRule{prefix: prefix, fn: fn})
}

// Start implements Starter for tests: it records the command and answers it from OnStart's
// rules; an unscripted one is ErrUnexpected.
func (f *Fake) Start(c Cmd) (Started, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts = append(f.starts, c)
	if err := errors.Join(c.check(), c.checkSecrets(), c.refuseFeed()); err != nil {
		return nil, err
	}
	for _, r := range slices.Backward(f.startRules) {
		if hasPrefix(c.argv(), r.prefix) {
			return r.fn(c)
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrUnexpected, c.Line())
}

// Starts is every command handed to Start so far, in order.
func (f *Fake) Starts() []Cmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Cmd(nil), f.starts...)
}
