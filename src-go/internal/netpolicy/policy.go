// Package netpolicy is a ccy session's network policy (Plan 00232, issue #114): what a session
// may reach, held as an nftables table in the container's own network namespace, applied from
// the host before the session's first instruction runs. The ccy-network-policy command is its
// front; the bash launcher sends its container run through `ccy-network-policy launch`.
//
// Intended behaviour, each held by a test:
//
//   - N1. The default refuses the private and LAN ranges (10/8, 172.16/12, 192.168/16,
//     100.64/10, 169.254/16, fe80::/10, fc00::/7, built from octets) and every global address
//     of the host itself, and keeps the public internet open.
//   - N2. Always open, derived on the host, never listed by hand: loopback, the container's
//     own nameservers on port 53 (read once, before start), the agent bus address on
//     agentbus0 on every port, and the subnet of every project network the session joins
//     (never the shared bridge `podman`, so sessions do not reach each other). The checkout's
//     seat bundles are never read: the container writes them (DESIGN.md section 10).
//   - N3. The owner widens it with CCY_NETWORK_ALLOW, in a host file no container sees
//     (HostFile) and for one launch in the host shell; the two add up. An entry is an
//     address, a prefix, a host name (resolved on the host at launch), any of them with
//     `:port` (an IPv6 address as `[addr]:port`), `lxc` (the LXC dev-site addresses in the
//     host's hosts file inside lxcbr0's prefix) or `off` (no policy). Anything else, an
//     unresolvable name or `lxc` with no address found, stops the launch naming it.
//   - N4. Each set is normalised (an entry another covers is dropped, duplicates merge) and
//     carries auto-merge, because the inputs overlap by nature (F21). A refusal is ICMP
//     admin-prohibited: it fails at once, "No route to host" (F33).
//   - N5. The table is applied with `podman unshare nsenter -t <pid> -n nft -f -` after
//     `create` and `init` and before `start`, and read back (`nft -j list table`): every
//     computed prefix must be covered and each allow set must hold nothing else. Any
//     failure there removes the container and stops the launch (fail closed).
//   - N6. The created container must not be privileged and must not hold NET_ADMIN or
//     SYS_ADMIN, or the launch stops: those would let it lift the table.
//   - N7. Where enforcement is unavailable (the Docker engine, no nft on the host) the
//     session runs unprotected, with a warning on stderr; `off` runs it unprotected on the
//     owner's word. Either way the session is told.
//   - N8. Every session is told its limits: a `CCY NETWORK POLICY` block appended to the
//     Claude Code system prompt (added to the agent's command), and the same text mounted
//     read-only at /run/ccy/network-policy.
//   - N9. `refresh` (run by `ccy --connect` and `--disconnect`) recomputes the allow sets of
//     every live session from its current networks and replaces them whole in one
//     transaction (a `delete element` can fail on a merged interval), verifies them, and
//     rewrites the context file in place.
package netpolicy

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Table is the nft table the policy is held in, in the container's own namespace.
const Table = "ccy_netpolicy"

// AllowVar is the setting an allow-list is written in.
const AllowVar = "CCY_NETWORK_ALLOW"

// PortAllow is an address or prefix opened on one port (TCP and UDP).
type PortAllow struct {
	Prefix netip.Prefix `json:"prefix"`
	Port   uint16       `json:"port"`
}

// NameAllow is a host name to resolve on the host; Port 0 is every port.
type NameAllow struct {
	Name string `json:"name"`
	Port uint16 `json:"port"`
}

// Allow is the owner's allow-list as parsed (N3).
type Allow struct {
	Off      bool           `json:"off"`
	LXC      bool           `json:"lxc"`
	Prefixes []netip.Prefix `json:"prefixes"`
	Ports    []PortAllow    `json:"ports"`
	Names    []NameAllow    `json:"names"`
	Entries  []string       `json:"entries"` // every entry as written, for the context
}

