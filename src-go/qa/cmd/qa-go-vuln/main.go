// Command qa-go-vuln is the Plan 00204 gate that src-go's code reaches no known vulnerability:
// the qa module's pinned govulncheck over src-go, which reads src-go/vendor. It is a QA tool, not
// deployed anywhere; scripts/qa-go-vuln.bash builds and runs it.
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
	return int(godeps.MainVuln(args, godeps.Env{
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Cwd:     cwd,
		Resolve: filepath.EvalSymlinks,
		Runner:  proc.OS{},
	}))
}
