package units

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// systemdVersionLine is `systemctl --version`'s first line: `systemd N`, then the package's
// version in parentheses when the build names one.
var systemdVersionLine = regexp.MustCompile(`^systemd ([0-9]+)(?: \(([0-9][0-9A-Za-z.~+_-]*)\))?`)

// SystemdVersion is the running systemd's version, as `systemctl --version` names it: the
// package version in its parentheses, else the bare number (the restore proof's key, issue #324).
func (m *Manager) SystemdVersion(ctx context.Context) (string, error) {
	res, err := m.run.Run(ctx, program(execx.Cmd{Name: "/usr/bin/systemctl", Args: []string{"--version"}}))
	if err != nil {
		return "", fmt.Errorf("systemctl --version: %w", err)
	}
	// The pattern is anchored at the start of the output, so it reads the first line only.
	match := systemdVersionLine.FindStringSubmatch(string(res.Stdout))
	if match == nil {
		return "", errors.New("systemctl --version names no systemd version")
	}
	if match[2] != "" {
		return match[2], nil
	}
	return match[1], nil
}
