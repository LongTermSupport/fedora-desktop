package sessprobe

import "fix/internal/execx"

// sessprobe is the other owner of podman.
func own() execx.Cmd {
	return execx.Cmd{Name: "podman", Args: []string{"inspect", "c"}}
}
