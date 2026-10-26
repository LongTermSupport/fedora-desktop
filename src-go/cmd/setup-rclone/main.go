// Command setup-rclone configures rclone remotes, vaults the configuration into host_vars,
// edits the rclone mounts and offers to run the rclone play.
package main

import (
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	os.Exit(int(run(os.Args[1:])))
}

func run(_ []string) term.ExitCode { return term.ExitFailure }
