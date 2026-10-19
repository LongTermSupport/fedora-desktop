package lxcfreeze

import (
	"context"
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// tmuxProgram is the one program InContainer runs.
const tmuxProgram = "tmux"

// InContainer is a runner for tmuxccy.Tmux that runs each tmux command inside a running
// container as its user: `sudo -n lxc-attach -P <lxcPath> -n <container> --clear-env -- runuser
// -u <user> -- env -i PATH=<guest path> tmux ...`. It reaches the tmux server an LXC member's
// session runs on (play-agent-bus.yml's `service: true` unit), so a host flow reads and types
// into that session the way it does a desktop one. sudo must be ready (sudo.Ready) first.
func (l *Lister) InContainer(lxcPath, container, user string) execx.Runner {
	return containerTmux{l: l, lxcPath: lxcPath, container: container, user: user}
}

type containerTmux struct {
	l                        *Lister
	lxcPath, container, user string
}

// Run runs the tmux command c inside the container; any other program is refused.
func (r containerTmux) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) {
	if c.Name != tmuxProgram {
		return execx.Result{}, fmt.Errorf("only tmux runs inside container %s this way, not %s", r.container, c.Name)
	}
	return r.l.Sudo.Capture(ctx, append([]string{
		"lxc-attach", "-P", r.lxcPath, "-n", r.container, "--clear-env", "--",
		"runuser", "-u", r.user, "--", "env", "-i", "PATH=" + guestPath, "tmux",
	}, c.Args...)...)
}
