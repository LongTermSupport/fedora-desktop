// Command qa-bash-limits is the QA gate for the rule "tooling is Go; bash only for very small
// helpers": it holds every bash file in the repository to the limits in src-go/CLAUDE.md, with
// a ratchet allowlist for the files already over them. It is a QA tool, not deployed anywhere;
// scripts/qa-bash-limits.bash runs it.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/bashlimits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ bash-limits: working directory: %v\n", err)
		return bashlimits.ExitCannotRun
	}
	app := &bashlimits.App{Runner: execx.OS{}, Out: os.Stdout, Err: os.Stderr}
	return app.Run(context.Background(), cwd, args)
}
