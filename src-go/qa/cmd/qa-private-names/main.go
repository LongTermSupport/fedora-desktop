// Command qa-private-names is the guard against install-specific names (agent teams, seats,
// containers, hosts, client projects) reaching this public repository (Plan 00245 Phase 3). It is
// a QA tool, not deployed anywhere; scripts/qa-private-names.bash builds and runs it for the
// pre-commit and pre-push hooks and for the QA gate.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/privatenames"
)

// containerRoot is where a ccy container mounts the checkout (CLAUDE/ContainerRules.md).
const containerRoot = "/workspace"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if cmdmain.RefuseClosedStdout() {
		return int(privatenames.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(privatenames.ExitCannotRun)
	}
	env := privatenames.Env{
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		Stdin:     os.Stdin,
		Cwd:       cwd,
		Run:       proc.OS{},
		Container: inContainer(cwd),
		Hostnames: hostnames(),
		Context:   os.Getenv("PRIVATE_NAMES_CONTEXT") != "",
		ReadFile:  os.ReadFile,
		WriteFile: writePrivate,
	}
	return int(privatenames.Main(args, env))
}

// inContainer is whether the checkout is the container's /workspace, or a work tree under it.
func inContainer(cwd string) bool {
	return cwd == containerRoot || strings.HasPrefix(cwd, containerRoot+"/")
}

// hostnames are this machine's name (a container's own id) and the host's, when the launcher says it.
func hostnames() []string {
	var names []string
	if h, err := os.Hostname(); err == nil {
		names = append(names, h)
	}
	if h := os.Getenv("CCY_HOST_HOSTNAME"); h != "" {
		names = append(names, h)
	}
	return names
}

// writePrivate writes the list for its owner alone: the terms are the one thing in the checkout
// that must not be read by others.
func writePrivate(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
