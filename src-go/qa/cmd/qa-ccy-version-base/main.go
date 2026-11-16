// Command qa-ccy-version-base names the commit the pre-commit hook judges a ccy version bump
// against (issue #298); package ccyversionbase is the specification.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/ccyversionbase"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if cmdmain.RefuseClosedStdout() {
		return int(ccyversionbase.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: qa-ccy-version-base: working directory: %v\n", err)
		return int(ccyversionbase.ExitCannotRun)
	}
	return runIn(args, os.Stdout, os.Stderr, cwd)
}

func runIn(args []string, stdout, stderr io.Writer, cwd string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return int(ccyversionbase.Main(ctx, args, ccyversionbase.Env{
		Stdout:   stdout,
		Stderr:   stderr,
		Runner:   proc.OS{},
		Cwd:      cwd,
		ReadFile: os.ReadFile,
	}))
}
