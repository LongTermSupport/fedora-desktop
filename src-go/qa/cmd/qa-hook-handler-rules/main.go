// Command qa-hook-handler-rules is the gate for the rules in .semgrep/hook-handlers.yml: code the
// hooks daemon loads (the project's handlers in .claude/hooks/handlers/) that resolves a path
// against the daemon's working directory. It is a QA tool, not deployed anywhere;
// scripts/qa-hook-handler-rules.bash builds and runs it. The engine is the sttrules package's,
// run with the HookHandlers spec.
package main

import (
	"fmt"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/sttrules"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if cmdmain.RefuseClosedStdout() {
		return int(sttrules.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(sttrules.ExitCannotRun)
	}
	env := sttrules.Env{Stdout: os.Stdout, Stderr: os.Stderr, Cwd: cwd, Runner: proc.OS{}}
	return int(sttrules.HookHandlers.Main(args, env))
}
