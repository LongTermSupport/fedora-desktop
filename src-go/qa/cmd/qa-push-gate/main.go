// Command qa-push-gate is the pre-push gate on CI's QA verdict (rule push-without-qa-verdict):
// scripts/git-hooks/pre-push runs it through scripts/qa-push-gate.bash with git's arguments and
// ref lines. It is a QA tool, not deployed anywhere.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/pushgate"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if cmdmain.RefuseClosedStdout() {
		return int(pushgate.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: qa-push-gate: working directory: %v\n", err)
		return int(pushgate.ExitCannotRun)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return int(pushgate.Main(ctx, args, pushgate.Env{
		Stdin:    os.Stdin,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Runner:   proc.OS{},
		Cwd:      cwd,
		Getenv:   os.Getenv,
		ReadFile: os.ReadFile,
	}))
}
