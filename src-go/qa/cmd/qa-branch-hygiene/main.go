// Command qa-branch-hygiene judges the repository's remote branches against
// scripts/branch-policy.toml. It is a QA tool, not deployed anywhere; scripts/qa-branch-hygiene.bash
// builds and runs it, and .github/workflows/branch-hygiene.yml is its only caller in CI.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/branchhygiene"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if cmdmain.RefuseClosedStdout() {
		return int(branchhygiene.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(branchhygiene.ExitCannotRun)
	}
	env := branchhygiene.Env{
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Cwd:      cwd,
		Eval:     filepath.EvalSymlinks,
		Now:      time.Now,
		Git:      proc.OS{},
		ReadFile: os.ReadFile,
		Getenv:   os.Getenv,
	}
	return int(branchhygiene.Main(args, env))
}