var (
	labelRE  = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$`)
	digitsRE = regexp.MustCompile(`^[0-9]+$`)
	valueRE  = regexp.MustCompile(`^[A-Za-z0-9.,:/\[\] -]*$`)
)

// ParseList parses a comma-separated allow-list.
func ParseList(list string) (Allow, error) {
	var a Allow
	for raw := range strings.SplitSeq(list, ",") {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if err := addEntry(&a, entry); err != nil {
			return Allow{}, fmt.Errorf("%s entry %q: %w", AllowVar, entry, err)
		}
		a.Entries = append(a.Entries, entry)
	}
	return a, nil
}

func addEntry(a *Allow, entry string) error {
	switch strings.ToLower(entry) {
	case "off":
		a.Off = true
		return nil
	case "lxc":
		a.LXC = true
		return nil
	}
	host, port, err := splitPort(entry)
	if err != nil {
		return err
	}
	if p, ok := parseAddrOrPrefix(host); ok {
		if port == 0 {
			a.Prefixes = append(a.Prefixes, p)
		} else {
			a.Ports = append(a.Ports, PortAllow{p, port})
		}
		return nil
	}
	if strings.Contains(host, ":") || strings.Contains(host, "/") || !isName(host) {
		return errors.New("not an address, a prefix (with no host bits set) or a host name")
	}
	a.Names = append(a.Names, NameAllow{strings.ToLower(host), port})
	return nil
}

// splitPort is an entry's host part and its port, 0 for none.
func splitPort(entry string) (string, uint16, error) {
	if rest, ok := strings.CutPrefix(entry, "["); ok {
		addr, portText, found := strings.Cut(rest, "]:")
		if !found {
			return "", 0, errors.New("a bracketed IPv6 address must be followed by :port")
		}
		port, err := parsePort(portText)
		return addr, port, err
	}
	if strings.Count(entry, ":") != 1 {
		return entry, 0, nil
	}
	i := strings.IndexByte(entry, ':')
	port, err := parsePort(entry[i+1:])
	return entry[:i], port, err
}

func parsePort(text string) (uint16, error) {
	n, err := strconv.ParseUint(text, 10, 16)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("the port %q is not 1 to 65535", text)
	}
	return uint16(n), nil
}

func parseAddrOrPrefix(text string) (netip.Prefix, bool) {
	if addr, err := netip.ParseAddr(text); err == nil && addr.Zone() == "" {
		return netip.PrefixFrom(addr.Unmap(), addr.Unmap().BitLen()), true
	}
	p, err := netip.ParsePrefix(text)
	if err != nil || p != p.Masked() {
		return netip.Prefix{}, false
	}
	return p, true
}

func isName(host string) bool {
	labels := strings.Split(host, ".")
	if len(host) > 253 || digitsRE.MatchString(labels[len(labels)-1]) {
		return false
	}
	for _, l := range labels {
		if len(l) > 63 || !labelRE.MatchString(l) {
			return false
		}
	}
	return true
}

// ParseFile parses the host file (N3): blank lines, # comments and CCY_NETWORK_ALLOW=<list>
// lines, the value optionally quoted. It is parsed, never sourced; any other line is refused.
func ParseFile(text []byte) (Allow, error) {
	var all Allow
	for i, raw := range bytes.Split(text, []byte("\n")) {
		line := strings.TrimSpace(string(raw))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		value, ok := strings.CutPrefix(line, AllowVar+"=")
		if !ok {
			return Allow{}, fmt.Errorf("line %d: only %s=<entries> lines and # comments are read here", i+1, AllowVar)
		}
		value = unquote(value)
		if !valueRE.MatchString(value) {
			return Allow{}, fmt.Errorf("line %d: %s holds a character no entry has", i+1, AllowVar)
		}
		a, err := ParseList(value)
		if err != nil {
			return Allow{}, fmt.Errorf("line %d: %w", i+1, err)
		}
		all = all.Merge(a)
	}
	return all, nil
}

func unquote(v string) string {
	for _, q := range []string{`"`, `'`} {
		if len(v) >= 2 && strings.HasPrefix(v, q) && strings.HasSuffix(v, q) {
			return v[1 : len(v)-1]
		}
	}
	return v
}

// Merge is both lists' entries; off or lxc in either holds.
func (a Allow) Merge(b Allow) Allow {
	return Allow{
		Off:      a.Off || b.Off,
		LXC:      a.LXC || b.LXC,
		Prefixes: slices.Concat(a.Prefixes, b.Prefixes),
		Ports:    slices.Concat(a.Ports, b.Ports),
		Names:    slices.Concat(a.Names, b.Names),
		Entries:  slices.Concat(a.Entries, b.Entries),
	}
}

