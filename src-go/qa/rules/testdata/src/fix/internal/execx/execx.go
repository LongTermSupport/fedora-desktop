// Package execx is the owner: it may start processes.
package execx

import (
	"os"
	"os/exec"
	"syscall"
)

// Cmd is a command for the runner.
type Cmd struct {
	Name string
	Args []string
}

// Run starts the command; the owner is the one place allowed to.
func Run(c Cmd) error {
	_ = syscall.Exec
	_ = os.StartProcess
	return exec.Command(c.Name, c.Args...).Run()
}
