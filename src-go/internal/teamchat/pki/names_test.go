package pki

import (
	"net/netip"
	"strings"
	"testing"
)

func TestP3NamesAreCheckedBeforeAnythingIsMade(t *testing.T) {
	m := newMaker()
	ca := mustCA(t, m, "team-a")
	many := make([]netip.Addr, 65)
	for i := range many {
		many[i] = netip.AddrFrom4([4]byte{192, 0, 2, byte(i)})
	}
	label64 := strings.Repeat("a", 64)
	cases := map[string]Names{
		"nothing":                       {},
		"the zero address":              {IPs: []netip.Addr{{}}},
		"an unspecified IPv4 address":   {IPs: ips(t, "0.0.0.0")},
		"an unspecified IPv6 address":   {IPs: ips(t, "::")},
		"an IPv4-mapped IPv6 address":   {IPs: ips(t, "::ffff:192.0.2.1")},
		"an address with a zone":        {IPs: ips(t, "fe80::1%eth0")},
		"a multicast address":           {IPs: ips(t, "224.0.0.1")},
		"more than 64 addresses":        {IPs: many},
		"an upper-case DNS name":        {DNS: "HS.example.test"},
		"an IP literal as the DNS":      {DNS: "192.0.2.1"},
		"an IPv6 literal as the DNS":    {DNS: "2001:db8::1"},
		"an empty label":                {DNS: "hs..example.test"},
		"a leading dot":                 {DNS: ".example.test"},
		"a trailing dot":                {DNS: "example.test."},
		"a label of 64 characters":      {DNS: label64 + ".test"},
		"an underscore":                 {DNS: "hs_a.example.test"},
		"a leading hyphen":              {DNS: "-hs.example.test"},
		"a trailing hyphen":             {DNS: "hs-.example.test"},
		"a name over 253 characters":    {DNS: strings.Repeat("abcdefghi.", 26) + "test"},
		"a space":                       {DNS: "hs example.test"},
		"a wildcard":                    {DNS: "*.example.test"},
		"a name of other than ASCII":    {DNS: "hé.example.test"},
		"a valid address and a bad DNS": {IPs: ips(t, "192.0.2.1"), DNS: "bad name"},
	}
	for name, names := range cases {
		if _, err := m.NewLeaf(ca, names); err == nil {
			t.Errorf("NewLeaf accepted %s", name)
		}
	}
	for name, names := range map[string]Names{
		"64 addresses":       {IPs: many[:64]},
		"a label of 63":      {DNS: strings.Repeat("a", 63) + ".test"},
		"a one-label name":   {DNS: "homeserver"},
		"a name with digits": {DNS: "hs-2.example.test"},
		"loopback":           {IPs: ips(t, "127.0.0.1", "::1")},
		"documentation IPv6": {IPs: ips(t, "2001:db8::1")},
	} {
		if _, err := m.NewLeaf(ca, names); err != nil {
			t.Errorf("NewLeaf refused %s: %v", name, err)
		}
	}
}

func TestP3ARefusalSaysWhichNameWasWrong(t *testing.T) {
	m := newMaker()
	ca := mustCA(t, m, "team-a")
	for names, want := range map[string]Names{"DNS": {DNS: "Bad Name"}, "IP": {IPs: ips(t, "0.0.0.0")}, "no name": {}} {
		_, err := m.NewLeaf(ca, want)
		if err == nil || !strings.Contains(err.Error(), names) {
			t.Errorf("NewLeaf(%+v) error = %v, want one that says %q", want, err, names)
		}
	}
}
