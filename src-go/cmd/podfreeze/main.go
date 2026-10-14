// Command podfreeze freezes (pauses) and thaws (unpauses) Podman containers: one, a whole
// network, or every CCY session at once.
package main

import (
	"io"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/podfreeze"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/srcdigest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// SourceDigest identifies the source this binary was built from, set at build time with the
// -X main.SourceDigest=<hex> linker flag that tasks/build-go-command.yml adds; `--version`
// prints it, and the deployed-drift check compares it with the checkout's.
var SourceDigest string

func main() {
	os.Exit(int(execute(os.Args[1:], os.Stdout, os.Stderr)))
}

// execute answers --version and otherwise runs the tool.
func execute(args []string, stdout, stderr io.Writer) term.ExitCode {
	return srcdigest.Run(args, SourceDigest, stdout, stderr, run)
}

func run(args []string) term.ExitCode {
	return podfreeze.NewApp().Run(args)
}
