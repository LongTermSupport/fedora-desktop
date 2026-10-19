package open

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/shellword"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// launch is E21-E27 and E31: the chosen candidate, or what it would run.
func (r *run) launch(ctx context.Context, c candidate, target, mime string) error {
	if r.opts.dryRun {
		r.dryRun(c, target, mime)
		return nil
	}
	if r.takeInterrupts().seen {
		return errCancelled
	}
	r.note(fmt.Sprintf("→ %s — %s", target, c.label))
	if c.gui() {
		return r.launchDesktop(ctx, c, target, mime)
	}
	if r.opts.setDefault {
		r.note(fmt.Sprintf("  Note: --set-default applies to GUI apps only; '%s' is a terminal handler.", c.label))
	}
	return r.runHandler(ctx, c)
}

// dryRun is E21, on stdout.
func (r *run) dryRun(c candidate, target, mime string) {
	if !c.gui() {
		line := shellword.Join(c.argv)
		if c.pager != nil {
			line += " | " + shellword.Join(c.pager)
		}
		r.out.Print(r.env.Stdout, line+"\n")
		return
	}
	if r.opts.setDefault {
		r.out.Print(r.env.Stdout, shellLine(registerCmd(c, mime))+"\n")
	}
	r.out.Print(r.env.Stdout, shellLine(launchCmd(c, target))+"\n")
}

func shellLine(c execx.Cmd) string { return shellword.Join(append([]string{c.Name}, c.Args...)) }

// registerCmd makes c's application the default for mime.
func registerCmd(c candidate, mime string) execx.Cmd {
	return execx.Cmd{Name: gioProg, Args: []string{"mime", mime, c.id}}
}

// launchCmd starts c's application on target; its stdin is /dev/null (E23).
func launchCmd(c candidate, target string) execx.Cmd {
	return execx.Cmd{Name: gioProg, Args: []string{"launch", c.desktopFile, target}}
}

// launchDesktop is E23: the default registered first when asked, then gio launch.
func (r *run) launchDesktop(ctx context.Context, c candidate, target, mime string) error {
	if r.opts.setDefault {
		register := registerCmd(c, mime)
		register.Stderr = r.env.Stderr
		if err := r.step(ctx, register); err != nil {
			if errors.Is(err, errCancelled) {
				return err
			}
			return fail("could not set %s as the default for %s: %v", c.id, mime, err)
		}
		r.note(fmt.Sprintf("  Registered %s as the default for %s.", c.id, mime))
	}
	launch := launchCmd(c, target)
	launch.Stdout, launch.Stderr = r.env.Stdout, r.env.Stderr
	if _, err := r.env.Runner.Run(ctx, launch); err != nil {
		// A failure the person's interrupt caused is a cancel; a launch that succeeded leaves
		// the interrupt for the next step (E31).
		if r.interruptedStep(err) {
			return errCancelled
		}
		return fail("gio launch failed: %v", err)
	}
	return nil
}

// runHandler is E24-E27: the terminal handler on this process's streams, its stdout fed to its
// pager when it has one. Its program is the one the person chose, so its name is not a constant
// (a dynamicNames row in src-go/qa/rules/table.go).
func (r *run) runHandler(ctx context.Context, c candidate) error {
	cmd := execx.Cmd{Name: c.argv[0], Args: c.argv[1:], Stdin: r.env.Stdin, Stdout: r.env.Stdout, Stderr: r.env.Stderr}
	if c.pager == nil {
		_, err := r.env.Runner.Run(ctx, cmd)
		return handlerResult(c.label, err, r.takeInterrupts().sigint)
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return fail("could not make the pipe to the pager: %v", err)
	}
	var producerDone atomic.Bool
	produced := make(chan error, 1)
	cmd.Stdin, cmd.Stdout = nil, pw
	go func() {
		_, runErr := r.env.Runner.Run(ctx, cmd)
		// Marked before the pipe closes, so a pager that ends on the EOF this close gives it
		// always finds the producer finished.
		producerDone.Store(true)
		produced <- errors.Join(runErr, closeErr("the pager pipe", pw.Close()))
	}()
	pager := execx.Cmd{Name: c.pager[0], Args: c.pager[1:], Stdin: pr, Stdout: r.env.Stdout, Stderr: r.env.Stderr}
	_, pagerErr := r.env.Runner.Run(ctx, pager)
	producerFirst := producerDone.Load()
	// The pager has ended: a producer still writing gets EPIPE from here on and stops.
	readErr := closeErr("the pager pipe", pr.Close())
	producerErr := <-produced
	sigint := r.takeInterrupts().sigint
	switch {
	case pagerErr != nil:
		return handlerResult(filepath.Base(c.pager[0]), pagerErr, sigint)
	case producerFirst && producerErr != nil:
		return handlerResult(c.label, producerErr, sigint)
	}
	if readErr != nil {
		return fail("%v", readErr)
	}
	return nil
}

// handlerResult is E25, E27, E29 and E30 for one finished program, named name; sigint is the
// person's Ctrl-C arriving while it ran.
func handlerResult(name string, err error, sigint bool) error {
	if err == nil {
		return nil
	}
	if exit, ok := errors.AsType[*execx.ExitError](err); ok {
		switch {
		case exit.Signal == syscall.SIGINT || sigint:
			return errHandlerInterrupted
		case exit.Signal != 0:
			return &failure{
				code: term.ExitCode(128 + int(exit.Signal)),
				msg:  fmt.Sprintf("%s killed by %s", name, unix.SignalName(exit.Signal)),
			}
		}
		return fail("%s exited %d", name, exit.Code)
	}
	if notInstalled, ok := errors.AsType[*execx.NotInstalledError](err); ok {
		return fail("%s is not installed", filepath.Base(notInstalled.Name))
	}
	return fail("%s: %v", name, err)
}

// errHandlerInterrupted is a handler the person ended with Ctrl-C: a cancel the handler's own
// ending has already shown, so open adds nothing (E29).
var errHandlerInterrupted = &failure{code: term.ExitCancelled, msg: "the handler was interrupted", quiet: true}

// caught is what takeInterrupts found: any signal, and whether a SIGINT was among them.
type caught struct {
	seen, sigint bool
}

// takeInterrupts empties the caught signals.
func (r *run) takeInterrupts() caught {
	var got caught
	for {
		select {
		case s := <-r.env.Interrupts:
			got.seen = true
			got.sigint = got.sigint || s == os.Interrupt
		default:
			return got
		}
	}
}

// step runs one of open's own programs before a handler has the terminal (a type detector,
// gio mime, a registration): an interrupt while it ran is errCancelled (E31).
func (r *run) step(ctx context.Context, c execx.Cmd) error {
	_, err := r.env.Runner.Run(ctx, c)
	if r.interruptedStep(err) {
		return errCancelled
	}
	return err
}

// interruptedStep is an interrupt caught while a step ran, or the step killed by one (the
// terminal signals its whole foreground group, so the step can end before open sees it).
func (r *run) interruptedStep(err error) bool {
	got := r.takeInterrupts()
	var sig syscall.Signal
	if exit, ok := errors.AsType[*execx.ExitError](err); ok {
		sig = exit.Signal
	}
	return got.seen || sig == syscall.SIGINT || sig == syscall.SIGQUIT
}

func closeErr(what string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("close %s: %w", what, err)
}
