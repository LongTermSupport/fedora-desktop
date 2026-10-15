package systemd

import (
	"context"
	"errors"
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sudo"
)

// UnitActive asks "systemctl is-active --quiet": a zero exit is active, a non-zero exit is the
// answer no, and only a systemctl that could not run is an error.
func (s *Systemd) UnitActive(ctx context.Context, unit string) (bool, error) {
	_, err := s.Runner.Run(ctx, execx.Cmd{Name: systemctlBin, Args: []string{"is-active", "--quiet", unit}})
	if err != nil {
		if exitErr, ok := errors.AsType[*execx.ExitError](err); !ok || exitErr == nil {
			return false, err
		}
	}
	return err == nil, nil
}

// StartAsRoot starts a system unit as root.
func (s *Systemd) StartAsRoot(ctx context.Context, root *sudo.Sudo, unit string) error {
	return named("systemctl start "+unit, root.Run(ctx, "systemctl", "start", unit))
}

// StopAsRoot stops a system unit as root.
func (s *Systemd) StopAsRoot(ctx context.Context, root *sudo.Sudo, unit string) error {
	return named("systemctl stop "+unit, root.Run(ctx, "systemctl", "stop", unit))
}

// DaemonReloadAsRoot makes systemd reread its unit files and drop-ins, as root.
func (s *Systemd) DaemonReloadAsRoot(ctx context.Context, root *sudo.Sudo) error {
	return named("systemctl daemon-reload", root.Run(ctx, "systemctl", "daemon-reload"))
}

func named(what string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", what, err)
}
