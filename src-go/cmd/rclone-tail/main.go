// Command rclone-tail watches the rclone mounts' uploads live: queue, uploads in flight,
// transfer rate and ETA.
package main

import (
	"context"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/rclonetail"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	os.Exit(int(run(os.Args[1:])))
}

func run(args []string) term.ExitCode {
	return rclonetail.App{Env: rclonetail.NewEnv()}.Run(context.Background(), args)
}
