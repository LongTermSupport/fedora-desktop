// Command qa-patterns is the pattern gate for repository-owned shell scripts: the semgrep rules
// of .semgrep/bash-conventions.yml, proven against their fixture, applied to every shell script
// git would publish. It is a QA tool, not deployed anywhere; scripts/qa-patterns.bash builds and
// runs it.
package main

import (
	"fmt"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/patterns"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(patterns.ExitCannotRun)
	}
	env := patterns.Env{
		Getenv:    os.Getenv,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		Cwd:       cwd,
		Runner:    proc.OS{},
		TempDir:   os.TempDir(),
		WriteFile: cmdmain.WriteReportPrivate,
	}
	return int(patterns.Main(args, env))
}
