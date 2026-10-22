package execx

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"syscall"
)

// Exit is how a foreground child ended: its exit status as a shell reports it (128+n for a
// child killed by signal n, Signal set), and the signals this process received while it ran.
type Exit struct {
	Code     int
	Signal   syscall.Signal
	Received []os.Signal
}

// Foregrounder runs a child in the terminal's foreground and waits for it: the container a
// launch starts (`podman run -it`), whose exit status the launcher reads.
type Foregrounder interface {
	Foreground(c Cmd) (Exit, error)
}

// Foreground implements Foregrounder. The child inherits this process's stdin, stdout and
// stderr (unless c sets them) and stays in its process group, the terminal's foreground group,
// so the terminal's Ctrl-C and Ctrl-\ reach it directly. While it runs, this process:
//
//   - takes SIGINT and SIGQUIT and does not forward them: the terminal has already sent them
//     to the child, and this process must outlive the child to clean up after it;
//   - forwards SIGTERM and SIGHUP, which are sent to this process alone (a reboot, a closed
//     tmux pane), to the child, and waits for it.
//
// The error is only a child that could not be started; a child that ran reports in Exit.
//
// Go hands a signal to every channel registered for it, so a SignalContext live around
// Foreground also sees the SIGINT this process outlives, and its context ends. A caller that
// reports the child's status must read it from Exit, not from that context's cause, which
// would make a Ctrl-C the child handled exit 130. With `podman run -it` a raw-mode terminal
// turns Ctrl-C into a byte, so no SIGINT is sent at all.
func (OS) Foreground(c Cmd) (Exit, error) {
	if err := errors.Join(c.checkSecrets(), c.refuseFeed()); err != nil {
		return Exit{}, err
	}
	cmd := exec.Command(c.Name, c.Args...)
	cmd.Dir = c.Dir
	if env, ok := c.childEnv(); ok {
		cmd.Env = env
	}
	cmd.Stdin = inherit(c.Stdin, os.Stdin)
	cmd.Stdout = inheritOut(c.Stdout, os.Stdout)
	cmd.Stderr = inheritOut(c.Stderr, os.Stderr)
	cmd.ExtraFiles = c.ExtraFiles
	sigs := make(chan os.Signal, 8)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)
	if err := cmd.Start(); err != nil {
		return Exit{}, startError(c.Name, err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var received []os.Signal
	var forwardErr error
	for {
		select {
		case sig := <-sigs:
			received = append(received, sig)
			if sig == syscall.SIGTERM || sig == syscall.SIGHUP {
				if err := cmd.Process.Signal(sig); err != nil && !errors.Is(err, os.ErrProcessDone) {
					forwardErr = errors.Join(forwardErr, fmt.Errorf("forward %v to %s: %w", sig, c.Name, err))
				}
			}
		case waitErr := <-done:
			exit, err := exitOf(c.Name, waitErr, received)
			return exit, errors.Join(err, forwardErr)
		}
	}
}

// inherit is the child's stdin: the one given, else this process's.
func inherit(given io.Reader, std *os.File) io.Reader {
	if given == nil {
		return std
	}
	return given
}

// inheritOut is one of the child's output streams: the one given, else this process's.
func inheritOut(given io.Writer, std *os.File) io.Writer {
	if given == nil {
		return std
	}
	return given
}

// exitOf reads a finished foreground child's status.
func exitOf(name string, waitErr error, received []os.Signal) (Exit, error) {
	exit := Exit{Received: received}
	if waitErr == nil {
		return exit, nil
	}
	exitErr, ok := errors.AsType[*exec.ExitError](waitErr)
	if !ok {
		return exit, fmt.Errorf("wait for %s: %w", name, waitErr)
	}
	exit.Code = exitErr.ExitCode()
	if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		exit.Signal = status.Signal()
		exit.Code = 128 + int(exit.Signal)
	}
	return exit, nil
}

// Foreground implements Foregrounder for tests: it records the command and answers it from
// the same rules as Run, the rule's ExitCode being the child's.
func (f *Fake) Foreground(c Cmd) (Exit, error) {
	if err := errors.Join(c.checkSecrets(), c.refuseFeed()); err != nil {
		return Exit{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.foregrounds = append(f.foregrounds, c)
	r, ok := f.match(c)
	if !ok {
		return Exit{}, fmt.Errorf("%w: %s", ErrUnexpected, c.Line())
	}
	if r.err != nil {
		return Exit{}, r.err
	}
	res := r.result
	if r.fn != nil {
		var err error
		if res, err = r.fn(c); err != nil {
			return Exit{}, err
		}
	}
	return Exit{Code: res.ExitCode}, nil
}

// Foregrounds is every command handed to Foreground so far, in order.
func (f *Fake) Foregrounds() []Cmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Cmd(nil), f.foregrounds...)
}

// match is the latest rule whose prefix c's argv starts with; f.mu must be held.
func (f *Fake) match(c Cmd) (rule, bool) {
	argv := c.argv()
	for _, r := range slices.Backward(f.rules) {
		if hasPrefix(argv, r.prefix) {
			return r, true
		}
	}
	return rule{}, false
}
