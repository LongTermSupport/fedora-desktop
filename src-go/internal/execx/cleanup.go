package execx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// Cleanups is a launch's cleanup stack: what it staged (key copies, a temporary git config, an
// agent) is pushed as it is staged and undone last first. Go has no EXIT trap, so a main runs
// it from a defer, after SignalContext has ended the flow on TERM, HUP, INT or QUIT, and before
// replacing the process (Exec). Each step runs once; a failed step does not stop the rest.
// The zero value is ready; safe for concurrent use.
type Cleanups struct {
	mu    sync.Mutex
	steps []cleanupStep
}

type cleanupStep struct {
	what string
	fn   func() error
}

// Push adds a step, named for its error, to run before every step pushed earlier.
func (c *Cleanups) Push(what string, fn func() error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.steps = append(c.steps, cleanupStep{what: what, fn: fn})
}

// Run runs and removes every step, last pushed first, and joins their errors.
func (c *Cleanups) Run() error {
	c.mu.Lock()
	steps := c.steps
	c.steps = nil
	c.mu.Unlock()
	var errs []error
	for _, step := range slices.Backward(steps) {
		if err := step.fn(); err != nil {
			errs = append(errs, fmt.Errorf("clean up %s: %w", step.what, err))
		}
	}
	return errors.Join(errs...)
}

// Exec runs the cleanups, then replaces the process with cmd: the relaunch inherits no staged
// state. A failed cleanup refuses the exec.
func (c *Cleanups) Exec(e Execer, cmd Cmd) error {
	if err := c.Run(); err != nil {
		return fmt.Errorf("not starting %s: %w", cmd.Name, err)
	}
	return e.Exec(cmd)
}

// SignalError is the signal that ended a SignalContext.
type SignalError struct {
	Signal syscall.Signal
}

func (e *SignalError) Error() string { return "received signal " + unix.SignalName(e.Signal) }

// ExitCode is the shell's status for a process ended by the signal: 128+n.
func (e *SignalError) ExitCode() int { return 128 + int(e.Signal) }

// SignalContext is parent, ended with a *SignalError cause when this process receives SIGTERM,
// SIGHUP, SIGINT or SIGQUIT, instead of the process dying with its cleanups pending. stop
// stops listening and ends the context.
func SignalContext(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT)
	stopped := make(chan struct{})
	go func() {
		select {
		case sig := <-sigs:
			if s, ok := sig.(syscall.Signal); ok {
				cancel(&SignalError{Signal: s})
			}
		case <-stopped:
		}
	}()
	var once sync.Once
	return ctx, func() {
		once.Do(func() {
			signal.Stop(sigs)
			close(stopped)
			cancel(context.Canceled)
		})
	}
}
