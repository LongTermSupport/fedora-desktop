package lxcfreeze

import "fix/internal/execx"

// lxcfreeze owns the lxc tools and the nmcli it runs inside a container.
func own() execx.Cmd {
	return execx.Cmd{Name: "lxc-info", Args: []string{"-n", "c", "-s"}}
}

func ownNmcli() execx.Cmd {
	return execx.Cmd{Name: "nmcli", Args: []string{"device"}}
}

// podman is not lxcfreeze's.
func foreign() execx.Cmd {
	return execx.Cmd{Name: "podman", Args: []string{"ps"}} // want `"podman" is owned by internal/sessprobe or internal/podfreeze`
}
