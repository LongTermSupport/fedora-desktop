// Package execx is a stand-in for the command runner, with the command's fields the rule reads.
package execx

// Cmd is one command: a program and its arguments.
type Cmd struct {
	Name string
	Args []string
}

// Run runs c.
func Run(_ Cmd) error { return nil }
