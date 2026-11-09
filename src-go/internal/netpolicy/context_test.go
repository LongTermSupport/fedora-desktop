package netpolicy

import (
	"net/netip"
	"strings"
	"testing"
)

func TestContextEnforced(t *testing.T) {
	c := Context{
		State:    Enforced,
		Entries:  []string{"192.0.2.10", "lxc"},
		HostFile: "/home/u/.config/claude-yolo/network/repo-0123456789abcdef.env",
		Bus:      []netip.Addr{addr4(10, 254, 0, 1)},
		Networks: []Network{{"shop-network", []netip.Prefix{v4(10, 89, 6, 0, 24)}}},
		LXC:      3,
	}
	text := c.Text()
	for _, want := range []string{
		Marker,
		"private and LAN ranges (" + v4(10, 0, 0, 0, 8).String(),
		"this host's own addresses, host.containers.internal",
		"other ccy sessions",
		"the agent bus at " + addr4(10, 254, 0, 1).String() + " (every port)",
		"shop-network (" + v4(10, 89, 6, 0, 24).String() + ")",
		"192.0.2.10, lxc (3 LXC dev-site addresses)",
		"No route to host",
		"curl exit 7",
		"not a service that is down",
		c.HostFile,
		"ccy --connect",
		ContextPath,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the context lacks %q:\n%s", want, text)
		}
	}
}

func TestContextNoAllowList(t *testing.T) {
	text := Context{State: Enforced, HostFile: "/h/f.env"}.Text()
	if !strings.Contains(text, "The owner's allow-list: none") || strings.Contains(text, "agent bus at") {
		t.Errorf("%s", text)
	}
}

func TestContextOffAndUnavailable(t *testing.T) {
	off := Context{State: Off, HostFile: "/h/f.env"}.Text()
	if !strings.HasPrefix(off, Marker) || !strings.Contains(off, "off for this checkout") || !strings.Contains(off, "the LAN and the host itself") {
		t.Errorf("off:\n%s", off)
	}
	none := Context{State: Unavailable, Reason: "the Docker engine cannot hold it", HostFile: "/h/f.env"}.Text()
	if !strings.Contains(none, "NO network policy") || !strings.Contains(none, "the Docker engine cannot hold it") {
		t.Errorf("unavailable:\n%s", none)
	}
}
