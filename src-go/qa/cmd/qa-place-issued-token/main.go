// Command qa-place-issued-token is the token-bundle-into-place-issued gate: the semgrep rule of
// .semgrep/place-issued-token.yml, proven against its fixture, applied to every file git would
// publish. It is a QA tool, not deployed anywhere; scripts/qa-place-issued-token.bash builds and
// runs it. The engine is the ghscopes package's, run with the PlaceIssuedToken spec.
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
	return int(ghscopes.PlaceIssuedToken.Main(args, ghscopes.Env{Stdout: os.Stdout, Stderr: os.Stderr, Cwd: cwd, Runner: proc.OS{}}))
}
