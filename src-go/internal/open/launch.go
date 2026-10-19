package open

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/shellword"
)

// launch is E21-E27: the chosen candidate, or what it would run.
func (r *run) launch(ctx context.Context, c candidate, target, mime string) error {
	if r.opts.dryRun {
		r.dryRun(c, target, mime)
		return nil
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
		if _, err := r.env.Runner.Run(ctx, register); err != nil {
			return fail("could not set %s as the default for %s: %v", c.id, mime, err)
		}
		r.note(fmt.Sprintf("  Registered %s as the default for %s.", c.id, mime))
	}
	launch := launchCmd(c, target)
	launch.Stdout, launch.Stderr = r.env.Stdout, r.env.Stderr
	if _, err := r.env.Runner.Run(ctx, launch); err != nil {
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
		return r.handlerResult(c, err)
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
	if pagerErr != nil {
		return r.handlerResult(c, pagerErr)
	}
	if producerFirst {
		if err := r.handlerResult(c, producerErr); err != nil {
			return err
		}
	}
	if readErr != nil {
		return fail("%v", readErr)
	}
	return nil
}

// handlerResult is E25 and E27 for one finished program.
func (r *run) handlerResult(c candidate, err error) error {
	if err == nil {
		return nil
	}
	if exit, ok := errors.AsType[*execx.ExitError](err); ok {
		return fail("%s exited %d", c.label, exit.Code)
	}
	if notInstalled, ok := errors.AsType[*execx.NotInstalledError](err); ok {
		return fail("%s is not installed", filepath.Base(notInstalled.Name))
	}
	return fail("%s: %v", c.label, err)
}

func closeErr(what string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("close %s: %w", what, err)
}
