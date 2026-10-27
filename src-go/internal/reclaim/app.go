// Package reclaim is the reclaim command: a report of what is using disk, and a menu of
// targeted cleanups, each behind a confirmation. The specification is
// CLAUDE/Plan/00192-.../SPEC-reclaim.md; the expectation numbers (E1...) and departures from
// the bash (B1...) the comments cite are its.
package reclaim

import (
	"context"
	_ "embed" // usage.txt is embedded verbatim
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sudo"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Version is the command's version (E1).
const Version = "2.0.1"

// The programs and arguments named in more than one place.
const (
	progPodman  = "podman"
	progDocker  = "docker"
	progFlatpak = "flatpak"
	progDnf     = "dnf"
	argSystem   = "system"
	duBytes     = "-B1"
)

//go:embed usage.txt
var usageText string

var errClosed = errors.New("closed")

// Env is everything a run touches outside its own memory and the home directory.
type Env struct {
	Runner execx.Runner
	// Fg runs ncdu, which takes the terminal.
	Fg execx.Foregrounder
	// Sudo runs the root commands; its Stdout is the terminal side (stderr), never stdout.
	Sudo   *sudo.Sudo
	Stdout io.Writer
	Stderr io.Writer
	// Stdin is what IsTerminal is asked of for E9.
	Stdin      any
	Getenv     func(string) string
	LookPath   func(string) (string, error)
	Hostname   func() (string, error)
	IsTerminal func(any) bool
	// OpenTTY opens the controlling terminal the menu and its questions read from.
	OpenTTY func() (io.ReadCloser, error)
	// DevNull opens /dev/null for writing: baobab's output.
	DevNull func() (*os.File, error)
	// Columns is the terminal's width for the headings, 0 when unknown.
	Columns func() int
	// VarDirs are the system directories the report sizes after the home ones (E6).
	VarDirs []string
	// RemoveAll removes one entry of ~/.cache or the trash with everything under it.
	RemoveAll func(string) error
}

// NewEnv is the production environment.
func NewEnv() Env {
	runner := execx.OS{}
	tm := term.New()
	s := sudo.New(runner, tm)
	s.Stdout = os.Stderr
	return Env{
		Runner: runner, Fg: runner, Sudo: s,
		Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin,
		Getenv: os.Getenv, LookPath: exec.LookPath, Hostname: os.Hostname,
		IsTerminal: term.FileIsTerminal,
		OpenTTY: func() (io.ReadCloser, error) {
			return os.Open("/dev/tty")
		},
		DevNull: func() (*os.File, error) {
			return os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		},
		Columns:   func() int { return term.FileColumns(os.Stderr) },
		VarDirs:   []string{"/var/log", "/var/cache"},
		RemoveAll: os.RemoveAll,
	}
}

// App is one reclaim invocation.
type App struct {
	Env Env
}

// run is the state of one invocation: the streams' first write errors, the terminal, and
// how many actions failed (E13).
type run struct {
	env    Env
	out    term.Printer
	errOut term.Printer
	colour bool
	home   string
	tty    *lineReader
	failed int
}

// Run is the whole command (E1, E2).
func (a App) Run(ctx context.Context, args []string) term.ExitCode {
	r := &run{env: a.Env, home: a.Env.Getenv("HOME")}
	r.colour = a.Env.IsTerminal(a.Env.Stderr) && a.Env.Getenv("NO_COLOR") == ""
	code := r.dispatch(ctx, args)
	if ctx.Err() != nil {
		return r.cancelled()
	}
	return r.finish(code)
}

// cancelled ends a run its context ended (Ctrl-C, or another signal SignalContext caught):
// every command it had started has finished, and nothing more runs (E30).
func (r *run) cancelled() term.ExitCode {
	r.say("")
	r.warn("Cancelled.")
	return term.ExitCancelled
}

// finished is ctx left as it is for a command that changes the machine: a cancel does not
// signal or kill it, so the run waits for it to end however the person's Ctrl-C (which the
// terminal sends it too) leaves it, and only then stops (E30).
func finished(ctx context.Context) context.Context {
	return context.WithoutCancel(ctx)
}

func (r *run) dispatch(ctx context.Context, args []string) term.ExitCode {
	if len(args) > 1 {
		return r.usageError(args[1])
	}
	arg := ""
	if len(args) == 1 {
		arg = args[0]
	}
	switch arg {
	case "":
		return r.menu(ctx)
	case "report":
		return r.report(ctx)
	case "-h", "--help":
		r.out.Print(r.env.Stdout, usageText)
		return term.ExitOK
	case "--version":
		r.out.Print(r.env.Stdout, "reclaim "+Version+"\n")
		return term.ExitOK
	default:
		return r.usageError(arg)
	}
}

func (r *run) usageError(arg string) term.ExitCode {
	r.failf("unknown argument: %s — see 'reclaim --help'.", arg)
	return term.ExitUsage
}

// finish is the exit code: an unwritable stdout or stderr fails a run that would succeed.
func (r *run) finish(code term.ExitCode) term.ExitCode {
	if code != term.ExitOK {
		return code
	}
	if err := r.out.Err(); err != nil {
		r.failf("could not write the output: %v", err)
		return term.ExitFailure
	}
	if r.errOut.Err() != nil {
		return term.ExitFailure
	}
	return code
}

// failf is an error line, `reclaim: <text>` on stderr (E2).
func (r *run) failf(format string, args ...any) {
	r.errOut.Print(r.env.Stderr, "reclaim: "+fmt.Sprintf(format, args...)+"\n")
}

// have is whether a program is on PATH.
func (r *run) have(name string) bool {
	_, err := r.env.LookPath(name)
	return err == nil
}
