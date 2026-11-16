// Package execx is the fixture's process seam: it hands a child the stdin it is given, and feeds
// one from a reader.
package execx

import (
	"io"
	"os/exec"
)

// Cmd is one command.
type Cmd struct {
	Name  string
	Stdin io.Reader
}

// Run runs it with its stdin, copied in by the child's pipe.
func Run(c Cmd, w io.WriteCloser) error {
	cmd := exec.Command(c.Name)
	cmd.Stdin = c.Stdin
	if _, err := io.Copy(w, c.Stdin); err != nil {
		return err
	}
	return cmd.Run()
}
