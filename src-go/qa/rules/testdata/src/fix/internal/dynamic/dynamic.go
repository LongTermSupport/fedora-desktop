// Package dynamic runs a program chosen at run time.
package dynamic

import "fix/internal/execx"

// Launch is named in the dynamicNames table, with its reason.
func Launch(program string) execx.Cmd {
	return execx.Cmd{Name: program}
}

// Runner has a method named in the table by its receiver type.
type Runner[T any] struct{ v T }

// Run is named as Runner.Run, whatever the receiver form.
func (r *Runner[T]) Run(program string) execx.Cmd {
	return execx.Cmd{Name: program}
}

// Plain is a value-receiver method, not named in the table.
func (r Runner[T]) Plain(program string) execx.Cmd {
	return execx.Cmd{Name: program} // want `Name is not a compile-time constant`
}

// Transient runs a program chosen at run time under systemd-run; the table row says so.
func Transient(argv []string) execx.Cmd {
	return execx.Cmd{Name: "systemd-run", Args: append([]string{"--collect", "--"}, argv...)}
}

// Unexcused is the same shape without a row: the wrapper hides the program.
func Unexcused(argv []string) execx.Cmd {
	return execx.Cmd{Name: "systemd-run", Args: append([]string{"--collect", "--"}, argv...)} // want `cannot see which program "systemd-run" runs: its arguments are not a literal`
}

// Other is not named, so the same shape is flagged.
func Other(program string) execx.Cmd {
	return execx.Cmd{Name: program} // want `Name is not a compile-time constant`
}
