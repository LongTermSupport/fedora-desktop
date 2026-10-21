// Package shellstring runs a command line a user configured, as the program's spec says.
package shellstring

import "fix/internal/execx"

// Configured is named in the shellStrings table, with its reason.
func Configured(line string) execx.Cmd {
	return execx.Cmd{Name: "/bin/sh", Args: []string{"-c", line}}
}

// Runner has a method named in the table by its receiver type.
type Runner struct{}

// Run is named as Runner.Run.
func (Runner) Run(line string) execx.Cmd {
	return execx.Cmd{Name: "env", Args: []string{"A=1", "bash", "-c", line}}
}

// Other is not named, so the same shape is flagged.
func Other(line string) execx.Cmd {
	return execx.Cmd{Name: "/bin/sh", Args: []string{"-c", line}} // want `sh -c is a shell string`
}

// OwnedInside is named, but the row excuses only the shell string: an owned program is still
// judged.
func OwnedInside() execx.Cmd {
	return execx.Cmd{Name: "git", Args: []string{"status"}} // want `"git" is owned by`
}
