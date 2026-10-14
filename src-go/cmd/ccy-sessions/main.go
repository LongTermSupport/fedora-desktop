// Command ccy-sessions lists every CCY and cc session, attaches, takes over or ends one, warns
// them all before a reboot, and brings them back after one.
package main

import (
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sessions"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	os.Exit(int(run(os.Args[1:])))
}

func run(args []string) term.ExitCode {
	return sessions.NewApp().Run(args)
}
