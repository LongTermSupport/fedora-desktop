package lxcfreeze

import (
	"context"
	"errors"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze"
)

// errNotRenewed marks a thaw that happened but whose DHCP lease was not renewed. The container
// IS thawed; the thaw still counts as a failure and the run exits 1, as documented.
var errNotRenewed = errors.New("thawed, but the DHCP lease was not renewed")

const ethernet = "ethernet"

// renewDHCP renews the lease of every ethernet device NetworkManager manages inside the
// container, because a freeze longer than the lease leaves it thawed but unreachable. lxc-attach
// chowns the file its stderr points at, so both streams are pipes: sudo.Capture hands the runner
// no writers. The returned text is the reason, in lxcfreeze's own words.
func (e *Engine) renewDHCP(ctx context.Context, name string) (string, error) {
	attach := []string{"lxc-attach", "-n", name, "--", "nmcli"}
	status, err := e.Sudo.Capture(ctx, append(attach, "-t", "-f", "DEVICE,TYPE", "device", "status")...)
	if err != nil {
		return "thawed, but the DHCP lease was not renewed — nmcli failed: " + freeze.FailureText(status, err), errNotRenewed
	}
	devices := ethernetDevices(string(status.Stdout))
	if len(devices) == 0 {
		return "thawed, but the DHCP lease was not renewed — NetworkManager manages no ethernet device", errNotRenewed
	}
	for _, dev := range devices {
		res, connectErr := e.Sudo.Capture(ctx, append(attach, "device", "connect", dev)...)
		if connectErr != nil {
			return "thawed, but the DHCP lease on " + dev + " was not renewed — nmcli failed: " + freeze.FailureText(res, connectErr), errNotRenewed
		}
	}
	return "", nil
}

// ethernetDevices are the first colon-separated field of each `nmcli -t` line whose second field
// is "ethernet".
func ethernetDevices(status string) []string {
	var devices []string
	for line := range strings.SplitSeq(status, "\n") {
		if fields := strings.Split(line, ":"); len(fields) >= 2 && fields[1] == ethernet {
			devices = append(devices, fields[0])
		}
	}
	return devices
}
