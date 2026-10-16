// Command qa-go-deps is the Plan 00204 gate on src-go's dependencies: every module allowlisted
// with its reason, nothing unpinned or replaced without a row, go.sum verified, go.mod tidy and
// src-go/vendor in step. It is a QA tool, not deployed anywhere; scripts/qa-go-deps.bash builds
// and runs it.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/godeps"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if cmdmain.RefuseClosedStdout() {
		return int(godeps.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(godeps.ExitCannotRun)
	}
	return int(godeps.Main(args, newEnv(cwd)))
}

// newEnv is the real world both commands run in.
func newEnv(cwd string) godeps.Env {
	return godeps.Env{
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Cwd:     cwd,
		Resolve: filepath.EvalSymlinks,
		Runner:  proc.OS{},
		TempDir: func() (string, error) { return os.MkdirTemp("", "qa-go-deps-") },
	}
}
