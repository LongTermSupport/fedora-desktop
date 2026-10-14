// Command qa-all is the full QA run, the gate before a push: every gate of scripts/qa-gates.tsv
// in order, one stage line each, a machine report and one verdict. It is a QA tool, not
// deployed anywhere; scripts/qa-all.bash builds and runs it.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/all"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(all.ExitCannotRun)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	env := all.Env{
		Getenv:   os.Getenv,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Runner:   proc.OS{},
		NewToken: newToken,
		Cwd:      cwd,
	}
	return int(all.Main(ctx, args, env))
}

// newToken names one run's counts file: the process and the instant, so a file left by another
// run or written by something that never read this run's arguments does not match.
func newToken() string {
	return fmt.Sprintf("qa-all-%d-%d", os.Getpid(), time.Now().UnixNano())
}
