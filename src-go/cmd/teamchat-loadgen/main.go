// Command teamchat-loadgen is Plan 00268's load generator: a test team on a lab homeserver and
// teamchat's traffic sent into it. A test tool, run from the checkout and never deployed; the
// flow and its target rule are internal/teamchat/loadgen.
package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/loadgen"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(int(code))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) term.ExitCode {
	return loadgen.Main(ctx, args, loadgen.Env{Stdout: stdout, Stderr: stderr, Clock: clock.System()})
}
