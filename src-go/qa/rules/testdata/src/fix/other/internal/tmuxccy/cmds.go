package tmuxccy

import "fix/internal/execx"

// The owner runs its own program, directly or behind a wrapper.
func own() execx.Cmd {
	return execx.Cmd{Name: "tmux", Args: []string{"-L", "s", "ls"}}
}

func scoped() execx.Cmd {
	return execx.Cmd{Name: "systemd-run", Args: []string{"--user", "--scope", "--", "tmux", "-L", "s", "new-session"}}
}

// A program owned elsewhere is still not the owner's to run.
func foreign() execx.Cmd {
	return execx.Cmd{Name: "git", Args: []string{"status"}} // want `"git" is owned by internal/gitx`
}
