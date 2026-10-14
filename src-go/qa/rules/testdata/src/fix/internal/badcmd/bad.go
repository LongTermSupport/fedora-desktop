// Package badcmd names programs it does not own.
package badcmd

import (
	"os/exec"

	"fix/internal/execx"
)

const tmuxBin = "tmux"

func keyed() execx.Cmd {
	return execx.Cmd{Name: "sudo", Args: []string{"true"}} // want `"sudo" is owned by internal/sudo`
}

func viaConst() execx.Cmd {
	return execx.Cmd{Name: tmuxBin, Args: []string{"has-session", "-t", "=alpha"}} // want `"tmux" is owned by internal/tmuxccy`
}

func unkeyed() execx.Cmd {
	return execx.Cmd{"git", nil} // want `"git" is owned by internal/gitx`
}

func assigned() execx.Cmd {
	var c execx.Cmd
	c.Name = "systemctl" // want `"systemctl" is owned by internal/systemd`
	return c
}

func picker() execx.Cmd {
	return execx.Cmd{Name: "fzf"} // want `"fzf" is owned by internal/ui`
}

func prompt() execx.Cmd {
	return execx.Cmd{Name: "gum"} // want `"gum" is owned by internal/ui`
}

func other() execx.Cmd {
	return execx.Cmd{Name: "wl-copy"}
}

// presence checks name the tool without running it.
func presence() error {
	_, err := exec.LookPath("gum")
	return err
}

// echoed is text that merely contains the name.
func echoed() string {
	return "sudo is needed"
}
