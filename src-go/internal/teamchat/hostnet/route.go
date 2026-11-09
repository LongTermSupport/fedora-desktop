package hostnet

import (
	"net/netip"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
)

// Route is the interface that routes network (SPEC-cross-network X3). A host prefix of an address
// the host holds is routed by the interface that holds it: such an address lives in the kernel's
// local table, which /proc/net/route does not list, so the table would send it down the default
// route. Anything else is the best route of the main table for the network's first address, as
// Firewall.Zone reads it.
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
	best, found, err := lookupRoute(n.Proc, addr)
	if err != nil || !found {
		return host.Route{}, false, err
	}
	return host.Route{Interface: best.dev, Default: best.prefix.Bits() == 0}, true, nil
}
