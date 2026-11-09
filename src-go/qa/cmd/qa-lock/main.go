// Command qa-lock runs one command under the QA lock, at low priority: the way a QA entry point
// that is not itself a Go command takes the lock (scripts/qa-go-qa.bash runs go-qa under it).
// Run inside qa-all or qa-changed, which already hold the lock, it inherits their lock and
// waits for nothing. It is a QA tool, not deployed anywhere; scripts/lib/go-tool.bash builds it.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/emit"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/qalock"
)

const (
	exitCannotRun   = 2
	exitInterrupted = 130
)

const usageText = `usage: qa-lock [--no-wait] --name NAME --root DIR -- COMMAND [ARG...]

Runs COMMAND under the QA lock of the repository at DIR (QA never runs in parallel), named NAME
in the lock's record, at nice 19 and ionice idle. While another QA run holds the lock this
waits, naming that run on stderr once and then every minute; inside a QA run that holds it,
it waits for nothing. COMMAND's exit code is qa-lock's.

  --no-wait       exit 2 at once, naming the holder, if another QA run holds the lock
  --name NAME     what the lock's record calls this run (e.g. qa-go-qa)
  --root DIR      the checkout whose repository's lock is taken
  -h, --help      this text

QA_LOCK_TIMEOUT   the longest wait as a Go duration (default: as long as the lock is held).
QA_FULL_PRIORITY  1 runs at the priority started with.

Exit codes: COMMAND's own; 2 bad usage, the lock not taken, or COMMAND not run to an exit;
130 interrupted.
`

func main() {
	if err := proc.OSPriority().Ensure(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: qa-lock: %v\n", err)
		os.Exit(exitCannotRun)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], osDeps())
	stop()
	os.Exit(code)
}

// deps is what a run touches: the lock, the process seam and the streams.
type deps struct {
	taker  func(name string) qalock.Taker
	runner proc.Runner
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

func osDeps() deps {
	return deps{
		taker:  func(name string) qalock.Taker { return qalock.OSTaker(name, os.Stderr) },
		runner: proc.OS{},
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
	}
}

// options is the parsed command line.
type options struct {
	help, noWait bool
	name, root   string
	command      []string
}

func run(ctx context.Context, args []string, d deps) int {
	if cmdmain.RefuseClosedStdout() {
		return exitCannotRun
	}
	out, errOut := emit.New(d.stdout), emit.New(d.stderr)
	return emit.Finish(out, errOut, held(ctx, args, d, out, errOut), exitCannotRun)
}

// held is a run after the stdout check: parse, take the lock, run the command, let go.
func held(ctx context.Context, args []string, d deps, out, errOut *emit.Emitter) int {
	opts, err := parseArgs(args)
	if err != nil {
		errOut.Line("ERROR: qa-lock: " + err.Error())
		errOut.Block(usageText)
		return exitCannotRun
	}
	if opts.help {
		out.Block(usageText)
		return 0
	}
	release, err := d.taker(opts.name)(ctx, opts.root, opts.noWait)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return exitInterrupted
		}
		errOut.Line(fmt.Sprintf("ERROR: %s: %v", opts.name, err))
		return exitCannotRun
	}
	code := runCommand(ctx, d, opts, errOut)
	if err := release(); err != nil {
		errOut.Line(fmt.Sprintf("ERROR: %s: cannot release the QA lock: %v", opts.name, err))
		return exitCannotRun
	}
	return code
}

// runCommand runs the command with this process's streams and returns its exit code.
func runCommand(ctx context.Context, d deps, opts options, errOut *emit.Emitter) int {
	res, err := d.runner.Run(ctx, proc.Cmd{Argv: opts.command, Stdin: d.stdin, StdoutTo: d.stdout, StderrTo: d.stderr})
	switch {
	case err != nil:
		errOut.Line(fmt.Sprintf("ERROR: %s: cannot run %s: %v", opts.name, opts.command[0], err))
		return exitCannotRun
	case ctx.Err() != nil:
		return exitInterrupted
	case res.Code < 0:
		errOut.Line(fmt.Sprintf("ERROR: %s: %s was killed by a signal", opts.name, opts.command[0]))
		return exitCannotRun
	}
	return res.Code
}

// parseArgs reads the command line: the flags, then "--" and the command.
func parseArgs(args []string) (options, error) {
	var opts options
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; arg {
		case "-h", "--help":
			opts.help = true
			return opts, nil
		case "--no-wait":
			opts.noWait = true
		case "--name", "--root":
			if i+1 >= len(args) || args[i+1] == "" || args[i+1][0] == '-' {
				return options{}, fmt.Errorf("%s needs a value", arg)
			}
			i++
			if arg == "--name" {
				opts.name = args[i]
			} else {
				opts.root = args[i]
			}
		case "--":
			opts.command = args[i+1:]
			return opts, opts.complete()
		default:
			return options{}, fmt.Errorf("unknown argument '%s'", arg)
		}
	}
	return options{}, errors.New("no command: give it after --")
}

// complete is whether a parsed command line names everything a run needs.
func (o options) complete() error {
	switch {
	case o.name == "":
		return errors.New("--name is required")
	case o.root == "":
		return errors.New("--root is required")
	case len(o.command) == 0:
		return errors.New("no command after --")
	}
	return nil
}
