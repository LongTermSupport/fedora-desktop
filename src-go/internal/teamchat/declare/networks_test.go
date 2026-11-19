package declare

import (
	"errors"
	"net/netip"
	"slices"
	"testing"
)

func prefixes(s ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(s))
	for _, p := range s {
		out = append(out, netip.MustParsePrefix(p))
	}
	return out
}

// bridgeAt is a bridge lookup that answers prefix and counts its asks.
func bridgeAt(prefix string, asked *int) func() (netip.Prefix, error) {
	return func() (netip.Prefix, error) {
		*asked++
		return netip.MustParsePrefix(prefix), nil
	}
}

func inputWith(t *testing.T, edit func(doc map[string]any)) Input {
	t.Helper()
	doc := inputDoc()
	edit(doc)
	return readInput(t, doc)
}

// X3 terms: the effective networks are the declared ones, the bus address as a host prefix, and
// the bridge's subnet while a present LXC member needs it; sorted, each once.
func TestEffectiveNetworks(t *testing.T) {
	asked := 0
	in := inputWith(t, func(d map[string]any) {
		docTeam(d)["networks"] = []any{"198.51.100.0/24", "192.0.2.0/24", "2001:db8::/32"}
		d["address"] = "192.0.2.10"
		d["lxc_members"] = []any{lxcEntry("alpha")}
	})
	got, err := in.EffectiveNetworks(bridgeAt("203.0.113.5/24", &asked))
	if err != nil {
		t.Fatal(err)
	}
	want := prefixes("192.0.2.0/24", "192.0.2.10/32", "198.51.100.0/24", "203.0.113.0/24", "2001:db8::/32")
	if !slices.Equal(got.All, want) {
		t.Fatalf("All = %v, want %v", got.All, want)
	}
	if !slices.Equal(got.Own, prefixes("192.0.2.10/32", "203.0.113.0/24")) {
		t.Fatalf("Own = %v", got.Own)
	}
	if asked != 1 {
		t.Fatalf("the bridge was asked %d times", asked)
	}
}

// X3: no LXC member, no bridge asked; no address declared, no host prefix.
func TestEffectiveNetworksWithoutBridgeOrAddress(t *testing.T) {
	asked := 0
	in := inputWith(t, func(d map[string]any) {
		delete(d, "address")
		delete(d, "lxc_members")
		docTeam(d)["url"] = "https://chat.example.com:8448"
		docTeam(d)["networks"] = []any{"192.0.2.0/24"}
	})
	got, err := in.EffectiveNetworks(bridgeAt("203.0.113.0/24", &asked))
	if err != nil || asked != 0 || !slices.Equal(got.All, prefixes("192.0.2.0/24")) || len(got.Own) != 0 {
		t.Fatalf("got %+v, asked %d, %v", got, asked, err)
	}
	// Only an absent member: the bridge is not wanted either.
	in = inputWith(t, func(d map[string]any) {
		member := lxcEntry("alpha")
		member["state"] = "absent"
		d["lxc_members"] = []any{member}
	})
	if got, err = in.EffectiveNetworks(bridgeAt("203.0.113.0/24", &asked)); err != nil || asked != 0 {
		t.Fatalf("an absent member asked for the bridge: %+v, asked %d, %v", got, asked, err)
	}
}

// X3: a declared network equal to the bus address's host prefix, or to the bridge, is one network.
func TestEffectiveNetworksAreDeduplicated(t *testing.T) {
	asked := 0
	in := inputWith(t, func(d map[string]any) {
		docTeam(d)["networks"] = []any{"192.0.2.10/32", "203.0.113.0/24"}
		d["lxc_members"] = []any{lxcEntry("alpha")}
	})
	got, err := in.EffectiveNetworks(bridgeAt("203.0.113.1/24", &asked))
	if err != nil {
		t.Fatal(err)
	}
	if want := prefixes("192.0.2.10/32", "203.0.113.0/24"); !slices.Equal(got.All, want) {
		t.Fatalf("All = %v, want %v", got.All, want)
	}
}