// DefaultBlock is the ranges the default refuses (N1), built from octets: RFC 1918, CGNAT
// (RFC 6598), IPv4 and IPv6 link-local, and IPv6 unique-local (RFC 4193).
func DefaultBlock() []netip.Prefix {
	octets := func(a, b byte, bits int) netip.Prefix {
		return netip.PrefixFrom(netip.AddrFrom4([4]byte{a, b, 0, 0}), bits)
	}
	return []netip.Prefix{
		octets(10, 0, 8), octets(172, 16, 12), octets(192, 168, 16), octets(100, 64, 10), octets(169, 254, 16),
		netip.PrefixFrom(netip.AddrFrom16([16]byte{0xfe, 0x80}), 10),
		netip.PrefixFrom(netip.AddrFrom16([16]byte{0xfc}), 7),
	}
}

// Normalise masks each prefix, sorts them, and drops every one another covers (N4).
func Normalise(in []netip.Prefix) []netip.Prefix {
	sorted := make([]netip.Prefix, 0, len(in))
	for _, p := range in {
		sorted = append(sorted, p.Masked())
	}
	slices.SortFunc(sorted, comparePrefix)
	out := make([]netip.Prefix, 0, len(sorted))
	for _, p := range sorted {
		if !slices.ContainsFunc(out, func(q netip.Prefix) bool { return q.Bits() <= p.Bits() && q.Contains(p.Addr()) }) {
			out = append(out, p)
		}
	}
	return out
}

func comparePrefix(a, b netip.Prefix) int {
	return cmp.Or(a.Addr().Compare(b.Addr()), cmp.Compare(a.Bits(), b.Bits()))
}

// Inputs is everything a policy is computed from: the owner's list and what the host found.
type Inputs struct {
	Allow       Allow                   `json:"allow"`
	Resolved    map[string][]netip.Addr `json:"resolved"`    // each NameAllow's addresses, resolved on the host
	Bus         []netip.Addr            `json:"bus"`         // agentbus0's addresses
	HostAddrs   []netip.Addr            `json:"hostAddrs"`   // the host's own global addresses
	Subnets     []netip.Prefix          `json:"subnets"`     // the project networks the session joins
	LXC         []netip.Addr            `json:"lxc"`         // the LXC dev-site addresses, when Allow.LXC
	Nameservers []netip.Addr            `json:"nameservers"` // the container's own, read before start
}

// Policy is one session's table, normalised.
type Policy struct {
	Allow4, Allow6, DNS4, DNS6, Block4, Block6 []netip.Prefix
	Ports                                      []PortAllow
}

// Compute is the policy for the inputs, or why there can be none.
func Compute(in Inputs) (Policy, error) {
	if in.Allow.Off {
		return Policy{}, errors.New("the policy is off for this checkout")
	}
	if in.Allow.LXC && len(in.LXC) == 0 {
		return Policy{}, errors.New("lxc is in the allow-list, but the host has no LXC dev-site address (no hosts-file entry inside lxcbr0's prefix)")
	}
	allow := slices.Concat(in.Allow.Prefixes, in.Subnets, hostPrefixes(in.Bus), hostPrefixes(in.LXC))
	ports := slices.Clone(in.Allow.Ports)
	for _, n := range in.Allow.Names {
		addrs := in.Resolved[n.Name]
		if len(addrs) == 0 {
			return Policy{}, fmt.Errorf("the allowed host name %s did not resolve on the host", n.Name)
		}
		for _, p := range hostPrefixes(addrs) {
			if n.Port == 0 {
				allow = append(allow, p)
			} else {
				ports = append(ports, PortAllow{p, n.Port})
			}
		}
	}
	var p Policy
	p.Allow4, p.Allow6 = split(Normalise(allow))
	p.DNS4, p.DNS6 = split(Normalise(hostPrefixes(in.Nameservers)))
	p.Block4, p.Block6 = split(Normalise(slices.Concat(DefaultBlock(), hostPrefixes(in.HostAddrs))))
	p.Ports = normalisePorts(ports)
	return p, nil
}

func hostPrefixes(addrs []netip.Addr) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(addrs))
	for _, a := range addrs {
		a = a.Unmap().WithZone("")
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out
}

