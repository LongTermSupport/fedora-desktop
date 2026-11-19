package declare

import (
	"cmp"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
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

// LXCURLProblem is why the team cannot serve its present LXC members, "" when it can (X5). Their
// guests reach the homeserver at LXCSeatURL, its listen address on the LXC bridge over https: a
// team with no listen address there, or a plain http url (loopback only, X12), passes every apply
// with each guest check offline and no member joined. The url serves the team's other seats and
// may be a name or any address. listen is the team's listen set, asked only when the url is not
// already on the bridge (so a team whose url names the bridge is judged as it always was); bridge
// is asked only while a present LXC member needs it.
func (in Input) LXCURLProblem(listen func() ([]netip.Addr, error), bridge func() (netip.Prefix, error)) (string, error) {
	if !in.hasPresentLXC() {
		return "", nil
	}
	subnet, err := bridge()
	if err != nil {
		return "", fmt.Errorf("the LXC bridge's subnet: %w", err)
	}
	if base, problem := urlOnBridge(in.Team, subnet); base != "" || problem != "" {
		return problem, nil
	}
	addrs, err := listen()
	if err != nil {
		return "", fmt.Errorf("the team's listen set: %w", err)
	}
	_, problem := listenURL(in.Team, addrs, subnet)
	return problem, nil
}

// LXCSeatURL is the base url a seat of kind lxc is given (X5): the team's url when its host is
// already an address inside the bridge's subnet, kept byte for byte, else https at the team's
// listen address inside that subnet and the team's port. Every other seat kind is given the url.
// A plain http url, or no listen address inside the subnet, is an error: no guest could reach it.
func LXCSeatURL(team Team, listen []netip.Addr, subnet netip.Prefix) (string, error) {
	base, problem := urlOnBridge(team, subnet)
	if base == "" && problem == "" {
		base, problem = listenURL(team, listen, subnet)
	}
	if problem != "" {
		return "", errors.New(problem)
	}
	return base, nil
}

// urlOnBridge is the team's url when it is https at an address inside subnet, or why no https url
// can serve a guest; both empty when the url is https elsewhere.
func urlOnBridge(team Team, subnet netip.Prefix) (string, string) {
	u, err := url.Parse(team.URL)
	switch {
	case err != nil:
		return "", fmt.Sprintf("url %q: %v", team.URL, err)
	case u.Scheme != "https":
		return "", "the url is plain http, which a guest refuses off loopback; an LXC member needs an https url"
	}
	if addr, isAddr := parseAddr(u.Hostname()); isAddr && subnet.Masked().Contains(addr.Unmap()) {
		return team.URL, ""
	}
	return "", ""
}

// listenURL is https at the team's first listen address inside subnet and its port, or why there
// is none.
func listenURL(team Team, listen []netip.Addr, subnet netip.Prefix) (string, string) {
	subnet = subnet.Masked()
	i := slices.IndexFunc(listen, func(a netip.Addr) bool { return subnet.Contains(a.Unmap()) })
	if i < 0 {
		return "", fmt.Sprintf("the team has no listen address inside the LXC bridge's subnet %s", subnet)
	}
	return "https://" + net.JoinHostPort(listen[i].Unmap().String(), strconv.Itoa(team.Port)), ""
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
