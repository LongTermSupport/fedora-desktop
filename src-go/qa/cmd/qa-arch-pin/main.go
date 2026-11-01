// Command qa-arch-pin is the arch-pinned-artefact gate: the semgrep rule of .semgrep/arch-pin.yml,
// proven against its fixture, applied to every file git would publish and judged against the
// ratchet scripts/arch-pin.allowlist. It is a QA tool, not deployed anywhere;
// scripts/qa-arch-pin.bash builds and runs it.
package main

import (
	"fmt"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/archpin"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if cmdmain.RefuseClosedStdout() {
		return int(archpin.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(archpin.ExitCannotRun)
	}
	return int(archpin.Main(args, archpin.Env{Stdout: os.Stdout, Stderr: os.Stderr, Cwd: cwd, Runner: proc.OS{}}))
}