func split(ps []netip.Prefix) ([]netip.Prefix, []netip.Prefix) {
	var v4, v6 []netip.Prefix
	for _, p := range ps {
		if p.Addr().Is4() {
			v4 = append(v4, p)
		} else {
			v6 = append(v6, p)
		}
	}
	return v4, v6
}

func normalisePorts(in []PortAllow) []PortAllow {
	out := make([]PortAllow, 0, len(in))
	for _, pa := range in {
		out = append(out, PortAllow{pa.Prefix.Masked(), pa.Port})
	}
	slices.SortFunc(out, func(a, b PortAllow) int {
		return cmp.Or(comparePrefix(a.Prefix, b.Prefix), cmp.Compare(a.Port, b.Port))
	})
	return slices.Compact(out)
}

// element is a prefix as nft writes it: a bare address for a single host.
func element(p netip.Prefix) string {
	if p.IsSingleIP() {
		return p.Addr().String()
	}
	return p.String()
}

func elements(ps []netip.Prefix) string {
	parts := make([]string, 0, len(ps))
	for _, p := range ps {
		parts = append(parts, element(p))
	}
	return strings.Join(parts, ", ")
}

const (
	ipv4Type = "ipv4_addr"
	ipv6Type = "ipv6_addr"
)

type namedSet struct {
	name, kind string
	elems      []netip.Prefix
}

func setsOf(p Policy) []namedSet {
	return []namedSet{
		{"allow4", ipv4Type, p.Allow4},
		{"allow6", ipv6Type, p.Allow6},
		{"dns4", ipv4Type, p.DNS4},
		{"dns6", ipv6Type, p.DNS6},
		{"block4", ipv4Type, p.Block4},
		{"block6", ipv6Type, p.Block6},
	}
}

// rules are the output chain's rules, in order: every allow before any block.
func rulesOf(p Policy) []string {
	rules := []string{`oifname "lo" accept`, "ip daddr @allow4 accept", "ip6 daddr @allow6 accept"}
	for _, pa := range p.Ports {
		family := "ip6"
		if pa.Prefix.Addr().Is4() {
			family = "ip"
		}
		for _, proto := range []string{"tcp", "udp"} {
			rules = append(rules, fmt.Sprintf("%s daddr %s %s dport %d accept", family, element(pa.Prefix), proto, pa.Port))
		}
	}
	return append(rules,
		"ip daddr @dns4 udp dport 53 accept", "ip daddr @dns4 tcp dport 53 accept",
		"ip6 daddr @dns6 udp dport 53 accept", "ip6 daddr @dns6 tcp dport 53 accept",
		"ip daddr @block4 reject with icmpx admin-prohibited", "ip6 daddr @block6 reject with icmpx admin-prohibited")
}

// RuleCount is how many rules the output chain holds.
func (p Policy) RuleCount() int { return len(rulesOf(p)) }

// Ruleset is the whole table, for `nft -f -` in the container's own namespace (N4, N5).
func (p Policy) Ruleset() string {
	var b strings.Builder
	b.WriteString("table inet " + Table + " {\n")
	for _, s := range setsOf(p) {
		fmt.Fprintf(&b, "    set %s {\n        type %s\n        flags interval\n        auto-merge\n", s.name, s.kind)
		if len(s.elems) > 0 {
			fmt.Fprintf(&b, "        elements = { %s }\n", elements(s.elems))
		}
		b.WriteString("    }\n")
	}
	b.WriteString("    chain output {\n        type filter hook output priority filter; policy accept;\n")
	for _, r := range rulesOf(p) {
		b.WriteString("        " + r + "\n")
	}
	b.WriteString("    }\n}\n")
	return b.String()
}

// AllowUpdate replaces both allow sets whole, as one `nft -f -` transaction (N9).
func (p Policy) AllowUpdate() string {
	var b strings.Builder
	for _, s := range setsOf(p)[:2] {
		fmt.Fprintf(&b, "flush set inet %s %s\n", Table, s.name)
		if len(s.elems) > 0 {
			fmt.Fprintf(&b, "add element inet %s %s { %s }\n", Table, s.name, elements(s.elems))
		}
	}
	return b.String()
}
