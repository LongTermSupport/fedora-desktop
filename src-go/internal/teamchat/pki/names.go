package pki

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strings"
)

const (
	maxIPs      = 64
	maxDNSBytes = 253
)

var dnsLabelRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// Names are what a leaf is reached by: IP addresses and, optionally, one DNS name (P3).
type Names struct {
	IPs []netip.Addr
	DNS string
}

// canonical is the names checked, with the addresses sorted and once each.
func (n Names) canonical() (Names, error) {
	if len(n.IPs) == 0 && n.DNS == "" {
		return Names{}, errors.New("pki: no name: a leaf needs at least one IP address or a DNS name")
	}
	if len(n.IPs) > maxIPs {
		return Names{}, fmt.Errorf("pki: %d IP names, at most %d", len(n.IPs), maxIPs)
	}
	for _, ip := range n.IPs {
		if !ip.IsValid() || ip.Zone() != "" || ip.Is4In6() || ip.IsUnspecified() || ip.IsMulticast() {
			return Names{}, fmt.Errorf("pki: IP name %q is not a specific unicast address in its plain form", ip.String())
		}
	}
	if n.DNS != "" && !validDNS(n.DNS) {
		return Names{}, fmt.Errorf("pki: DNS name %q is not lower-case ASCII DNS labels", n.DNS)
	}
	ips := slices.Clone(n.IPs)
	slices.SortFunc(ips, netip.Addr.Compare)
	return Names{IPs: slices.Compact(ips), DNS: n.DNS}, nil
}

// same says two canonical name sets are the same set.
func (n Names) same(other Names) bool {
	return n.DNS == other.DNS && slices.Equal(n.IPs, other.IPs)
}

// validDNS says name is lower-case ASCII DNS labels, not an IP literal.
func validDNS(name string) bool {
	if len(name) > maxDNSBytes {
		return false
	}
	if _, err := netip.ParseAddr(name); err == nil {
		return false
	}
	for label := range strings.SplitSeq(name, ".") {
		if !dnsLabelRE.MatchString(label) {
			return false
		}
	}
	return true
}
