package systemd

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

const (
	systemdRunBin = "systemd-run"
	journalBin    = "journalctl"
)

// run is one systemctl command whose output goes to Chatter; a failure names the command.
func (s *Systemd) run(ctx context.Context, args ...string) error {
	_, err := s.Runner.Run(ctx, execx.Cmd{Name: systemctlBin, Args: args, Stdout: s.Chatter, Stderr: s.Chatter})
	if err != nil {
		return fmt.Errorf("%s %s: %w", systemctlBin, strings.Join(args, " "), err)
	}
	return nil
}

// Show is one property of a unit (`systemctl show -P`), without its trailing newline. A
// list property set more than once comes back one value per line.
func (s *Systemd) Show(ctx context.Context, property, unit string) (string, error) {
	res, err := s.Runner.Run(ctx, execx.Cmd{Name: systemctlBin, Args: []string{"show", "-P", property, unit}})
	if err != nil {
		return "", fmt.Errorf("%s show -P %s %s: %w", systemctlBin, property, unit, err)
	}
	return strings.TrimRight(string(res.Stdout), "\n"), nil
}

// IsEnabled is whether the unit is enabled. Any exit status of systemctl is an answer; only
// a systemctl that could not run is an error.
func (s *Systemd) IsEnabled(ctx context.Context, unit string) (bool, error) {
	_, err := s.Runner.Run(ctx, execx.Cmd{Name: systemctlBin, Args: []string{"is-enabled", "--quiet", unit}})
	if err == nil {
		return true, nil
	}
	if _, refused := execx.ExitOf(err); refused {
		return false, nil
	}
	return false, fmt.Errorf("%s is-enabled %s: %w", systemctlBin, unit, err)
}

// Enable enables the unit.
func (s *Systemd) Enable(ctx context.Context, unit string) error {
	return s.run(ctx, "enable", unit)
}

// DisableNow disables and stops the units.
func (s *Systemd) DisableNow(ctx context.Context, units ...string) error {
	return s.run(ctx, append([]string{"disable", "--now"}, units...)...)
}

// Start starts the unit.
func (s *Systemd) Start(ctx context.Context, unit string) error {
	return s.run(ctx, "start", unit)
}

// Restart restarts the unit.
func (s *Systemd) Restart(ctx context.Context, unit string) error {
	return s.run(ctx, "restart", unit)
}

// Stop stops the unit.
func (s *Systemd) Stop(ctx context.Context, unit string) error {
	return s.run(ctx, "stop", unit)
}

// DaemonReload makes systemd re-read its unit files.
func (s *Systemd) DaemonReload(ctx context.Context) error {
	return s.run(ctx, "daemon-reload")
}

// SignalMain sends a signal (such as SIGUSR2) to the unit's main process only.
func (s *Systemd) SignalMain(ctx context.Context, unit, signal string) error {
	return s.run(ctx, "kill", "--kill-whom=main", "--signal="+signal, unit)
}

// RunTransient runs argv once in a transient unit named unit, with the given properties
// (Name=Value). The unit is collected even when the run fails, so a failure leaves no failed
// unit behind to block the name.
func (s *Systemd) RunTransient(ctx context.Context, unit string, properties, argv []string) error {
	args := []string{"--unit=" + unit, "--collect"}
	for _, p := range properties {
		args = append(args, "--property="+p)
	}
	args = append(append(args, "--"), argv...)
	_, err := s.Runner.Run(ctx, execx.Cmd{Name: systemdRunBin, Args: args, Stdout: s.Chatter, Stderr: s.Chatter})
	if err != nil {
		return fmt.Errorf("%s --unit=%s: %w", systemdRunBin, unit, err)
	}
	return nil
}

// Journal writes the last lines of the unit's journal to out.
func (s *Systemd) Journal(ctx context.Context, unit string, lines int, out io.Writer) error {
	_, err := s.Runner.Run(ctx, execx.Cmd{
		Name:   journalBin,
		Args:   []string{"--unit", unit, "--lines", strconv.Itoa(lines), "--no-pager", "--output", "short-iso"},
		Stdout: out,
		Stderr: out,
	})
	if err != nil {
		return fmt.Errorf("%s --unit %s: %w", journalBin, unit, err)
	}
	return nil
}
