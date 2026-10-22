// Command qa-hook-handlers runs the tests of the project's hooks-daemon handlers under the
// daemon's own interpreter with the project's pytest. It is a QA tool, not deployed anywhere;
// scripts/qa-hook-handlers.bash builds and runs it.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/hookhandlers"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if cmdmain.RefuseClosedStdout() {
		return int(hookhandlers.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(hookhandlers.ExitCannotRun)
	}
	return int(hookhandlers.Main(args, hookhandlers.Env{
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Cwd:     cwd,
		Resolve: filepath.EvalSymlinks,
		Getenv:  os.Getenv,
		IsDir:   isDir,
		Runner:  proc.OS{},
	}))
}

// isDir is whether path is a directory, following symlinks.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
