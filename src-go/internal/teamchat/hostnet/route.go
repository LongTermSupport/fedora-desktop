package hostnet

import (
	"net/netip"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
)

// loopbackDev is the loopback interface's name.
const loopbackDev = "lo"

// Route is the interface that routes network (SPEC-cross-network X3). A host prefix of an address
// the host holds is routed by the interface that holds it: such an address lives in the kernel's
// local table, which /proc/net/route does not list, so the table would send it down the default
// route. Anything else is the best route of the main table that covers the whole network, never
// one judged by its first address alone, with Split naming another interface that a longer route
// inside the network goes by (Plan 00257 HOST-7).
func (n Network) Route(network netip.Prefix) (host.Route, bool, error) {
	addr := network.Addr().Unmap()
	if network.Bits() == addr.BitLen() {
		links, err := n.read()
		if err != nil {
			return host.Route{}, false, err
		}
		for _, l := range links {
			for _, p := range l.Prefixes {
				if p.Addr() == addr {
					return host.Route{Interface: l.Name}, true, nil
				}
			}
		}
	}
	routes, err := routesOf(n.Proc, addr)
	if err != nil {
		return host.Route{}, false, err
	}
	whole := netip.PrefixFrom(addr, network.Bits()).Masked()
	best, found := bestRoute(routes, func(p netip.Prefix) bool { return p.Bits() <= whole.Bits() && p.Contains(whole.Addr()) })
	if !found {
		return host.Route{}, false, nil
	}
	return host.Route{Interface: best.dev, Default: best.prefix.Bits() == 0, Split: split(routes, whole, best.dev)}, true, nil
}

// split is an interface other than dev that a route longer than whole, inside it, goes by, ""
// when none does.
func split(routes []route, whole netip.Prefix, dev string) string {
	for _, inner := range routes {
		// The IPv6 table lists the host's own addresses as host routes on loopback, which carry
		// no member; they split nothing.
		if inner.dev != loopbackDev && inner.dev != dev && inner.prefix.Bits() > whole.Bits() && whole.Contains(inner.prefix.Addr()) {
			return inner.dev
		}
	}
	return ""
}
