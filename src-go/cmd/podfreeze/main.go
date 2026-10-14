// Command podfreeze freezes (pauses) and thaws (unpauses) Podman containers: one, a whole
// network, or every CCY session at once.
package main

import (
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/podfreeze"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	os.Exit(int(run(os.Args[1:])))
}

func run(args []string) term.ExitCode {
	return podfreeze.NewApp().Run(args)
}
