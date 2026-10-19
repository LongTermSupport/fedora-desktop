// Package open is the open command: one command to open a file, a directory or a URL, on a
// desktop or over SSH. It opens the registered default when there is one, and asks (fzf, or a
// numbered menu) when it is not sure; with no graphical session it offers only terminal
// handlers that are installed. The specification is CLAUDE/Plan/00192-.../SPEC-open.md; the
// expectation numbers (E1...) and departures from the bash (B1...) the comments cite are its.
package open

import (
	"context"
	_ "embed" // usage.txt is embedded verbatim
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

//go:embed usage.txt
var usageText string

// Picker is the fzf chooser; *ui.Picker is the production one.
type Picker interface {
	Pick(ctx context.Context, h ui.Heading, s ui.PickSpec) (ui.Picked, error)
}

// Env is everything a run touches outside its own memory and the paths it is given.
type Env struct {
	Runner execx.Runner
	Picker Picker
	// Stdin, Stdout and Stderr are this process's: a terminal handler runs on them.
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	Getenv   func(string) string
	LookPath func(string) (string, error)
	// Readable is access(2) with R_OK: nil when this user may read the path.
	Readable func(string) error
	// IsTerminal reports whether a stream is a terminal (asked of Stderr).
	IsTerminal func(any) bool
	// OpenTTY opens the controlling terminal the numbered menu reads from.
	OpenTTY func() (io.ReadCloser, error)
	// Interrupts is SIGINT and SIGQUIT, caught so a handler owns them while it runs (E26, E29)
	// and anything before the handler is cancelled by them (E19, E31).
	Interrupts <-chan os.Signal
}

// NewEnv is the production environment; sig is the caught interrupts.
func NewEnv(sig <-chan os.Signal) Env {
	plain := os.Getenv("NO_COLOR") != "" || !term.FileIsTerminal(os.Stderr)
	return Env{
		Runner: execx.OS{},
		Picker: &ui.Picker{Runner: execx.OS{}, Stderr: os.Stderr, Plain: plain},
		Stdin:  os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		Getenv: os.Getenv, LookPath: exec.LookPath,
		Readable:   func(p string) error { return unix.Access(p, unix.R_OK) },
		IsTerminal: term.FileIsTerminal,
		OpenTTY: func() (io.ReadCloser, error) {
			return os.Open("/dev/tty")
		},
		Interrupts: sig,
	}
}

// App is one open invocation.
type App struct {
	Env Env
}

// failure ends a run with code after printing "open: <msg>", or printing nothing when quiet.
type failure struct {
	code  term.ExitCode
	msg   string
	quiet bool
}

func (f *failure) Error() string { return f.msg }

func fail(format string, args ...any) error {
	return &failure{code: term.ExitFailure, msg: fmt.Sprintf(format, args...)}
}

// errCancelled is the person cancelling a choice (E17-E19).
var errCancelled = &failure{code: term.ExitCancelled, msg: "cancelled — nothing opened."}

// run is the state of one invocation.
type run struct {
	env    Env
	opts   options
	out    term.Printer
	errOut term.Printer
	// apps is the desktop-entry index, built on first use (E9).
	apps map[string]string
	tty  *ttyReader
}

// Run is one invocation; its exit code is section 4's.
func (a App) Run(ctx context.Context, args []string) term.ExitCode {
	r := a.newRun()
	err := r.main(ctx, args)
	if cerr := r.closeTTY(); cerr != nil && err == nil {
		err = fail("%v", cerr)
	}
	code := r.result(err)
	if code == term.ExitOK && r.out.Err() != nil {
		r.say("could not write the output: " + r.out.Err().Error())
		return term.ExitFailure
	}
	return code
}

func (a App) newRun() *run { return &run{env: a.Env} }

func (r *run) result(err error) term.ExitCode {
	f, ok := errors.AsType[*failure](err)
	switch {
	case err == nil:
		return term.ExitOK
	case ok:
		if !f.quiet {
			r.say(f.msg)
		}
		return f.code
	default:
		r.say(err.Error())
		return term.ExitFailure
	}
}

func (r *run) say(msg string) { r.errOut.Print(r.env.Stderr, "open: "+msg+"\n") }

// note is a line for the person on stderr, not an error.
func (r *run) note(msg string) { r.errOut.Print(r.env.Stderr, msg+"\n") }

func (r *run) main(ctx context.Context, args []string) error {
	opts, err := parseArgs(args)
	if opts.help {
		r.out.Print(r.env.Stdout, usageText)
		return nil
	}
	if err != nil {
		return err
	}
	r.opts = opts
	for i, target := range opts.targets {
		if err := r.openTarget(ctx, target, i+1, len(opts.targets)); err != nil {
			return err
		}
	}
	return nil
}

// openTarget is E4-E27 for one target, the nth of total.
func (r *run) openTarget(ctx context.Context, target string, nth, total int) error {
	target, mime, err := r.resolve(ctx, target)
	if err != nil {
		return err
	}
	cands, confident, err := r.candidates(ctx, target, mime)
	if err != nil {
		return err
	}
	if r.opts.list {
		r.list(target, mime, cands)
		return nil
	}
	if len(cands) == 0 {
		return fail("no handler available for '%s' (%s). Install a viewer, or run 'open --list'.", target, mime)
	}
	chosen := 0
	if r.opts.ask || !confident {
		if chosen, err = r.choose(ctx, target, mime, nth, total, cands); err != nil {
			return err
		}
	}
	return r.launch(ctx, cands[chosen], target, mime)
}

// candidates is E7-E15: the desktop apps in a graphical session, then the terminal handlers,
// and whether the first may be opened without asking.
func (r *run) candidates(ctx context.Context, target, mime string) ([]candidate, bool, error) {
	var cands []candidate
	hasDefault := false
	if r.graphical() {
		gui, def, err := r.desktopCandidates(ctx, mime)
		if err != nil {
			return nil, false, err
		}
		cands, hasDefault = gui, def
	}
	terms, known := r.terminalCandidates(mime, target)
	confident := hasDefault || (len(cands) == 0 && known && len(terms) > 0)
	return append(cands, terms...), confident, nil
}

// graphical is E7.
func (r *run) graphical() bool {
	if r.opts.terminal {
		return false
	}
	return r.env.Getenv("WAYLAND_DISPLAY") != "" || r.env.Getenv("DISPLAY") != ""
}

// list is E20, on stdout.
func (r *run) list(target, mime string, cands []candidate) {
	r.out.Print(r.env.Stdout, fmt.Sprintf("%s (%s)\n", target, mime))
	if len(cands) == 0 {
		r.out.Print(r.env.Stdout, "  (no handler available)\n")
		return
	}
	for i, c := range cands {
		r.out.Print(r.env.Stdout, fmt.Sprintf("  %2d) %-42s %s\n", i+1, c.label, c.kind()))
	}
}
