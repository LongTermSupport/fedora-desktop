package hostnet

import (
	"net/netip"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
)

// routeWorld is a /proc holding the given route tables and the interfaces the host reports.
func routeWorld(t *testing.T, route, route6 string, links ...link) Network {
	t.Helper()
	proc := t.TempDir()
	if route != "" {
		writeFile(t, filepath.Join(proc, "net/route"), route)
	}
	if route6 != "" {
		writeFile(t, filepath.Join(proc, "net/ipv6_route"), route6)
	}
	return Network{Proc: proc, links: fakeLinks(links...)}
}

func wantRoute(t *testing.T, n Network, network string, want host.Route) {
	t.Helper()
	got, found, err := n.Route(netip.MustParsePrefix(network))
	if err != nil || !found || got != want {
		t.Fatalf("Route(%s) = %+v, found %v, %v; want %+v", network, got, found, err, want)
	}
}

// X3: a network is routed by the interface the route table says, the longest prefix then the
// lowest metric, as the firewall's zone lookup reads it; the default route is marked.
func TestRouteIsTheInterfaceThatRoutesTheNetwork(t *testing.T) {
	n := routeWorld(t, routeHeader+
		route4("eth0", mask0, "0003", "100", mask0)+
		route4("eth1", dest192, "0001", "100", mask24)+
		route4("cheap", dest198, "0001", "50", mask24)+
		route4("dear", dest198, "0001", "300", mask24)+
		route4("down", dest198, "0000", "1", mask24), "")
	wantRoute(t, n, "192.0.2.0/24", host.Route{Interface: "eth1"})
	wantRoute(t, n, "198.51.100.0/24", host.Route{Interface: "cheap"})
	wantRoute(t, n, "203.0.113.0/24", host.Route{Interface: "eth0", Default: true})
}

// X3: a host with one NIC on its own segment has the default route and an explicit route per
// member network, all through that NIC. A member network is routed by its explicit route, so it is
// not marked as the default route's though the interface is the same; a network with no explicit
// route is.
func TestAnExplicitRouteThroughTheDefaultInterfaceIsNotTheDefaultRoute(t *testing.T) {
	n := routeWorld(t, routeHeader+
		route4("eth0", mask0, "0003", "100", mask0)+
		route4("eth0", dest198, "0003", "100", mask24), "")
	wantRoute(t, n, "198.51.100.0/24", host.Route{Interface: "eth0"})
	wantRoute(t, n, "192.0.2.0/24", host.Route{Interface: "eth0", Default: true})
}

func TestRouteOfAnIPv6Network(t *testing.T) {
	n := routeWorld(t, routeHeader,
		route6(dest2001, "20", "00000400", "00000001", "eth2")+
			route6(strings.Repeat("0", 32), "00", "00000100", "00000003", "eth0"))
	wantRoute(t, n, "2001:db8:1::/48", host.Route{Interface: "eth2"})
	wantRoute(t, n, "2001:db9::/32", host.Route{Interface: "eth0", Default: true})
}

// X3: no route covering the network is not an error of the read; the caller names the CIDR.
func TestRouteWithNoRouteIsNotFound(t *testing.T) {
	n := routeWorld(t, routeHeader+route4("eth1", dest198, "0001", "100", mask24), "")
	got, found, err := n.Route(netip.MustParsePrefix("192.0.2.0/24"))
	if err != nil || found {
		t.Fatalf("Route = %+v, found %v, %v", got, found, err)
	}
}

// X3: the host's own address is routed by the interface that holds it, which the main route
// table does not list (a host address lives in the local table), so the default route that
// covers it never takes it.
func TestRouteOfTheHostsOwnAddressIsItsInterface(t *testing.T) {
	n := routeWorld(t, routeHeader+route4("eth0", mask0, "0003", "100", mask0), "",
		link{Name: "eth0", Prefixes: prefixes("203.0.113.9/24")},
		link{Name: "teamchat0", Prefixes: prefixes("192.0.2.10/32", "2001:db8::10/128")})
	wantRoute(t, n, "192.0.2.10/32", host.Route{Interface: "teamchat0"})
	wantRoute(t, n, "2001:db8::10/128", host.Route{Interface: "teamchat0"})
	// A host prefix that is nobody's address falls to the table.
	wantRoute(t, n, "192.0.2.11/32", host.Route{Interface: "eth0", Default: true})
	// An address of the host inside a wider network does not make the interface route the network.
	wantRoute(t, n, "192.0.2.0/24", host.Route{Interface: "eth0", Default: true})
}

func TestRouteFailsOnWhatItCannotRead(t *testing.T) {
	n := routeWorld(t, "", "")
	if _, _, err := n.Route(netip.MustParsePrefix("192.0.2.0/24")); err == nil {
		t.Fatal("Route succeeded without a route table")
	}
	n = routeWorld(t, routeHeader+"eth1\t000200C0\t0\n", "")
	if _, _, err := n.Route(netip.MustParsePrefix("192.0.2.0/24")); err == nil {
		t.Fatal("Route succeeded over a line that does not parse")
	}
	n = routeWorld(t, routeHeader, "")
	n.links = func() ([]link, error) { return nil, errRouteLine }
	if _, _, err := n.Route(netip.MustParsePrefix("192.0.2.10/32")); err == nil {
		t.Fatal("Route succeeded when the interfaces could not be read")
	}
}

const (
	dest203    = "007100CB" // 203.0.113.0
	dest203top = "807100CB" // 203.0.113.128
	mask25     = "80FFFFFF"
)

// Plan 00257 HOST-7: a network is judged whole, never by its first address. Only a route that
// covers all of it routes it, and a route inside it through another interface is named, so the
// apply can refuse a network whose members would arrive on an interface no rule admits.
func TestHOST7ANetworkIsJudgedWhole(t *testing.T) {
	split := routeWorld(t, routeHeader+
		route4("eth0", mask0, "0003", "100", mask0)+
		route4("eth0", dest203, "0001", "100", mask25)+
		route4("vpn0", dest203top, "0001", "100", mask25), "")
	wantRoute(t, split, "203.0.113.0/24", host.Route{Interface: "eth0", Default: true, Split: "vpn0"})
	inside := routeWorld(t, routeHeader+
		route4("eth1", dest203, "0001", "100", mask24)+
		route4("vpn0", dest203top, "0001", "100", mask25), "")
	wantRoute(t, inside, "203.0.113.0/24", host.Route{Interface: "eth1", Split: "vpn0"})
	same := routeWorld(t, routeHeader+
		route4("eth1", dest203, "0001", "100", mask24)+
		route4("eth1", dest203top, "0001", "100", mask25), "")
	wantRoute(t, same, "203.0.113.0/24", host.Route{Interface: "eth1"})
	wantRoute(t, inside, "203.0.113.128/25", host.Route{Interface: "vpn0"})
	// The IPv6 table's host route of the host's own address, on loopback, splits nothing.
	own := routeWorld(t, routeHeader,
		route6(dest2001, "20", "00000400", "00000001", "eth2")+
			route6(dest2001[:len(dest2001)-1]+"1", "80", "00000000", "00000001", "lo"))
	wantRoute(t, own, "2001:db8::/32", host.Route{Interface: "eth2"})
}
