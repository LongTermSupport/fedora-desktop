package podfreeze

import "fix/internal/execx"

// podfreeze is one of podman's two owners.
func own() execx.Cmd {
	return execx.Cmd{Name: "podman", Args: []string{"network", "ls"}}
}

// The other owners' programs are not podfreeze's.
func foreign() execx.Cmd {
	return execx.Cmd{Name: "lxc-ls", Args: []string{"-1"}} // want `"lxc-ls" is owned by internal/lxcfreeze`
}
