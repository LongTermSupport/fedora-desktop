// Package ttyecho holds a terminal's echo off between the screens of an interactive command, so
// keys typed between them are not echoed. A screen (fzf, gum, a Bubble Tea program) takes the
// terminal raw and gives back the mode it found when it ends; until the next screen takes it, the
// line discipline echoes what is typed, and an arrow key pressed while the command works (reads
// the machine, runs a play) shows as ^[[B on the top line of the next screen. Held, the keys are
// not echoed and stay in the input for the next screen to read.
//
// The hold is taken when the first screen runs (an EchoHeld child: ui marks every fzf, gum and
// tput it runs), so a run that shows no screen (a listing piped to a pager, a job in the
// background) never touches the terminal; keys typed before the first screen still echo. It is the mode found with only ECHO cleared. A child
// given the terminal for its own input (a play, tmux attach, sudo's prompt, a shell's read) runs
// in the mode found, and the echo is off again when the last such child ends; a line this
// process reads itself is read through Given, with the echo on. The process replaced by Exec gets
// the mode found. Held puts the mode back when the run ends, a panic included; SIGTERM and SIGHUP
// (unless ignored) put it back before they end the process.
package ttyecho

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Modes reads and sets a terminal's mode.
type Modes interface {
	Get(fd int) (*unix.Termios, error)
	Set(fd int, t *unix.Termios) error
}

// ioctlModes is the terminal's own mode, through the termios ioctls. TCSETS takes effect at
// once and keeps what has been typed.
type ioctlModes struct{}

func (ioctlModes) Get(fd int) (*unix.Termios, error) {
	t, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return nil, fmt.Errorf("TCGETS: %w", err)
	}
	return t, nil
}

func (ioctlModes) Set(fd int, t *unix.Termios) error {
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, t); err != nil {
		return fmt.Errorf("TCSETS: %w", err)
	}
	return nil
}

// Hold is the echo of one terminal, held off from the first screen to the end of the run. A nil
// Hold holds nothing. Every field is a seam a test replaces.
type Hold struct {
	TTY        *os.File
	IsTerminal func(v any) bool
	Modes      Modes
	// Notify, StopNotify and Ignored are signal.Notify, signal.Stop and signal.Ignored; Raise
	// sends the signal again to this process once the mode is back.
	Notify     func(c chan<- os.Signal, sigs ...os.Signal)
	StopNotify func(c chan<- os.Signal)
	Ignored    func(sig os.Signal) bool
	Raise      func(sig os.Signal) error
	// Stderr is where a mode that could not be put back on a signal or a panic is said.
	Stderr io.Writer

	mu sync.Mutex
	// found is the mode found when the hold was taken and quiet the same with the echo off; nil
	// when not held.
	found, quiet *unix.Termios
	// given is how many children are running with the terminal given to them.
	given int
	stop  chan struct{}
	done  chan struct{}
	out   term.Printer
}

// New is a hold on tty, the terminal the command reads its keys from (its stdin).
func New(tty *os.File) *Hold {
	return &Hold{
		TTY: tty, IsTerminal: term.FileIsTerminal, Modes: ioctlModes{},
		Notify: signal.Notify, StopNotify: signal.Stop, Ignored: signal.Ignored, Raise: raise, Stderr: os.Stderr,
	}
}

// raise sends sig to this process again, its own handling stopped, so it ends as it would have.
func raise(sig os.Signal) error {
	s, ok := sig.(syscall.Signal)
	if !ok {
		return fmt.Errorf("raise %v: not a system signal", sig)
	}
	if err := syscall.Kill(os.Getpid(), s); err != nil {
		return fmt.Errorf("raise %v: %w", sig, err)
	}
	return nil
}

// Held runs run, then puts back the mode the hold found, if a screen took it; when run panics
// the mode is put back first and the panic goes on. The error is a mode that could not be put
// back.
func (h *Hold) Held(run func()) error {
	if h == nil {
		run()
		return nil
	}
	defer h.releaseOnPanic()
	run()
	return h.release()
}

// Given runs read, a read of a line in this process (a numbered menu), with the echo on.
func (h *Hold) Given(read func() error) error {
	if h == nil {
		return read()
	}
	if err := h.give(); err != nil {
		return fmt.Errorf("give the terminal's echo back: %w", err)
	}
	err := read()
	if takeErr := h.take(); takeErr != nil {
		// The terminal is the failure: the read's own end (an EOF read as "no") is named but not
		// wrapped, so no caller takes it for the answer.
		if err != nil {
			return fmt.Errorf("turn the terminal's echo off: %w (after the read: %s)", takeErr, err.Error())
		}
		return fmt.Errorf("turn the terminal's echo off: %w", takeErr)
	}
	return err
}

// Runner is r with the hold taken before the first screen, and every child given the terminal
// (its stdin) run in the mode found.
func (h *Hold) Runner(r execx.Runner) execx.Runner {
	if h == nil {
		return r
	}
	return heldRunner{hold: h, next: r}
}

