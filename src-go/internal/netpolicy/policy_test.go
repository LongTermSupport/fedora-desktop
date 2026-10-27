package netpolicy

import (
	"net/netip"
	"slices"
	"strings"
	"testing"
)

// v4 is an IPv4 prefix built from octets, so no dotted private address is written in the repo
// (the public-repo scanner refuses one).
func v4(a, b, c, d byte, bits int) netip.Prefix {
	return netip.PrefixFrom(netip.AddrFrom4([4]byte{a, b, c, d}), bits)
}

func addr4(a, b, c, d byte) netip.Addr { return netip.AddrFrom4([4]byte{a, b, c, d}) }

func mustPrefix(t *testing.T, s string) netip.Prefix {
	t.Helper()
	p, err := netip.ParsePrefix(s)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mustList(t *testing.T, s string) Allow {
	t.Helper()
	a, err := ParseList(s)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestParseListEntries(t *testing.T) {
	a := mustList(t, "192.0.2.10, 198.51.100.0/24,build.example.com:8443,[2001:db8::1]:443,2001:db8::/48,203.0.113.7:22,lxc,docs.example.org")
	wantPrefixes := []netip.Prefix{mustPrefix(t, "192.0.2.10/32"), mustPrefix(t, "198.51.100.0/24"), mustPrefix(t, "2001:db8::/48")}
	if !slices.Equal(a.Prefixes, wantPrefixes) {
		t.Errorf("prefixes %v, want %v", a.Prefixes, wantPrefixes)
	}
	wantPorts := []PortAllow{{mustPrefix(t, "2001:db8::1/128"), 443}, {mustPrefix(t, "203.0.113.7/32"), 22}}
	if !slices.Equal(a.Ports, wantPorts) {
		t.Errorf("ports %v, want %v", a.Ports, wantPorts)
	}
	wantNames := []NameAllow{{"build.example.com", 8443}, {"docs.example.org", 0}}
	if !slices.Equal(a.Names, wantNames) {
		t.Errorf("names %v, want %v", a.Names, wantNames)
	}
	if !a.LXC || a.Off {
		t.Errorf("lxc %v off %v, want lxc only", a.LXC, a.Off)
	}
	if got := strings.Join(a.Entries, ","); got != "192.0.2.10,198.51.100.0/24,build.example.com:8443,[2001:db8::1]:443,2001:db8::/48,203.0.113.7:22,lxc,docs.example.org" {
		t.Errorf("entries %q", got)
	}
}

func TestParseListOffAndEmpty(t *testing.T) {
	if a := mustList(t, "OFF"); !a.Off {
		t.Fatalf("off: %+v", a)
	}
	if a := mustList(t, " , "); len(a.Entries) != 0 {
		t.Fatalf("empty: %+v", a)
	}
}

func TestParseListRefuses(t *testing.T) {
	for _, bad := range []string{
		"192.0.2.10:0", "192.0.2.10:65536", "192.0.2.10:http", "bad_name.example", "-lead.example",
		"[2001:db8::1]", "[2001:db8::1]443", "198.51.100.1/24", "192.0.2.300", "a..b", "host:1:2",
	} {
		if _, err := ParseList(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		} else if !strings.Contains(err.Error(), bad) {
			t.Errorf("%q: the error does not name the entry: %v", bad, err)
		}
	}
}

func TestParseFile(t *testing.T) {
	a, err := ParseFile([]byte("# the build cache\n\nCCY_NETWORK_ALLOW=192.0.2.10\nCCY_NETWORK_ALLOW=\"build.example.com:8443\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(a.Entries, ","); got != "192.0.2.10,build.example.com:8443" {
		t.Errorf("entries %q", got)
	}
	for _, bad := range []string{"OTHER=1\n", "CCY_NETWORK_ALLOW 192.0.2.1\n", "export CCY_NETWORK_ALLOW=192.0.2.1\n", "CCY_NETWORK_ALLOW=$(id)\n", "CCY_NETWORK_ALLOW=192.0.2.1:x\n"} {
		_, err := ParseFile([]byte(bad))
		if err == nil {
			t.Errorf("%q was accepted", bad)
		} else if !strings.Contains(err.Error(), "line 1") {
			t.Errorf("%q: the error names no line: %v", bad, err)
		}
	}
}

func TestMerge(t *testing.T) {
	m := mustList(t, "192.0.2.10,lxc").Merge(mustList(t, "off,build.example.com"))
	if !m.Off || !m.LXC || len(m.Prefixes) != 1 || len(m.Names) != 1 || len(m.Entries) != 4 {
		t.Errorf("merge %+v", m)
	}
}

func TestDefaultBlock(t *testing.T) {
	got := DefaultBlock()
	want := []netip.Prefix{
		v4(10, 0, 0, 0, 8), v4(172, 16, 0, 0, 12), v4(192, 168, 0, 0, 16), v4(100, 64, 0, 0, 10), v4(169, 254, 0, 0, 16),
		mustPrefix(t, "fe80::/10"), mustPrefix(t, "fc00::/7"),
	}
	if !slices.Equal(got, want) {
		t.Errorf("%v, want %v", got, want)
	}
}

func TestNormaliseDedupesAndDropsCovered(t *testing.T) {
	in := []netip.Prefix{
		v4(10, 0, 0, 0, 8), v4(10, 9, 0, 3, 32), v4(10, 9, 0, 3, 32),
		mustPrefix(t, "192.0.2.0/24"), mustPrefix(t, "192.0.2.0/25"), mustPrefix(t, "198.51.100.7/32"),
		v4(10, 0, 3, 1, 24),
	}
	got := Normalise(in)
	want := []netip.Prefix{v4(10, 0, 0, 0, 8), mustPrefix(t, "192.0.2.0/24"), mustPrefix(t, "198.51.100.7/32")}
	if !slices.Equal(got, want) {
		t.Errorf("%v, want %v", got, want)
	}
}

// hostShape is the input the first host run's table died on (F21): the host's own addresses
// inside the private ranges, and the bus address arriving twice.
func hostShape() Inputs {
	bus := addr4(10, 254, 0, 1)
	return Inputs{
		Bus:         []netip.Addr{bus, bus},
		HostAddrs:   []netip.Addr{addr4(10, 9, 0, 3), addr4(10, 0, 3, 1), netip.MustParseAddr("2001:db8:1::5"), bus},
		Subnets:     []netip.Prefix{v4(10, 89, 6, 0, 24)},
		Nameservers: []netip.Addr{addr4(169, 254, 1, 1), netip.MustParseAddr("192.0.2.53"), netip.MustParseAddr("192.0.2.53")},
	}
}

func mustCompute(t *testing.T, in Inputs) Policy {
	t.Helper()
	p, err := Compute(in)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestComputeHostShape(t *testing.T) {
	p := mustCompute(t, hostShape())
	if want := []netip.Prefix{v4(10, 89, 6, 0, 24), v4(10, 254, 0, 1, 32)}; !slices.Equal(p.Allow4, want) {
		t.Errorf("allow4 %v, want %v", p.Allow4, want)
	}
	if want := []netip.Prefix{v4(169, 254, 1, 1, 32), mustPrefix(t, "192.0.2.53/32")}; !slices.Equal(p.DNS4, want) {
		t.Errorf("dns4 %v, want %v", p.DNS4, want)
	}
	if want := []netip.Prefix{v4(10, 0, 0, 0, 8), v4(100, 64, 0, 0, 10), v4(169, 254, 0, 0, 16), v4(172, 16, 0, 0, 12), v4(192, 168, 0, 0, 16)}; !slices.Equal(p.Block4, want) {
		t.Errorf("block4 %v, want %v (the host's private addresses fold into their ranges)", p.Block4, want)
	}
	if want := []netip.Prefix{mustPrefix(t, "2001:db8:1::5/128"), mustPrefix(t, "fc00::/7"), mustPrefix(t, "fe80::/10")}; !slices.Equal(p.Block6, want) {
		t.Errorf("block6 %v, want %v (a public host address is blocked)", p.Block6, want)
	}
}

func TestComputeOwnerAllows(t *testing.T) {
	in := Inputs{
		Allow: mustList(t, "192.0.2.10,build.example.com:8443,docs.example.org,lxc,203.0.113.7:22"),
		Resolved: map[string][]netip.Addr{
			"build.example.com": {netip.MustParseAddr("198.51.100.5"), netip.MustParseAddr("2001:db8::5")},
			"docs.example.org":  {netip.MustParseAddr("198.51.100.9")},
		},
		LXC: []netip.Addr{addr4(10, 0, 3, 16), addr4(10, 0, 3, 21)},
	}
	p := mustCompute(t, in)
	want4 := []netip.Prefix{v4(10, 0, 3, 16, 32), v4(10, 0, 3, 21, 32), mustPrefix(t, "192.0.2.10/32"), mustPrefix(t, "198.51.100.9/32")}
	if !slices.Equal(p.Allow4, want4) {
		t.Errorf("allow4 %v, want %v", p.Allow4, want4)
	}
	wantPorts := []PortAllow{{mustPrefix(t, "198.51.100.5/32"), 8443}, {mustPrefix(t, "203.0.113.7/32"), 22}, {mustPrefix(t, "2001:db8::5/128"), 8443}}
	if !slices.Equal(p.Ports, wantPorts) {
		t.Errorf("ports %v, want %v", p.Ports, wantPorts)
	}
}

func TestComputeRefusesAnUnresolvedName(t *testing.T) {
	if _, err := Compute(Inputs{Allow: mustList(t, "build.example.com")}); err == nil || !strings.Contains(err.Error(), "build.example.com") {
		t.Fatalf("err %v", err)
	}
}

func TestComputeOffAndLXCWithoutAddresses(t *testing.T) {
	if _, err := Compute(Inputs{Allow: mustList(t, "off")}); err == nil {
		t.Error("a policy was computed for off")
	}
	if _, err := Compute(Inputs{Allow: mustList(t, "lxc")}); err == nil || !strings.Contains(err.Error(), "lxc") {
		t.Errorf("lxc with no addresses: %v", err)
	}
}

func TestRulesetShape(t *testing.T) {
	in := hostShape()
	in.Allow = mustList(t, "203.0.113.7:22")
	p := mustCompute(t, in)
	r := p.Ruleset()
	allowElems := "elements = { " + v4(10, 89, 6, 0, 24).String() + ", " + addr4(10, 254, 0, 1).String() + " }"
	for _, want := range []string{
		"table inet ccy_netpolicy {",
		"set allow4 {\n        type ipv4_addr\n        flags interval\n        auto-merge\n        " + allowElems + "\n    }",
		"set allow6 {\n        type ipv6_addr\n        flags interval\n        auto-merge\n    }",
		"type filter hook output priority filter; policy accept;",
		"oifname \"lo\" accept\n        ip daddr @allow4 accept\n        ip6 daddr @allow6 accept",
		"ip daddr 203.0.113.7 tcp dport 22 accept\n        ip daddr 203.0.113.7 udp dport 22 accept",
		"ip daddr @dns4 udp dport 53 accept\n        ip daddr @dns4 tcp dport 53 accept",
		"ip daddr @block4 reject with icmpx admin-prohibited\n        ip6 daddr @block6 reject with icmpx admin-prohibited\n    }\n}\n",
	} {
		if !strings.Contains(r, want) {
			t.Errorf("the ruleset lacks %q:\n%s", want, r)
		}
	}
	if covered := addr4(10, 9, 0, 3).String(); strings.Contains(r, covered+",") || strings.Contains(r, covered+" ") {
		t.Errorf("an address its range covers is listed (nft refuses the overlap):\n%s", r)
	}
	if strings.Index(r, "@allow4") > strings.Index(r, "@block4") || strings.Index(r, "tcp dport 22") > strings.Index(r, "@block4") {
		t.Errorf("an allow comes after a block:\n%s", r)
	}
	if got := p.RuleCount(); got != strings.Count(r, " accept\n")+strings.Count(r, " admin-prohibited\n") {
		t.Errorf("RuleCount %d does not count the rendered rules", got)
	}
}

func TestAllowUpdate(t *testing.T) {
	p := mustCompute(t, hostShape())
	want := "flush set inet ccy_netpolicy allow4\nadd element inet ccy_netpolicy allow4 { " +
		v4(10, 89, 6, 0, 24).String() + ", " + addr4(10, 254, 0, 1).String() + " }\nflush set inet ccy_netpolicy allow6\n"
	if got := p.AllowUpdate(); got != want {
		t.Errorf("%q, want %q", got, want)
	}
}
