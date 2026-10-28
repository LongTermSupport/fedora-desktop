// Package nord is the nord command: NordVPN's OpenVPN configs imported into NetworkManager and
// connected, switched and disconnected through nmcli. The specification is
// CLAUDE/Plan/00192-.../SPEC-nord.md; the expectation numbers (E1...) and departures from the
// bash (B1...) the comments cite are its.
package nord

import (
	"context"
	_ "embed" // usage.txt is embedded verbatim
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Version is the command's version (E1).
const Version = "2.0.0"

// play sets nord up: the packages, the directories, the credentials and this command.
const play = "./playbooks/imports/optional/common/play-nordvpn-openvpn.yml"

// The files nord reads and writes, under the home directory.
const (
	configsRel     = ".config/nordvpn/configs"
	accountFileRel = ".config/nordvpn/.credentials"
	logRel         = ".local/share/nordvpn/nord.log"
)

//go:embed usage.txt
var usageText string

// Env is everything a run touches outside its own memory and the home directory.
type Env struct {
	Runner execx.Runner
	Stdout io.Writer
	Stderr io.Writer
	// Stdin is what IsTerminal is asked of for the chooser (E22).
	Stdin      any
	Getenv     func(string) string
	LookPath   func(string) (string, error)
	IsTerminal func(any) bool
	// OpenTTY opens the controlling terminal the chooser reads from.
	OpenTTY func() (io.ReadCloser, error)
	// Columns is the terminal's width for the headings, 0 when unknown.
	Columns func() int
	// Now stamps the log's lines.
	Now func() time.Time
}

// NewEnv is the production environment.
func NewEnv() Env {
	return Env{
		Runner: execx.OS{},
		Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin,
		Getenv: os.Getenv, LookPath: exec.LookPath, IsTerminal: term.FileIsTerminal,
		OpenTTY: func() (io.ReadCloser, error) {
			return os.Open("/dev/tty")
		},
		Columns: func() int { return term.FileColumns(os.Stderr) },
		Now:     time.Now,
	}
}

// App is one nord invocation.
type App struct {
	Env Env
}

// run is the state of one invocation: the streams' first write errors, the runner (with
// --debug's naming of each command), and the log once a changing command has opened it.
type run struct {
	env    Env
	out    term.Printer
	errOut term.Printer
	home   string
	debug  bool
	runner execx.Runner
	log    *runLog
}

// Run is the whole command (E1-E4, E24).
func (a App) Run(ctx context.Context, args []string) term.ExitCode {
	r := &run{env: a.Env, home: a.Env.Getenv("HOME")}
	r.runner = debugRunner{r: r, next: a.Env.Runner}
	code := r.dispatch(ctx, args)
	if ctx.Err() != nil {
		code = r.cancelled(ctx)
	}
	return r.finish(code)
}

// cancelled ends a run its context ended (Ctrl-C, or another signal SignalContext caught): every
// command that changes something it had started has finished, and nothing more runs (E24).
func (r *run) cancelled(ctx context.Context) term.ExitCode {
	r.say("")
	r.say("Cancelled.")
	if se, ok := errors.AsType[*execx.SignalError](context.Cause(ctx)); ok {
		return term.ExitCode(se.ExitCode())
	}
	return term.ExitCancelled
}

// changing is ctx left as it is for a command that changes the machine: a cancel does not signal
// or kill it, so the run waits for it to end and only then stops (E24).
func changing(ctx context.Context) context.Context {
	return context.WithoutCancel(ctx)
}

// command is how many names a command takes, and whether it changes anything (so it logs, E21).
type command struct {
	names   int
	changes bool
}

var commands = map[string]command{
	"list":        {},
	"list-active": {},
	"status":      {},
	"connect":     {names: 1, changes: true},
	"switch":      {names: 1, changes: true},
	"disconnect":  {changes: true},
	"cleanup":     {changes: true},
}

// dispatch is the command line (E1, E2).
func (r *run) dispatch(ctx context.Context, args []string) term.ExitCode {
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "--debug":
			r.debug = true
		case "-h", "--help":
			r.out.Print(r.env.Stdout, usageText)
			return term.ExitOK
		case "-v", "--version":
			r.out.Print(r.env.Stdout, "nord "+Version+"\n")
			return term.ExitOK
		default:
			return r.usageError("unknown option: " + args[0])
		}
		args = args[1:]
	}
	if len(args) == 0 {
		if !r.openLog() {
			return term.ExitFailure
		}
		return r.chooser(ctx)
	}
	name, rest := args[0], args[1:]
	cmd, ok := commands[name]
	if !ok {
		return r.usageError("unknown command: " + name)
	}
	for _, a := range rest {
		if strings.HasPrefix(a, "-") {
			return r.usageError("unknown option: " + a + " (options come before the command)")
		}
	}
	if code, ok := r.checkArity(name, cmd, rest); !ok {
		return code
	}
	if cmd.changes && !r.openLog() {
		return term.ExitFailure
	}
	return r.runCommand(ctx, name, rest)
}

// checkArity is E2's count of a command's names.
func (r *run) checkArity(name string, cmd command, rest []string) (term.ExitCode, bool) {
	switch {
	case cmd.names == 1 && len(rest) == 0:
		code := r.usageError(name + " needs a config name")
		r.listNamesToStderr()
		return code, false
	case cmd.names == 0 && len(rest) > 0:
		return r.usageError(name + " takes no argument"), false
	case len(rest) > cmd.names:
		return r.usageError(name + " takes one config name"), false
	}
	return term.ExitOK, true
}

func (r *run) runCommand(ctx context.Context, name string, rest []string) term.ExitCode {
	switch name {
	case "list":
		return r.list()
	case "list-active":
		return r.listActive(ctx)
	case "status":
		return r.status(ctx)
	case "disconnect":
		return r.disconnect(ctx)
	case "cleanup":
		return r.cleanup(ctx)
	}
	return r.connect(ctx, rest[0])
}

func (r *run) usageError(what string) term.ExitCode {
	r.failf("%s — see 'nord --help'.", what)
	return term.ExitUsage
}

// finish is the exit code: the log closed, and an unwritable stdout, stderr or log fails a run
// that would succeed (E4, E21).
func (r *run) finish(code term.ExitCode) term.ExitCode {
	if r.log != nil {
		if err := r.log.close(); err != nil {
			r.log = nil
			r.failf("could not write the log %s: %v", r.abbrev(r.path(logRel)), err)
			if code == term.ExitOK {
				code = term.ExitFailure
			}
		}
	}
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

// say is one line on stderr, logged when the log is open (E21).
func (r *run) say(text string) {
	r.errOut.Print(r.env.Stderr, text+"\n")
	if r.log != nil {
		r.log.line(text)
	}
}

// failf is an error line, `nord: <text>` on stderr (E2).
func (r *run) failf(format string, args ...any) {
	r.say("nord: " + fmt.Sprintf(format, args...))
}

// print is a line of a report on stdout (E23).
func (r *run) print(text string) {
	r.out.Print(r.env.Stdout, text+"\n")
}

func (r *run) path(rel string) string { return filepath.Join(r.home, rel) }

func (r *run) abbrev(path string) string { return term.AbbrevHome(path, r.home) }

// debugRunner names each command before it runs when --debug is given (E3).
type debugRunner struct {
	r    *run
	next execx.Runner
}

func (d debugRunner) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) {
	if d.r.debug {
		d.r.say("nord: run: " + c.Line())
	}
	res, err := d.next.Run(ctx, c)
	if err != nil {
		return res, fmt.Errorf("%s: %w", c.Name, err)
	}
	return res, nil
}
