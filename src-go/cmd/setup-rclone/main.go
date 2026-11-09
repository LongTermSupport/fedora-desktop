// Command setup-rclone configures rclone remotes, vaults the configuration into host_vars,
// edits the rclone mounts and offers to run the rclone play.
package main

import (
	"context"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/setuprclone"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	os.Exit(int(run(os.Args[1:])))
}

func run(args []string) term.ExitCode {
	ctx, stop := execx.SignalContext(context.Background())
	defer stop()
	return setuprclone.App{Env: setuprclone.NewEnv()}.Run(ctx, args)
}
