package badcmd

import "fix/internal/execx"

// A test spells out the argv it expects, so it may name any program.
func expected() execx.Cmd {
	return execx.Cmd{Name: "tmux"}
}
