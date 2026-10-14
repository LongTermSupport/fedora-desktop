// Package proc is the nested qa module's seam: it may start processes, as execx does.
package proc

import "os/exec"

// Run starts the command; the seam is the one place in its module allowed to.
func Run(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}
