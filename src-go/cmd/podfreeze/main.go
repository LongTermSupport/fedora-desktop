// Command podfreeze freezes (pauses) and thaws (unpauses) Podman containers: one, a whole
// network, or every CCY session at once.
package main

import (
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/podfreeze"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	return podfreeze.NewApp().Run(args)
}
