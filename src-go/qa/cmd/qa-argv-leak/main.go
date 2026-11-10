// Command qa-argv-leak is the secret-in-argv gate: the semgrep rules of
// .semgrep/argv-leak.yml, proven against their fixture, applied to every file git would
// publish. It is a QA tool, not deployed anywhere; scripts/qa-argv-leak.bash builds and runs
// it. The engine is the ghscopes package's, run with the ArgvLeak spec.
package main

import (
	"fmt"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/ghscopes"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if cmdmain.RefuseClosedStdout() {
		return int(ghscopes.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(ghscopes.ExitCannotRun)
	}
	return int(ghscopes.ArgvLeak.Main(args, ghscopes.Env{Stdout: os.Stdout, Stderr: os.Stderr, Cwd: cwd, Runner: proc.OS{}}))
}
