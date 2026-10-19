// Command cc-project-dir prints the directory host cc starts its session in: the top of the
// git work tree it was run in, or, offered and accepted, the fedora-desktop checkout.
package main

import (
	"context"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccproject"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// RootDir is the fedora-desktop checkout the binary was built from, set at build time with
// -ldflags "-X main.RootDir=<checkout>": it is the directory offered outside a repository.
var RootDir string

func main() {
	os.Exit(int(run(os.Args[1:], RootDir)))
}

func run(args []string, root string) term.ExitCode {
	return ccproject.NewApp(root).Run(context.Background(), args)
}
