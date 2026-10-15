// Command qa-python is the Python gate: every Python file git would publish must compile and pass
// ruff, and every Python template must compile once its expressions are substituted. It is a QA
// tool, not deployed anywhere; scripts/qa-python.bash builds and runs it.
package main

import (
	"fmt"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/python"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if cmdmain.RefuseClosedStdout() {
		return int(python.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(python.ExitCannotRun)
	}
	return int(python.Main(args, newEnv(cwd)))
}

// newEnv is the production environment of the gate: the real process, file system and report writer.
func newEnv(cwd string) python.Env {
	return python.Env{
		Getenv:    os.Getenv,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		Cwd:       cwd,
		Runner:    proc.OS{},
		TempDir:   os.TempDir(),
		WriteFile: cmdmain.WriteReportPrivate,
	}
}
