// Command branch-delete-list turns a candidate list of origin's branch names into the delete
// commands for the owner to run, or refuses it (src-go/qa/branchdelete). It is not deployed;
// scripts/branch-delete-list.bash builds and runs it.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/branchdelete"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if cmdmain.RefuseClosedStdout() {
		return int(branchdelete.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(branchdelete.ExitCannotRun)
	}
	env := branchdelete.Env{
		Stdin:    os.Stdin,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Cwd:      cwd,
		Eval:     filepath.EvalSymlinks,
		Git:      proc.OS{},
		ReadFile: os.ReadFile,
	}
	return int(branchdelete.Main(args, env))
}
