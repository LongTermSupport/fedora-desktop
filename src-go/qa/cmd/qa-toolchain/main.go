// Command qa-toolchain asserts that the QA tools on this machine match .qa-versions. It is a QA
// tool, not deployed anywhere; scripts/qa-toolchain.bash builds and runs it.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/toolchain"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(toolchain.ExitCannotRun)
	}
	env := toolchain.Env{
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Cwd:      cwd,
		Resolve:  filepath.EvalSymlinks,
		Open:     os.DirFS,
		LookPath: exec.LookPath,
		Runner:   proc.OS{},
	}
	return int(toolchain.Main(args, env))
}