func TestEffectiveNetworksOfAnIPv6BusAddress(t *testing.T) {
	asked := 0
	in := inputWith(t, func(d map[string]any) {
		d["address"] = "2001:db8::10"
		delete(d, "lxc_members")
		docTeam(d)["networks"] = []any{}
	})
	got, err := in.EffectiveNetworks(bridgeAt("203.0.113.0/24", &asked))
	if err != nil || !slices.Equal(got.All, prefixes("2001:db8::10/128")) {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func addrs(s ...string) []netip.Addr {
	out := make([]netip.Addr, 0, len(s))
	for _, a := range s {
		out = append(out, netip.MustParseAddr(a))
	}
	return out
}

// lxcInput is the sample input with url and one LXC member in state, or none when state is "".
func lxcInput(t *testing.T, url, state string) Input {
	t.Helper()
	return inputWith(t, func(d map[string]any) {
		docTeam(d)["url"] = url
		delete(d, "lxc_members")
		if state != "" {
			member := lxcEntry("alpha")
			member["state"] = state
			d["lxc_members"] = []any{member}
		}
	})
}

// X5: a team with a present LXC member is reached by its guests on the LXC bridge, at the team's
// listen address there, over https. The url serves the team's other seats and may be a name or
// any address; a team with no listen address on the bridge, or a plain http url (loopback only,
// X12), would pass every run with no member joined (each guest check offline), so it is refused.
func TestLXCURLProblem(t *testing.T) {
	// asks is how often the bridge and the listen set are asked: the listen set only for an https
	// url off the bridge, so a team whose url names the bridge is judged as it always was.
	type asks struct{ bridge, listen int }
	cases := []struct {
		name, url, bridge, want string
		listen                  []netip.Addr
		member                  string // the member's state; "" is none at all
		asks                    asks
	}{
		{name: "on the bridge", url: "https://203.0.113.1:8448", bridge: "203.0.113.1/24", listen: addrs("203.0.113.1"), member: "present", asks: asks{1, 0}},
		{name: "an IPv6 bridge", url: "https://[2001:db8::1]:8448", bridge: "2001:db8::/64", listen: addrs("2001:db8::1"), member: "present", asks: asks{1, 0}},
		{name: "another address", url: "https://192.0.2.10:8448", bridge: "203.0.113.1/24", listen: addrs("192.0.2.10", "203.0.113.1"), member: "present", asks: asks{1, 1}},
		{name: "a name", url: "https://chat.example.com:8448", bridge: "203.0.113.1/24", listen: addrs("192.0.2.10", "203.0.113.1"), member: "present", asks: asks{1, 1}},
		{
			name: "no listen address on the bridge", url: "https://192.0.2.10:8448", bridge: "203.0.113.1/24", listen: addrs("192.0.2.10"), member: "present", asks: asks{1, 1},
			want: "the team has no listen address inside the LXC bridge's subnet 203.0.113.0/24",
		},
		{
			name: "plain http", url: "http://127.0.0.1:8448", bridge: "203.0.113.1/24", listen: addrs("203.0.113.1"), member: "present", asks: asks{1, 0},
			want: "the url is plain http, which a guest refuses off loopback; an LXC member needs an https url",
		},
		{name: "only an absent member", url: "http://127.0.0.1:8448", bridge: "203.0.113.1/24", member: "absent"},
		{name: "no member", url: "http://127.0.0.1:8448", bridge: "203.0.113.1/24"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got asks
			listen := func() ([]netip.Addr, error) {
				got.listen++
				return c.listen, nil
			}
			problem, err := lxcInput(t, c.url, c.member).LXCURLProblem(listen, bridgeAt(c.bridge, &got.bridge))
			if err != nil || problem != c.want {
				t.Fatalf("problem %q, %v; want %q", problem, err, c.want)
			}
			if got != c.asks {
				t.Fatalf("asked %+v, want %+v", got, c.asks)
			}
		})
	}
	boom := errors.New("the bridge is gone")
	in := inputWith(t, func(d map[string]any) { d["lxc_members"] = []any{lxcEntry("alpha")} })
	noListen := func() ([]netip.Addr, error) { return nil, boom }
	if _, err := in.LXCURLProblem(noListen, func() (netip.Prefix, error) { return netip.Prefix{}, boom }); !errors.Is(err, boom) {
		t.Fatalf("a bridge failure: %v", err)
	}
	in = inputWith(t, func(d map[string]any) {
		d["lxc_members"] = []any{lxcEntry("alpha")}
		docTeam(d)["url"] = "https://chat.example.com:8448"
	})
	asked := 0
	if _, err := in.LXCURLProblem(noListen, bridgeAt("203.0.113.1/24", &asked)); !errors.Is(err, boom) {
		t.Fatalf("a listen set failure: %v", err)
	}
}

// X5: an LXC seat's url. A url already on the bridge is kept byte for byte (a team of LXC members
// alone is served exactly as before); any other is the team's listen address inside the bridge's
// subnet, at the team's port, over https.
func TestLXCSeatURL(t *testing.T) {
	subnet := netip.MustParsePrefix("203.0.113.0/24")
	cases := []struct {
		name, url, want string
		port            int
		listen          []netip.Addr
		subnet          netip.Prefix
	}{
		{name: "on the bridge", url: "https://203.0.113.1:8448", port: 8448, listen: addrs("203.0.113.1"), subnet: subnet, want: "https://203.0.113.1:8448"},
		{name: "on the bridge at 443", url: "https://203.0.113.1", port: 443, listen: addrs("203.0.113.1"), subnet: subnet, want: "https://203.0.113.1"},
		{name: "a name", url: "https://chat.example.com:8448", port: 8448, listen: addrs("192.0.2.10", "203.0.113.1"), subnet: subnet, want: "https://203.0.113.1:8448"},
		{name: "another address", url: "https://192.0.2.10:9000", port: 9000, listen: addrs("192.0.2.10", "203.0.113.1"), subnet: subnet, want: "https://203.0.113.1:9000"},
		{name: "an IPv6 bridge", url: "https://chat.example.com:8448", port: 8448, listen: addrs("192.0.2.10", "2001:db8::1"), subnet: netip.MustParsePrefix("2001:db8::/64"), want: "https://[2001:db8::1]:8448"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := LXCSeatURL(Team{URL: c.url, Port: c.port}, c.listen, c.subnet)
			if err != nil || got != c.want {
				t.Fatalf("LXCSeatURL = %q, %v; want %q", got, err, c.want)
			}
		})
	}
	for name, team := range map[string]Team{
		"plain http":  {URL: "http://127.0.0.1:8448", Port: 8448},
		"not a url":   {URL: "https://[::1", Port: 8448},
		"off the set": {URL: "https://chat.example.com:8448", Port: 8448},
	} {
		listen := addrs("203.0.113.1")
		if name == "off the set" {
			listen = addrs("192.0.2.10")
		}
		if got, err := LXCSeatURL(team, listen, subnet); err == nil {
			t.Errorf("%s: LXCSeatURL = %q, no error", name, got)
		}
	}
}

func TestEffectiveNetworksNameABridgeFailure(t *testing.T) {
	boom := errors.New("the bridge is gone")
	in := inputWith(t, func(d map[string]any) { d["lxc_members"] = []any{lxcEntry("alpha")} })
	_, err := in.EffectiveNetworks(func() (netip.Prefix, error) { return netip.Prefix{}, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}
