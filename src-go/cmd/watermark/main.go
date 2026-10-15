// Command watermark puts a visible watermark and commercial-licence metadata on one image.
package main

import (
	"context"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/watermark"
)

func main() {
	os.Exit(int(run(os.Args[1:])))
}

func run(args []string) term.ExitCode {
	return watermark.App{Env: watermark.NewEnv()}.Run(context.Background(), args)
}
