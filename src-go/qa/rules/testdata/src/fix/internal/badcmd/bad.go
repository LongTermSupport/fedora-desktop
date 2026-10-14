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

// The installer's programs: each is owned by the package that decides its flags.
func installerPrograms() []execx.Cmd {
	return []execx.Cmd{
		{Name: "journalctl"},   // want `"journalctl" is owned by internal/systemd`
		{Name: "firewall-cmd"}, // want `"firewall-cmd" is owned by internal/firewalld`
		{Name: "ip"},           // want `"ip" is owned by internal/hostnet`
		{Name: "ss"},           // want `"ss" is owned by internal/hostnet`
		{Name: "nmcli"},        // want `"nmcli" is owned by internal/lxcfreeze or internal/hostnet`
		{Name: "curl"},         // want `"curl" is owned by internal/hostnet`
		{Name: "rpm"},          // want `"rpm" is owned by internal/hostsys`
		{Name: "dnf"},          // want `"dnf" is owned by internal/hostsys`
		{Name: "getent"},       // want `"getent" is owned by internal/hostsys`
		{Name: "useradd"},      // want `"useradd" is owned by internal/hostsys`
		{Name: "uname"},        // want `"uname" is owned by internal/hostsys`
		{Name: "zstd"},         // want `"zstd" is owned by internal/hostsys`
	}
}

// systemd-run is not owned: more than one package starts transient units with it.
func transient() execx.Cmd {
	return execx.Cmd{Name: "systemd-run", Args: []string{"--scope", "--", "true"}}
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
