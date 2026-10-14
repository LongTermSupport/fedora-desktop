// Command lxcfreeze freezes and thaws LXC containers: one, a whole bridge, or all of them.
package main

import (
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/lxcfreeze"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	os.Exit(int(run(os.Args[1:])))
}

func run(args []string) term.ExitCode {
	return lxcfreeze.NewApp().Run(args)
}
