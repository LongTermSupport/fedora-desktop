// Package badexec starts processes behind the runner's back.
package badexec

import (
	"context"
	"os"
	"os/exec"
	"syscall"
)

func direct() error {
	return exec.Command("ls").Run() // want `exec.Command outside internal/execx`
}

func withContext(ctx context.Context) error {
	return exec.CommandContext(ctx, "ls").Run() // want `exec.CommandContext outside internal/execx`
}

// shellEvasion is the sh -c route round a gosec G204 finding: the command is still not
// the runner's.
func shellEvasion(arg string) error {
	return exec.Command("sh", "-c", "tmux kill-server "+arg).Run() // want `exec.Command outside internal/execx`
}

func replace() error {
	return syscall.Exec("/bin/ls", []string{"ls"}, nil) // want `syscall.Exec outside internal/execx`
}

func spawn() (*os.Process, error) {
	return os.StartProcess("/bin/ls", []string{"ls"}, &os.ProcAttr{}) // want `os.StartProcess outside internal/execx`
}

// lookup is allowed anywhere: finding a tool starts nothing.
func lookup() (string, error) {
	return exec.LookPath("gum")
}
