package declare

import (
	"cmp"
	"fmt"
	"net/netip"
	"net/url"
	"slices"
)

// EffectiveNetworks is the one set of networks a team's homeserver is reached from (X3, X4, X5).
// Every consumer, the unit's IP filter, the firewall rules, the listen addresses and the LXC
// guest source check, reads this set and never the declared list.
type EffectiveNetworks struct {
	// All is every effective network, each once, sorted.
	All []netip.Prefix
	// Own are the networks the host holds itself: its bus address as a host prefix and the LXC
	// bridge's subnet. They may route over the default route (X3); a declared network may not.
	Own []netip.Prefix
}

// EffectiveNetworks is the declared networks, the bus address as a host prefix when one is
// declared, and the bridge's subnet while the team has an LXC member that is present. bridge is
// asked only in that last case, so a host with no bridge serves a team without one.
func (in Input) EffectiveNetworks(bridge func() (netip.Prefix, error)) (EffectiveNetworks, error) {
	var out EffectiveNetworks
	for _, text := range in.Team.Networks {
		p, err := netip.ParsePrefix(text)
		if err != nil {
			return EffectiveNetworks{}, fmt.Errorf("networks: %q: %w", text, err)
		}
		out.All = append(out.All, p)
	}
	if in.Address != "" {
		addr, err := netip.ParseAddr(in.Address)
		if err != nil {
			return EffectiveNetworks{}, fmt.Errorf("address: %q: %w", in.Address, err)
		}
		host := netip.PrefixFrom(addr, addr.BitLen())
		out.All, out.Own = append(out.All, host), append(out.Own, host)
	}
	if in.hasPresentLXC() {
		subnet, err := bridge()
		if err != nil {
			return EffectiveNetworks{}, fmt.Errorf("the LXC bridge's subnet: %w", err)
		}
		subnet = subnet.Masked()
		out.All, out.Own = append(out.All, subnet), append(out.Own, subnet)
	}
	out.All, out.Own = sortedPrefixes(out.All), sortedPrefixes(out.Own)
	return out, nil
}

// BridgeURLProblem is why the team's url cannot serve its present LXC members, "" when it can
// (X5). Their guests reach the homeserver on the LXC bridge, so the url's host must be an address
// inside the bridge's subnet: a url elsewhere, or a name, passes every apply with each guest check
// offline and no member joined. bridge is asked only while a present LXC member needs it.
func (in Input) BridgeURLProblem(bridge func() (netip.Prefix, error)) (string, error) {
	if !in.hasPresentLXC() {
		return "", nil
	}
	subnet, err := bridge()
	if err != nil {
		return "", fmt.Errorf("the LXC bridge's subnet: %w", err)
	}
	subnet = subnet.Masked()
	u, err := url.Parse(in.Team.URL)
	if err != nil {
		return "", fmt.Errorf("url %q: %w", in.Team.URL, err)
	}
	addr, isAddr := parseAddr(u.Hostname())
	switch {
	case !isAddr:
		return fmt.Sprintf("%s is a name, not an address inside the LXC bridge's subnet %s", u.Hostname(), subnet), nil
	case !subnet.Contains(addr.Unmap()):
		return fmt.Sprintf("%s is not inside the LXC bridge's subnet %s", addr, subnet), nil
	}
	return "", nil
}

// parseAddr is host as an IP address, and whether it is one rather than a name.
func parseAddr(host string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(host)
	return addr, err == nil
}

func (in Input) hasPresentLXC() bool {
	return slices.ContainsFunc(in.LXC, func(m LXCMember) bool { return m.State == StatePresent })
}

// sortedPrefixes is list without repeats, by address and then by length.
func sortedPrefixes(list []netip.Prefix) []netip.Prefix {
	out := slices.Clone(list)
	slices.SortFunc(out, func(a, b netip.Prefix) int {
		return cmp.Or(a.Addr().Compare(b.Addr()), cmp.Compare(a.Bits(), b.Bits()))
	})
	return slices.Compact(out)
}
