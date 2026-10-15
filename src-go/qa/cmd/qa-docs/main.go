// Command qa-docs is the documentation integrity gate: links, anchors, the playbook catalogue, the
// topic index and the gate inventory. It is a QA tool, not deployed anywhere;
// scripts/qa-docs.bash builds and runs it.
package main

import (
	"fmt"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/docs"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if cmdmain.RefuseClosedStdout() {
		return int(docs.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(docs.ExitCannotRun)
	}
	return int(docs.Main(args, newEnv(cwd)))
}

// newEnv is the production environment of the gate: the real process, file system and report writer.
func newEnv(cwd string) docs.Env {
	return docs.Env{
		Getenv:    os.Getenv,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		Cwd:       cwd,
		Runner:    proc.OS{},
		ReadFile:  os.ReadFile,
		Stat:      os.Stat,
		Lstat:     os.Lstat,
		WriteFile: cmdmain.WriteReport,
	}
}