// Execer is x with the hold released before the process is replaced: the program that takes
// its place owns the terminal.
func (h *Hold) Execer(x execx.Execer) execx.Execer {
	if h == nil {
		return x
	}
	return heldExecer{hold: h, next: x}
}

// heldRunner is a Runner under a hold.
type heldRunner struct {
	hold *Hold
	next execx.Runner
}

func (x heldRunner) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) {
	if c.EchoHeld {
		if err := x.hold.engage(); err != nil {
			return execx.Result{}, fmt.Errorf("hold the terminal's echo off for %s: %w", c.Name, err)
		}
		return x.next.Run(ctx, c)
	}
	if !x.hold.IsTerminal(c.Stdin) {
		return x.next.Run(ctx, c)
	}
	if err := x.hold.give(); err != nil {
		return execx.Result{}, fmt.Errorf("give the terminal's echo back for %s: %w", c.Name, err)
	}
	res, err := x.next.Run(ctx, c)
	if takeErr := x.hold.take(); takeErr != nil {
		return res, errors.Join(err, fmt.Errorf("turn the terminal's echo off after %s: %w", c.Name, takeErr))
	}
	return res, err
}

// heldExecer is an Execer under a hold.
type heldExecer struct {
	hold *Hold
	next execx.Execer
}

func (x heldExecer) Exec(c execx.Cmd) error {
	if err := x.hold.release(); err != nil {
		return fmt.Errorf("before %s: %w", c.Name, err)
	}
	return x.next.Exec(c)
}

// engage takes the hold for a screen, once: the mode found kept, the echo turned off. Not a
// terminal is nothing to do.
func (h *Hold) engage() error {
	if h.TTY == nil || !h.IsTerminal(h.TTY) {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.found != nil {
		return nil
	}
	fd := int(h.TTY.Fd())
	found, err := h.Modes.Get(fd)
	if errors.Is(err, unix.ENOTTY) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read the terminal's mode: %w", err)
	}
	quiet := *found
	quiet.Lflag &^= unix.ECHO
	if err := h.Modes.Set(fd, &quiet); err != nil {
		return fmt.Errorf("turn the terminal's echo off: %w", err)
	}
	h.found, h.quiet = found, &quiet
	h.watch()
	return nil
}

// release puts back the mode found and stops watching the signals. Not held is nothing to do.
func (h *Hold) release() error {
	h.mu.Lock()
	stop := h.stop
	h.stop = nil
	err := h.restore()
	h.mu.Unlock()
	if stop != nil {
		close(stop)
	}
	return err
}

// releaseOnPanic is Held's way out of a panic: the mode back, then the panic goes on.
func (h *Hold) releaseOnPanic() {
	p := recover()
	if p == nil {
		return
	}
	if err := h.release(); err != nil {
		h.out.Print(h.Stderr, "ERROR: "+err.Error()+"; run reset to see what you type.\n")
	}
	panic(p)
}

// watch puts the mode back when SIGTERM or SIGHUP ends the process, until release. A signal this
// process was started ignoring (SIGHUP under nohup) is left ignored.
func (h *Hold) watch() {
	var watched []os.Signal
	for _, sig := range []os.Signal{syscall.SIGTERM, syscall.SIGHUP} {
		if !h.Ignored(sig) {
			watched = append(watched, sig)
		}
	}
	if len(watched) == 0 {
		// Notify with no signal would catch every one, the runtime's own among them.
		return
	}
	sigs := make(chan os.Signal, 1)
	h.Notify(sigs, watched...)
	stop, done := make(chan struct{}), make(chan struct{})
	h.stop, h.done = stop, done
	go func() {
		defer close(done)
		select {
		case sig := <-sigs:
			h.StopNotify(sigs)
			h.mu.Lock()
			h.stop = nil
			err := h.restore()
			h.mu.Unlock()
			if err != nil {
				h.out.Print(h.Stderr, "ERROR: "+err.Error()+"; run reset to see what you type.\n")
			}
			if err := h.Raise(sig); err != nil {
				h.out.Print(h.Stderr, "ERROR: "+err.Error()+"\n")
			}
		case <-stop:
			h.StopNotify(sigs)
		}
	}()
}

// restore sets the mode found and ends the hold; h.mu is held.
func (h *Hold) restore() error {
	if h.found == nil {
		return nil
	}
	found := h.found
	h.found, h.quiet, h.given = nil, nil, 0
	if err := h.Modes.Set(int(h.TTY.Fd()), found); err != nil {
		return fmt.Errorf("put the terminal's mode back: %w", err)
	}
	return nil
}

// give sets the mode found for a child given the terminal; the first of them sets it.
func (h *Hold) give() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.found == nil {
		return nil
	}
	if h.given == 0 {
		if err := h.Modes.Set(int(h.TTY.Fd()), h.found); err != nil {
			return err
		}
	}
	h.given++
	return nil
}

// take turns the echo off again once the last child given the terminal has ended.
func (h *Hold) take() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.found == nil || h.given == 0 {
		return nil
	}
	h.given--
	if h.given > 0 {
		return nil
	}
	return h.Modes.Set(int(h.TTY.Fd()), h.quiet)
}
