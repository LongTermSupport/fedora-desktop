package netprobe

import (
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"
)

func TestBoundAddresses(t *testing.T) {
	cases := []struct {
		name, out string
		want      []string
	}{
		{"empty", "", nil},
		{"one v4", "LISTEN 0 1024 127.0.0.1:8008 0.0.0.0:*\n", []string{"127.0.0.1"}},
		{
			"v4 and v6 sorted unique",
			"LISTEN 0 1024 [2001:db8::1]:8008 [::]:*\nLISTEN 0 1024 127.0.0.1:8008 0.0.0.0:*\nLISTEN 0 1024 127.0.0.1:8008 0.0.0.0:*\n",
			[]string{"127.0.0.1", "2001:db8::1"},
		},
		{"wildcard v4", "LISTEN 0 1024 0.0.0.0:8008 0.0.0.0:*\n", []string{"0.0.0.0"}},
		{"wildcard star", "LISTEN 0 1024 *:8008 *:*\n", []string{"*"}},
		{"wildcard v6", "LISTEN 0 1024 [::]:8008 [::]:*\n", []string{"::"}},
		{"zone on v6", "LISTEN 0 1024 [fe80::1%eth0]:8008 [::]:*\n", []string{"fe80::1"}},
		{"zone on v4", "LISTEN 0 1024 127.0.0.1%lo:8008 0.0.0.0:*\n", []string{"127.0.0.1"}},
		{"mapped v4", "LISTEN 0 1024 [::ffff:192.0.2.1]:8008 [::]:*\n", []string{"::ffff:192.0.2.1"}},
		{"tabs and runs of spaces", "LISTEN\t0\t1024\t127.0.0.1:8008\t0.0.0.0:*\n", []string{"127.0.0.1"}},
		{"leading spaces", "   LISTEN 0 1024 127.0.0.1:8008 0.0.0.0:*\n", []string{"127.0.0.1"}},
		{"crlf", "LISTEN 0 1024 127.0.0.1:8008 0.0.0.0:*\r\n", []string{"127.0.0.1"}},
		{"short row skipped", "LISTEN 0 1024\n\nLISTEN 0 1024 127.0.0.1:8008 0.0.0.0:*\n", []string{"127.0.0.1"}},
		{"no trailing newline", "LISTEN 0 1024 127.0.0.1:8008 0.0.0.0:*", []string{"127.0.0.1"}},
		{"four fields exactly", "LISTEN 0 1024 127.0.0.1:8008", []string{"127.0.0.1"}},
		{
			"unsorted", "LISTEN 0 1 192.0.2.9:80 x\nLISTEN 0 1 127.0.0.1:80 x\nLISTEN 0 1 192.0.2.10:80 x\n",
			[]string{"127.0.0.1", "192.0.2.10", "192.0.2.9"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BoundAddresses(tc.out)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExpectedAddresses(t *testing.T) {
	cases := []struct {
		name   string
		listen []string
		want   []string
	}{
		{"none", nil, []string{"127.0.0.1"}},
		{"one", []string{"192.0.2.1"}, []string{"127.0.0.1", "192.0.2.1"}},
		{"duplicate of loopback", []string{"127.0.0.1", "192.0.2.1"}, []string{"127.0.0.1", "192.0.2.1"}},
		{"duplicates", []string{"192.0.2.1", "192.0.2.1"}, []string{"127.0.0.1", "192.0.2.1"}},
		{"unsorted v6", []string{"2001:db8::2", "2001:db8::1"}, []string{"127.0.0.1", "2001:db8::1", "2001:db8::2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExpectedAddresses(tc.listen); !slices.Equal(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAssertListening(t *testing.T) {
	ok := "LISTEN 0 1024 127.0.0.1:8008 0.0.0.0:*\nLISTEN 0 1024 192.0.2.1:8008 0.0.0.0:*\n"
	if err := AssertListening(8008, ok, []string{"192.0.2.1"}); err != nil {
		t.Fatal(err)
	}
	err := AssertListening(8008, ok+"LISTEN 0 1024 0.0.0.0:8008 0.0.0.0:*\n", []string{"192.0.2.1"})
	want := "port 8008 is bound on [0.0.0.0 127.0.0.1 192.0.2.1], not exactly [127.0.0.1 192.0.2.1]"
	if err == nil || err.Error() != want {
		t.Fatalf("error %v, want %q", err, want)
	}
	err = AssertListening(8008, "", []string{"192.0.2.1"})
	if err == nil || err.Error() != "port 8008 is bound on [], not exactly [127.0.0.1 192.0.2.1]" {
		t.Fatalf("error %v", err)
	}
	if err := AssertListening(8008, "LISTEN 0 1 127.0.0.1:8008 x\n", []string{"192.0.2.1"}); err == nil {
		t.Fatal("a missing listen address was accepted")
	}
}

func TestRouteDev(t *testing.T) {
	good := map[string]string{
		`[{"dst":"192.0.2.5","gateway":"192.0.2.1","dev":"eth0","prefsrc":"192.0.2.9"}]`: "eth0",
		`[{"dev":"enp1s0"}]`:                    "enp1s0",
		`[ { "dev" : "wlan0" } ]`:               "wlan0",
		`[{"dev":"br-1a2b3c"},{"dev":"other"}]`: "br-1a2b3c",
		`[{"dev":"veth@if2"}]`:                  "veth@if2",
	}
	for in, want := range good {
		got, err := RouteDev([]byte(in))
		if err != nil || got != want {
			t.Errorf("RouteDev(%s) = %q, %v; want %q", in, got, err, want)
		}
	}
	none := []string{`[]`, `[{}]`, `[{"dev":null}]`, `[{"dev":""}]`, `null`, `[null]`}
	for _, in := range none {
		got, err := RouteDev([]byte(in))
		if !errors.Is(err, ErrNoRoute) || got != "" {
			t.Errorf("RouteDev(%s) = %q, %v; want ErrNoRoute", in, got, err)
		}
	}
	bad := []string{
		``, `not json`, `{"dev":"eth0"}`, `[{"dev":5}]`, `[{"dev":["a"]}]`, `[{"dev":"a/b"}]`, `[{"dev":"a b"}]`,
		`[{"dev":"a\nb"}]`, `[{"dev":".."}]`, `[{"dev":"."}]`, `[{"dev":"abcdefghijklmnop"}]`, `[{"dev":"a\u0000b"}]`,
		`[{"dev":"--add-source=x"}]`, `[{"dev":"-x"}]`, `[{"dev":"x=y"}]`,
	}
	for _, in := range bad {
		got, err := RouteDev([]byte(in))
		if err == nil || errors.Is(err, ErrNoRoute) || got != "" {
			t.Errorf("RouteDev(%q) = %q, %v; want a refusal", in, got, err)
		}
	}
}

func TestParseZone(t *testing.T) {
	good := map[string]string{
		"public\n":               "public",
		"public":                 "public",
		"  FedoraWorkstation \n": "FedoraWorkstation",
		"my-zone_1.x\n":          "my-zone_1.x",
		"trusted":                "trusted",
	}
	for in, want := range good {
		zone, none, err := ParseZone(in)
		if err != nil || none || zone != want {
			t.Errorf("ParseZone(%q) = %q, %v, %v", in, zone, none, err)
		}
	}
	// B12: the text "no zone" means no zone, whatever the exit status was.
	for _, in := range []string{"no zone", "no zone\n", "  no zone  \n"} {
		zone, none, err := ParseZone(in)
		if err != nil || !none || zone != "" {
			t.Errorf("ParseZone(%q) = %q, %v, %v", in, zone, none, err)
		}
	}
	for _, in := range []string{"", "\n", "two words", "a\nb", "-bad", "a=b", "a/b", "Error: INVALID_INTERFACE", "zone\x00", "no zone at all", "NO ZONE"} {
		if zone, none, err := ParseZone(in); err == nil {
			t.Errorf("ParseZone(%q) = %q, %v, accepted", in, zone, none)
		}
	}
}

func TestHasLine(t *testing.T) {
	out := "Wired connection 1\nagentbus0\nagentbus00\n agentbus1\n"
	if !HasLine(out, "agentbus0") {
		t.Fatal("exact line not found")
	}
	for _, name := range []string{"agentbus", "agentbus1", "bus0", "", "Wired"} {
		if HasLine(out, name) {
			t.Errorf("HasLine(%q) matched a partial line", name)
		}
	}
	if !HasLine("a\r\nagentbus0\r\n", "agentbus0") {
		t.Error("CRLF line not found")
	}
	if HasLine("", "x") {
		t.Error("matched in nothing")
	}
}

func TestSettings(t *testing.T) {
	v4 := Settings(netip.MustParseAddr("192.0.2.9"))
	if got := strings.Join(v4, "|"); got != "ipv4.method|manual|ipv4.addresses|192.0.2.9/32|ipv6.method|disabled|ipv6.addresses|" {
		t.Fatalf("v4 settings %q", got)
	}
	v6 := Settings(netip.MustParseAddr("2001:db8::1"))
	if got := strings.Join(v6, "|"); got != "ipv4.method|disabled|ipv4.addresses||ipv6.method|manual|ipv6.addresses|2001:db8::1/128" {
		t.Fatalf("v6 settings %q", got)
	}
}

func TestAddressesMatch(t *testing.T) {
	v4 := netip.MustParseAddr("192.0.2.9")
	v6 := netip.MustParseAddr("2001:db8::1")
	cases := []struct {
		name string
		out  string
		addr netip.Addr
		want bool
	}{
		{"v4 exact", "192.0.2.9/32\n--\n", v4, true},
		{"v4 without second line", "192.0.2.9/32\n", v4, true},
		{"v4 empty v6 as blank", "192.0.2.9/32\n\n", v4, true},
		{"v6 exact (nmcli -g escapes colons)", "--\n2001\\:db8\\:\\:1/128\n", v6, true},
		{"v6 exact unescaped", "\n2001:db8::1/128\n", v6, true},
		{"v6 non-canonical text of the same address", "--\n2001:0db8::1/128\n", v6, true},
		{"v4 wrong address", "192.0.2.10/32\n--\n", v4, false},
		{"v4 wrong prefix", "192.0.2.9/24\n--\n", v4, false},
		{"v4 extra address (comma)", "192.0.2.9/32, 192.0.2.10/32\n--\n", v4, false},
		{"v4 extra address (pipe)", "192.0.2.9/32 | 192.0.2.10/32\n--\n", v4, false},
		{"v4 with an unwanted v6", "192.0.2.9/32\n2001\\:db8\\:\\:1/128\n", v4, false},
		{"v6 with an unwanted v4", "192.0.2.9/32\n2001\\:db8\\:\\:1/128\n", v6, false},
		{"nothing configured", "--\n--\n", v4, false},
		{"empty output", "", v4, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := AddressesMatch(tc.out, tc.addr)
			if err != nil || got != tc.want {
				t.Fatalf("AddressesMatch = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
	for _, out := range []string{"garbage\n--\n", "192.0.2.9\n--\n", "192.0.2.9/33\n--\n", "--\nnot-an-address/128\n"} {
		if _, err := AddressesMatch(out, v4); err == nil {
			t.Errorf("AddressesMatch(%q) accepted unparseable output", out)
		}
	}
}
