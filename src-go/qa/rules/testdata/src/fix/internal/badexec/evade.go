package badexec

import (
	"os/exec"
	"syscall"
)

// The evasions: every one starts a process without a call to exec.Command spelled out.

func funcValue() {
	f := exec.Command // want `exec.Command outside internal/execx`
	use(f("sh", "-c", "id"))
}

func funcValueAsArgument() {
	use(exec.CommandContext) // want `exec.CommandContext outside internal/execx`
}

func use(any) {}

func structLiteral() error {
	c := &exec.Cmd{Path: "/bin/sh"} // want `exec.Cmd outside internal/execx`
	return c.Run()                  // want `\(\*exec.Cmd\).Run outside internal/execx`
}

func newCmd() error {
	c := new(exec.Cmd) // want `exec.Cmd outside internal/execx`
	return c.Start()   // want `\(\*exec.Cmd\).Start outside internal/execx`
}

func output(c *exec.Cmd) ([]byte, error) { // want `exec.Cmd outside internal/execx`
	return c.Output() // want `\(\*exec.Cmd\).Output outside internal/execx`
}

func methodValue(c *exec.Cmd) func() error { // want `exec.Cmd outside internal/execx`
	return c.Run // want `\(\*exec.Cmd\).Run outside internal/execx`
}

func forkExec() (int, error) {
	return syscall.ForkExec("/bin/sh", nil, nil) // want `syscall.ForkExec outside internal/execx`
}

func startProcess() (int, uintptr, error) {
	return syscall.StartProcess("/bin/sh", nil, nil) // want `syscall.StartProcess outside internal/execx`
}

func assigned() {
	var run func(string, ...string) *exec.Cmd // want `exec.Cmd outside internal/execx`
	run = exec.Command                        // want `exec.Command outside internal/execx`
	use(run)
}
