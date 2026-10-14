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

// Transient has a dynamicNames row, but a row excuses the Name, not a wrapper whose program
// is hidden: only a program runner's own body is excused that.
func Transient(argv []string) execx.Cmd {
	return execx.Cmd{Name: "systemd-run", Args: append([]string{"--collect", "--"}, argv...)} // want `cannot see which program "systemd-run" runs: its arguments are not a literal`
}

// Systemd stands for the systemd seam: RunTransient is registered as a program runner.
type Systemd struct{}

// RunTransient takes the program as its own parameter; the wrapper's program is that
// parameter, and the calls are judged instead.
func (s *Systemd) RunTransient(unit string, props []string, program string, args ...string) execx.Cmd {
	argv := append(append([]string{"--unit=" + unit}, props...), "--", program)
	return execx.Cmd{Name: "systemd-run", Args: append(argv, args...)}
}

// Hidden is the same call shape in a function that is not a runner.
func (s *Systemd) Hidden(program string) execx.Cmd {
	return execx.Cmd{Name: "systemd-run", Args: []string{"--", program}} // want `cannot see which program "systemd-run" runs: its arguments are not a literal`
}

// CallsTransient is judged at the call: an owned program is flagged, an unowned one is free.
func CallsTransient(s *Systemd, program string) {
	s.RunTransient("u.service", nil, "podman", "ps") // want `"podman" is owned by`
	s.RunTransient("u.service", nil, "/usr/local/lib/tool", "--flag")
	s.RunTransient("u.service", nil, program) // want `Name is not a compile-time constant`
}

// Unexcused is the same shape without a row: the wrapper hides the program.
func Unexcused(argv []string) execx.Cmd {
	return execx.Cmd{Name: "systemd-run", Args: append([]string{"--collect", "--"}, argv...)} // want `cannot see which program "systemd-run" runs: its arguments are not a literal`
}

// Other is not named, so the same shape is flagged.
func Other(program string) execx.Cmd {
	return execx.Cmd{Name: program} // want `Name is not a compile-time constant`
}
