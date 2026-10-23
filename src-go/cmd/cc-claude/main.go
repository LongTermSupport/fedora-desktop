// Command cc-claude is the claude host cc starts: the session environment a ccy session
// gets, then claude itself (internal/ccclaude).
package main

import (
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccclaude"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	os.Exit(int(run(ccclaude.NewApp(), os.Args[1:])))
}

func run(app *ccclaude.App, args []string) term.ExitCode {
	return app.Run(args)
}
