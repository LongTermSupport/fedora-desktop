// Package sudo owns the sudo command.
package sudo

import "fix/internal/execx"

// Command is sudo with -n, the one form that never prompts.
func Command(args ...string) execx.Cmd {
	return execx.Cmd{Name: "sudo", Args: append([]string{"-n"}, args...)}
}
