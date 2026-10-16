// Command qa-hidden-bindings is the gate for the rule project_hidden_bindings outside Go: an
// action reachable only by a key, in a shell fzf call or a Textual app. It is a QA tool, not
// deployed anywhere; scripts/qa-hidden-bindings.bash builds and runs it.
package main

import (
	"fmt"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/hiddenbindings"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if cmdmain.RefuseClosedStdout() {
		return int(hiddenbindings.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(hiddenbindings.ExitCannotRun)
	}
	env := hiddenbindings.Env{Stdout: os.Stdout, Stderr: os.Stderr, Cwd: cwd, Runner: proc.OS{}}
	return int(hiddenbindings.Main(args, env))
}
