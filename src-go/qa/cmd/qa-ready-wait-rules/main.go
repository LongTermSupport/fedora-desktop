// Command qa-ready-wait-rules is the gate for the rule ready-wait-ignores-child-exit: a loop that
// sleeps while it waits for a process this code started and never asks whether that process has
// exited. It is a QA tool, not deployed anywhere; scripts/qa-ready-wait-rules.bash builds and
// runs it.
package main

import (
	"fmt"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/readywait"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if cmdmain.RefuseClosedStdout() {
		return int(readywait.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(readywait.ExitCannotRun)
	}
	env := readywait.Env{Stdout: os.Stdout, Stderr: os.Stderr, Cwd: cwd, Runner: proc.OS{}}
	return int(readywait.Main(args, env))
}
