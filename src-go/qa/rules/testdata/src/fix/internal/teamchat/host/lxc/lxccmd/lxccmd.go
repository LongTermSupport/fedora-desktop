// Package lxccmd is the second owner of lxc-ls, lxc-info and lxc-attach: it runs them as root
// with no sudo, and the program inside lxc-attach is a host-written path it names at run time.
package lxccmd

import "fix/internal/execx"

// LXC has the dynamicNames row: its inner program after lxc-attach -- may be a variable.
type LXC struct{}

func (LXC) ls() execx.Cmd {
	return execx.Cmd{Name: "lxc-ls", Args: []string{"-1"}}
}

func (LXC) info(c string) execx.Cmd {
	return execx.Cmd{Name: "lxc-info", Args: []string{"-n", c, "-sH"}}
}

// Guest hands lxc-attach a binary path known only at run time; the row excuses it.
func (LXC) Guest(c, binary string, words []string) execx.Cmd {
	return execx.Cmd{Name: "lxc-attach", Args: append([]string{"-n", c, "--clear-env", "--", binary}, words...)}
}

// Inside is excused too, but an owned program after the -- is still judged.
func (LXC) Inside(c string) execx.Cmd {
	return execx.Cmd{Name: "lxc-attach", Args: []string{"-n", c, "--", "git", "log"}} // want `"git" is owned by internal/gitx`
}

// Env is excused, but env is not an inner wrapper, so its hidden program is still a finding.
func (LXC) Env(prog string) execx.Cmd {
	return execx.Cmd{Name: "env", Args: []string{"-i", prog}} // want `cannot see which program "env" runs: its arguments are not a literal`
}

// Other has no row, so the same shape as Guest is a finding.
func Other(c, binary string) execx.Cmd {
	return execx.Cmd{Name: "lxc-attach", Args: []string{"-n", c, "--", binary}} // want `cannot see which program "lxc-attach" runs: its arguments are not a literal`
}

// freeze is still lxcfreeze's alone.
func freeze(c string) execx.Cmd {
	return execx.Cmd{Name: "lxc-freeze", Args: []string{"-n", c}} // want `"lxc-freeze" is owned by internal/lxcfreeze`
}
