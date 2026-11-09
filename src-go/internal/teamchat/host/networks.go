package host

import (
	"fmt"
	"net/netip"
	"slices"
)

// listenAddress is the address of iface that serves network (SPEC-cross-network X3): of the
// network's family, never an IPv6 link-local one (it needs a zone to be bound); the lowest
// inside the network if the interface has one, else the lowest it has. An own network (the
// host's bus address, the bridge) is served only from an address inside it.
func listenAddress(network netip.Prefix, own bool, iface Interface) (netip.Addr, error) {
	family := "IPv4"
	if network.Addr().Is6() {
		family = "IPv6"
	}
	var inside, outside []netip.Addr
	for _, a := range iface.Addrs {
		switch {
		case a.Is4() != network.Addr().Is4() || (a.Is6() && a.IsLinkLocalUnicast()):
		case network.Contains(a):
			inside = append(inside, a)
		default:
			outside = append(outside, a)
		}
	}
	switch {
	case len(inside) > 0:
		return slices.MinFunc(inside, netip.Addr.Compare), nil
	case len(outside) == 0:
		return netip.Addr{}, fmt.Errorf("%s has no %s address", iface.Name, family)
	case own:
		return netip.Addr{}, fmt.Errorf("%s has no address inside it", iface.Name)
	}
	return slices.MinFunc(outside, netip.Addr.Compare), nil
}
