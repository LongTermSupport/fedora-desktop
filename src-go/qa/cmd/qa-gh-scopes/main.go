// Command qa-gh-scopes is the gh-scope-outside-ssot gate: the semgrep rule of .semgrep/gh-scopes.yml,
// proven against its fixture, applied to every file git would publish. It is a QA tool, not
// deployed anywhere; scripts/qa-gh-scopes.bash builds and runs it.
package main

import (
	"fmt"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/ghscopes"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(ghscopes.ExitCannotRun)
	}
	return int(ghscopes.Main(args, ghscopes.Env{Stdout: os.Stdout, Stderr: os.Stderr, Cwd: cwd, Runner: proc.OS{}}))
}
