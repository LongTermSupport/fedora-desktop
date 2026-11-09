package netpolicy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// BusIface is the host's agent bus interface (Plan 00161); LXCBridge the LXC dev sites' bridge.
const (
	BusIface  = "agentbus0"
	LXCBridge = "lxcbr0"
)

// Iface is one host interface and its addresses, each with its prefix length.
type Iface struct {
	Name  string
	Addrs []netip.Prefix
}

// Host is what the policy reads of the host's interfaces (N1, N2, N3).
type Host struct {
	Global      []netip.Addr   // every address that is neither loopback nor link-local
	Bus         []netip.Addr   // agentbus0's
	LXCPrefixes []netip.Prefix // lxcbr0's
}

// ReadHost sorts the interfaces' addresses into what the policy needs.
func ReadHost(ifaces []Iface) Host {
	var h Host
	for _, i := range ifaces {
		for _, p := range i.Addrs {
			a := p.Addr().Unmap().WithZone("")
			if a.IsLoopback() || a.IsLinkLocalUnicast() {
				continue
			}
			h.Global = append(h.Global, a)
			switch i.Name {
			case BusIface:
				h.Bus = append(h.Bus, a)
			case LXCBridge:
				h.LXCPrefixes = append(h.LXCPrefixes, p.Masked())
			}
		}
	}
	return h
}

// SystemIfaces is this machine's interfaces.
func SystemIfaces() ([]Iface, error) {
	nics, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("listing the host's interfaces: %w", err)
	}
	out := make([]Iface, 0, len(nics))
	for _, nic := range nics {
		addrs, err := nic.Addrs()
		if err != nil {
			return nil, fmt.Errorf("the addresses of %s: %w", nic.Name, err)
		}
		i := Iface{Name: nic.Name}
		for _, a := range addrs {
			if p, err := netip.ParsePrefix(a.String()); err == nil {
				i.Addrs = append(i.Addrs, p)
			}
		}
		out = append(out, i)
	}
	return out, nil
}

// Nameservers is the nameserver lines of a resolv.conf, zones dropped.
func Nameservers(resolvConf []byte) []netip.Addr {
	var out []netip.Addr
	for line := range bytes.Lines(resolvConf) {
		fields := strings.Fields(string(line))
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}
		if a, err := netip.ParseAddr(fields[1]); err == nil {
			out = append(out, a.Unmap().WithZone(""))
		}
	}
	return out
}

// LXCAddrs is every hosts-file address inside one of the bridge's prefixes (the `lxc` keyword).
func LXCAddrs(hosts []byte, bridge []netip.Prefix) []netip.Addr {
	var out []netip.Addr
	for line := range bytes.Lines(hosts) {
		fields := strings.Fields(string(line))
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		a, err := netip.ParseAddr(fields[0])
		if err != nil || slices.Contains(out, a) {
			continue
		}
		if slices.ContainsFunc(bridge, func(p netip.Prefix) bool { return p.Contains(a) }) {
			out = append(out, a)
		}
	}
	return out
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// HostFile is the checkout's policy file on the host (N3): under the user's configuration,
// never in the checkout, so no container sees it. The name is the checkout's, then the start
// of the SHA-256 of its path, so two checkouts of one name keep two files.
func HostFile(home, checkout string) string {
	sum := sha256.Sum256([]byte(checkout))
	name := strings.Trim(unsafeName.ReplaceAllString(filepath.Base(checkout), "-"), "-")
	return filepath.Join(home, ".config", "claude-yolo", "network", name+"-"+hex.EncodeToString(sum[:])[:16]+".env")
}
