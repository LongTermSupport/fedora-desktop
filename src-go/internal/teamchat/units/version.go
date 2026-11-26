package units

import (
	"context"
	"fmt"
	"regexp"

	"github.com/godbus/dbus/v5"
)

// systemdVersion is a systemd version as its Manager names it: the package's version
// ("256.7-1.fc41") or the bare number ("257"); the grammar restore's proof record keeps.
var systemdVersion = regexp.MustCompile(`\A[0-9]{1,6}(?:[.~+_-][0-9A-Za-z.~+_-]{0,57})?\z`)

// SystemdVersion is the running systemd's version, the Manager's Version property over the bus
// (the restore proof's key, issue #324).
func (m *Manager) SystemdVersion(ctx context.Context) (string, error) {
	const what = "version"
	s, err := m.connect(ctx)
	if err != nil {
		return "", fmt.Errorf("systemd %s: %w", what, err)
	}
	body, err := m.callOn(ctx, s, what, managerPath, propertiesGet, managerIface, "Version")
	if err != nil {
		return "", err
	}
	v, err := reply[dbus.Variant](body, "Version")
	if err != nil {
		return "", fmt.Errorf("systemd %s: %w", what, err)
	}
	version, ok := v.Value().(string)
	if !ok || !systemdVersion.MatchString(version) {
		return "", fmt.Errorf("systemd %s: %w: Version is not a systemd version", what, ErrMalformed)
	}
	return version, nil
}
