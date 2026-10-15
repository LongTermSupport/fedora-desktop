// Command qa-speech-to-text-rules is the gate for the rules in .semgrep/speech-to-text.yml: a
// model judged present without its weights, and a dropdown label that explains itself. It is a
// QA tool, not deployed anywhere; scripts/qa-speech-to-text-rules.bash builds and runs it.
package main

import (
	"fmt"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/sttrules"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if cmdmain.RefuseClosedStdout() {
		return int(sttrules.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(sttrules.ExitCannotRun)
	}
	env := sttrules.Env{Stdout: os.Stdout, Stderr: os.Stderr, Cwd: cwd, Runner: proc.OS{}}
	return int(sttrules.Main(args, env))
}
