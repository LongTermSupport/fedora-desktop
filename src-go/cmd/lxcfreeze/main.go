// Command lxcfreeze freezes and thaws LXC containers: one, a whole bridge, or all of them.
package main

import (
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/lxcfreeze"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	return lxcfreeze.NewApp().Run(args)
}
