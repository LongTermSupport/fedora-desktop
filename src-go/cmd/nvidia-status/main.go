// Command nvidia-status reports whether the NVIDIA driver installed by play-nvidia.yml is built,
// loaded and working. It only reads, so a Ctrl-C ends it at once with nothing to put back.
package main

import (
	"context"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/nvidiastatus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	os.Exit(int(run(os.Args[1:])))
}

func run(args []string) term.ExitCode {
	return nvidiastatus.App{Env: nvidiastatus.NewEnv()}.Run(context.Background(), args)
}
