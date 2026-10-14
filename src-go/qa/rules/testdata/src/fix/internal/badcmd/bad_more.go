package badcmd

import (
	"os/exec"

	"fix/internal/execx"
)

var gumName = "gum"

func progName() string { return "sudo" }

func fromVar() execx.Cmd {
	return execx.Cmd{Name: gumName} // want `Name is not a compile-time constant`
}

func fromCall() execx.Cmd {
	return execx.Cmd{Name: progName()} // want `Name is not a compile-time constant`
}

func fromLookPath() (execx.Cmd, error) {
	p, err := exec.LookPath("tmux")
	return execx.Cmd{Name: p}, err // want `Name is not a compile-time constant`
}

func assignedVar(p string) execx.Cmd {
	var c execx.Cmd
	c.Name = p // want `Name is not a compile-time constant`
	return c
}

func absolute() execx.Cmd {
	return execx.Cmd{Name: "/usr/bin/gum"} // want `"gum" is owned by internal/ui`
}

func relative() execx.Cmd {
	return execx.Cmd{Name: "./bin/tmux"} // want `"tmux" is owned by internal/tmuxccy`
}

func envSudo() execx.Cmd {
	return execx.Cmd{Name: "env", Args: []string{"sudo", "-n", "id"}} // want `"sudo" is owned by internal/sudo`
}

func envAssignments() execx.Cmd {
	return execx.Cmd{Name: "env", Args: []string{"A=1", "-u", "B", "git", "status"}} // want `"git" is owned by internal/gitx`
}

func envUnowned() execx.Cmd {
	return execx.Cmd{Name: "env", Args: []string{"PYTHONPATH=/x", "python3", "-P"}}
}

func envConcat(rest []string) execx.Cmd {
	return execx.Cmd{Name: "env", Args: append([]string{"A=" + rest[0], "tmux"}, rest...)} // want `"tmux" is owned by internal/tmuxccy`
}

func envHidden(argv []string) execx.Cmd {
	return execx.Cmd{Name: "env", Args: argv} // want `cannot see which program "env" runs`
}

func envNoProgram() execx.Cmd {
	return execx.Cmd{Name: "env", Args: []string{"A=1"}} // want `cannot see which program "env" runs`
}

func timed() execx.Cmd {
	return execx.Cmd{Name: "timeout", Args: []string{"-s", "KILL", "5", "fzf"}} // want `"fzf" is owned by internal/ui`
}

func niced() execx.Cmd {
	return execx.Cmd{Name: "nice", Args: []string{"-n", "5", "gum"}} // want `"gum" is owned by internal/ui`
}

func detached() execx.Cmd {
	return execx.Cmd{Name: "setsid", Args: []string{"-w", "systemctl", "reboot"}} // want `"systemctl" is owned by internal/systemd`
}

func scoped() execx.Cmd {
	return execx.Cmd{Name: "systemd-run", Args: []string{"--user", "--unit", "x", "--", "tmux", "ls"}} // want `"tmux" is owned by internal/tmuxccy`
}

func scopedUnowned() execx.Cmd {
	return execx.Cmd{Name: "systemd-run", Args: []string{"--user", "--", "setsid", "-w", "gio", "launch", "x"}}
}

func chained() execx.Cmd {
	return execx.Cmd{Name: "env", Args: []string{"A=1", "nice", "-n", "1", "sudo", "id"}} // want `"sudo" is owned by internal/sudo`
}

func processTable() execx.Cmd {
	return execx.Cmd{Name: "ps", Args: []string{"-ww"}} // want `"ps" is owned by internal/sessprobe`
}

func logind() execx.Cmd {
	return execx.Cmd{Name: "busctl", Args: []string{"--system"}} // want `"busctl" is owned by internal/systemd`
}

func shell(script string) execx.Cmd {
	return execx.Cmd{Name: "bash", Args: []string{"-c", script}} // want `a shell string`
}

func shellViaEnv(script string) execx.Cmd {
	return execx.Cmd{Name: "env", Args: []string{"A=1", "/bin/sh", "-c", script}} // want `a shell string`
}

func shellScriptFile() execx.Cmd {
	return execx.Cmd{Name: "bash", Args: []string{"/opt/run.bash"}}
}

func unownedProgram() execx.Cmd {
	return execx.Cmd{Name: "systemd-escape", Args: []string{"--", "x"}}
}

func podmanOutsideItsOwners() execx.Cmd {
	return execx.Cmd{Name: "podman", Args: []string{"pause", "c"}} // want `"podman" is owned by internal/sessprobe or internal/podfreeze`
}

func nmcliOutsideItsOwner() execx.Cmd {
	return execx.Cmd{Name: "nmcli", Args: []string{"device"}} // want `"nmcli" is owned by internal/lxcfreeze`
}
