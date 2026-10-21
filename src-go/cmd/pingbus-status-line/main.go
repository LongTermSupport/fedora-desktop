// Command pingbus-status-line is `pingbus status-line` (Plan 00213): the session's Claude Code
// status line, with the agent-bus teams the session is on. The flow is internal/pingbus/statusline.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/statusline"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// inputLimit is the most stdin the status line reads: one byte past the 1 MiB it accepts (S1).
const inputLimit = 1<<20 + 1

func main() {
	// Subscribed before stdin is read, so a cancel that arrives early is not lost (S12).
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	os.Exit(int(run(os.Stdin, os.Stdout, os.Stderr, os.Environ(), os.Getwd, signals)))
}

// run renders the status line. A cancel by signal exits 128 plus the signal with nothing on
// stdout (S12); every other render exits 0 (S3): a stdin that cannot be read is no payload and
// a working directory that cannot be had is an empty one.
func run(stdin io.Reader, stdout, stderr io.Writer, environ []string, getwd func() (string, error),
	signals <-chan os.Signal,
) term.ExitCode {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	done := make(chan struct{})
	defer close(done)
	go relaySignals(signals, done, cancel)
	raw, ok := readInput(ctx, stdin)
	if !ok {
		return term.ExitCode(statusline.CancelStatus(ctx))
	}
	cwd, err := getwd()
	if err != nil {
		cwd = ""
	}
	out, code := statusline.Run(ctx, statusline.Input{Env: envMap(environ), Cwd: cwd, Stdin: raw}, stderr)
	if code != 0 {
		return term.ExitCode(code)
	}
	if _, err := stdout.Write(out); err != nil {
		return fail(stderr, "cannot write the line", err)
	}
	return term.ExitOK
}

// readInput is stdin, read beside the cancel so a signal during the read still ends the render
// (S12). A read that fails is no payload. False means the render was cancelled first.
func readInput(ctx context.Context, stdin io.Reader) ([]byte, bool) {
	got := make(chan []byte, 1)
	go func() {
		raw, err := io.ReadAll(io.LimitReader(stdin, inputLimit))
		if err != nil {
			raw = nil
		}
		got <- raw
	}()
	select {
	case raw := <-got:
		return raw, true
	case <-ctx.Done():
		return nil, false
	}
}

// fail says what went wrong on stderr and returns the failure status. A stderr that cannot be
// written to leaves nothing else to report, so its own error is not reported again.
func fail(stderr io.Writer, what string, err error) term.ExitCode {
	if _, werr := fmt.Fprintf(stderr, "pingbus status-line: %s: %v\n", what, err); werr != nil {
		return term.ExitFailure
	}
	return term.ExitFailure
}

// relaySignals turns the first signal into the render's cancel, until the render is over.
func relaySignals(signals <-chan os.Signal, done <-chan struct{}, cancel context.CancelCauseFunc) {
	for {
		select {
		case <-done:
			return
		case sig := <-signals:
			if s, ok := sig.(syscall.Signal); ok {
				cancel(statusline.Cancel(s))
				return
			}
		}
	}
}

// envMap is the environment as the status line reads it: KEY=VALUE pairs, the first one of a
// name winning, as Python's os.environ keeps it.
func envMap(environ []string) map[string]string {
	env := make(map[string]string, len(environ))
	for _, kv := range environ {
		name, value, found := strings.Cut(kv, "=")
		if _, seen := env[name]; found && !seen {
			env[name] = value
		}
	}
	return env
}
