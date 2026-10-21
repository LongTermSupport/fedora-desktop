// Command convert turns a file into the other formats its type supports, through a proven
// converter (Markdown to DOCX through pandoc first).
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/convert"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	// Ctrl-C or a TERM cancels the run: the engine is stopped and its temporary removed
	// (SPEC-convert.md E14).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:])
	stop()
	os.Exit(int(code))
}

func run(ctx context.Context, args []string) term.ExitCode {
	return convert.App{Env: convert.NewEnv()}.Run(ctx, args)
}
