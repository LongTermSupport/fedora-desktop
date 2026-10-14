// Command qa-plan-script-logging is the Plan 00130 gate: no active plan script sends its own output
// to a process substitution, and no active plan folder keeps a logs/ directory. It is a QA tool,
// not deployed anywhere; scripts/qa-plan-script-logging.bash builds and runs it.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/planscriptlogging"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(planscriptlogging.ExitCannotRun)
	}
	env := planscriptlogging.Env{
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Cwd:     cwd,
		Resolve: filepath.EvalSymlinks,
		Open:    os.DirFS,
	}
	return int(planscriptlogging.Main(args, env))
}
