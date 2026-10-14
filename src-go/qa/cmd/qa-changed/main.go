// Command qa-changed is the diff-scoped QA run while iterating: it runs only the checks the
// changed files need. It is a QA tool, not deployed anywhere; scripts/qa-changed.bash builds and
// runs it.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/changed"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(changed.ExitCannotRun)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	env := changed.Env{
		Getenv:   os.Getenv,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Runner:   proc.OS{},
		LookPath: lookPath,
		Cwd:      cwd,
	}
	return int(changed.Main(ctx, args, env))
}

// lookPath is whether a tool is on PATH.
func lookPath(tool string) error {
	if _, err := exec.LookPath(tool); err != nil {
		return fmt.Errorf("%s: %w", tool, err)
	}
	return nil
}
