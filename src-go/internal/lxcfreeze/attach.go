package lxcfreeze

import (
	"context"
	"fmt"
	"slices"
	"strings"

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

// StartUnit starts a system unit inside a running container as root: `sudo -n lxc-attach -P
// <lxcPath> -n <container> --clear-env -- env -i PATH=<guest path> systemctl start -- <unit>`.
// It is ccy-teams' Start its session for an LXC member, the one start of play-agent-bus.yml's
// `service: true` unit, which nothing enables. A failure carries systemd's own words. sudo
// must be ready (sudo.Ready) first.
func (l *Lister) StartUnit(ctx context.Context, lxcPath, container, unit string) error {
	res, err := l.Sudo.Capture(ctx, "lxc-attach", "-P", lxcPath, "-n", container, "--clear-env", "--",
		"env", "-i", "PATH="+guestPath, "systemctl", "start", "--", unit)
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(res.Stderr)))
	}
	return nil
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

// UnitState is a unit's LoadState and ActiveState inside a running container, asked of systemd
// as root: `sudo -n lxc-attach -P <lxcPath> -n <container> --clear-env -- env -i PATH=<guest
// path> systemctl show --property=Id,LoadState,ActiveState -- <unit>`. A unit with no file is
// loaded "not-found". An answer missing either is an error. sudo must be ready first.
func (l *Lister) UnitState(ctx context.Context, lxcPath, container, unit string) (string, string, error) {
	states, err := l.UnitStates(ctx, lxcPath, container, unit)
	if err != nil {
		return "", "", err
	}
	return states[0].Load, states[0].Active, nil
}

// UnitStatus is a unit's LoadState and ActiveState.
type UnitStatus struct {
	Load, Active string
}

// UnitStates is UnitState for every unit given, in the order given, asked in one systemctl show
// inside the container with each unit's Id; systemd answers one block per unit, blank-line
// separated, and each block is matched to its unit by its Id, not by its position. An answer
// with another number of blocks, a block with no Id, an Id named twice or not asked, or a unit
// missing either state, is an error.
func (l *Lister) UnitStates(ctx context.Context, lxcPath, container string, units ...string) ([]UnitStatus, error) {
	res, err := l.Sudo.Capture(ctx, append([]string{
		"lxc-attach", "-P", lxcPath, "-n", container, "--clear-env", "--",
		"env", "-i", "PATH=" + guestPath, "systemctl", "show", "--property=Id,LoadState,ActiveState", "--",
	}, units...)...)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(res.Stderr)))
	}
	blocks := strings.Split(strings.TrimSpace(string(res.Stdout)), "\n\n")
	if len(blocks) != len(units) {
		return nil, fmt.Errorf("systemctl show in container %s answered %d units of %d (%s)", container, len(blocks), len(units), strings.Join(units, " "))
	}
	byID := map[string]UnitStatus{}
	for _, block := range blocks {
		props := map[string]string{}
		for line := range strings.SplitSeq(block, "\n") {
			if key, value, ok := strings.Cut(line, "="); ok {
				props[key] = value
			}
		}
		id := props["Id"]
		if _, seen := byID[id]; seen || !slices.Contains(units, id) {
			return nil, fmt.Errorf("systemctl show in container %s answered for unit %q, which is not one asked once (%s)", container, id, strings.Join(units, " "))
		}
		byID[id] = UnitStatus{Load: props["LoadState"], Active: props["ActiveState"]}
	}
	states := make([]UnitStatus, 0, len(units))
	for _, unit := range units {
		s := byID[unit]
		if s.Load == "" || s.Active == "" {
			return nil, fmt.Errorf("systemctl show in container %s gave no LoadState or ActiveState for %s", container, unit)
		}
		states = append(states, s)
	}
	return states, nil
}
