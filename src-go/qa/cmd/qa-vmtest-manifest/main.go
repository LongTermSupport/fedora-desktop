// Command qa-vmtest-manifest checks the VM-test scenario manifest, vars/vm-test-scenarios.yml,
// against the host's validator and the guest scripts. It is a QA tool, not deployed anywhere;
// scripts/qa-vmtest-manifest.bash builds and runs it.
package main

import (
	"fmt"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/vmtestmanifest"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if cmdmain.RefuseClosedStdout() {
		return int(vmtestmanifest.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(vmtestmanifest.ExitCannotRun)
	}
	env := vmtestmanifest.Env{Stdout: os.Stdout, Stderr: os.Stderr, Cwd: cwd, Runner: proc.OS{}}
	return int(vmtestmanifest.Main(args, env))
}
