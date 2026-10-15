// Command qa-ansible-syntax is the Ansible syntax gate: every playbook the repository holds must
// pass ansible-playbook --syntax-check. It is a QA tool, not deployed anywhere;
// scripts/qa-ansible-syntax.bash builds and runs it.
package main

import (
	"fmt"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/ansiblesyntax"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if cmdmain.RefuseClosedStdout() {
		return int(ansiblesyntax.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(ansiblesyntax.ExitCannotRun)
	}
	return int(ansiblesyntax.Main(args, newEnv(cwd)))
}

// newEnv is the production environment of the gate: the real process, file system and report writer.
func newEnv(cwd string) ansiblesyntax.Env {
	return ansiblesyntax.Env{
		Getenv:    os.Getenv,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		Cwd:       cwd,
		Runner:    proc.OS{},
		WriteFile: cmdmain.WriteReport,
	}
}
