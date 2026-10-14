// Command go-source is the build and QA tool for the source digest of the deployed Go
// commands: `digest` prints a command's digest (tasks/build-go-command.yml bakes it in) and
// `drift` compares the installed commands with the checkout (scripts/qa-deployed-drift.bash).
// It is not deployed anywhere.
package main

import (
	"context"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/gobuild"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	return gobuild.Main(context.Background(), proc.OS{}, args, os.Stdout, os.Stderr)
}
