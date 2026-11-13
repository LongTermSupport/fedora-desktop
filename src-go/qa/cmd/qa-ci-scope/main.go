// Command qa-ci-scope decides how much QA a CI run of .github/workflows/qa.yml does: the full
// suite or qa-changed's diff mode against a protected line's tip (src-go/qa/ciscope). The
// workflow runs it through scripts/qa-ci-scope.bash. It is a QA tool, not deployed anywhere.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/ciscope"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if cmdmain.RefuseClosedStdout() {
		return int(ciscope.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: qa-ci-scope: working directory: %v\n", err)
		return int(ciscope.ExitCannotRun)
	}
	return runIn(args, os.Stdout, os.Stderr, cwd)
}

func runIn(args []string, stdout, stderr io.Writer, cwd string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return int(ciscope.Main(ctx, args, ciscope.Env{
		Stdout:   stdout,
		Stderr:   stderr,
		Runner:   proc.OS{},
		Cwd:      cwd,
		ReadFile: os.ReadFile,
	}))
}
