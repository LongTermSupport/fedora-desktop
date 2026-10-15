// Command qa-version-pins is the Plan 00109 gate on vars/version-pins.yml: the manifest is well
// formed and every pin in it still names a file that declares its var. It is a QA tool, not
// deployed anywhere; scripts/qa-version-pins.bash builds and runs it.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/versionpins"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(versionpins.ExitCannotRun)
	}
	env := versionpins.Env{
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Cwd:     cwd,
		Resolve: filepath.EvalSymlinks,
		Open:    os.DirFS,
	}
	return int(versionpins.Main(args, env))
}
