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

// X5: a team with a present LXC member is reached by its guests on the LXC bridge, so its url's
// host is an address inside the bridge's subnet. A url elsewhere, or a name, would pass every run
// with no member joined (each guest check offline), so it is refused, naming the subnet.
func TestBridgeURLProblem(t *testing.T) {
	cases := []struct {
		name, url, bridge, want string
		absent, none            bool
	}{
		{name: "on the bridge", url: "https://203.0.113.1:8448", bridge: "203.0.113.1/24"},
		{name: "an IPv6 bridge", url: "https://[2001:db8::1]:8448", bridge: "2001:db8::/64"},
		{name: "off the bridge", url: "https://192.0.2.10:8448", bridge: "203.0.113.1/24", want: "192.0.2.10 is not inside the LXC bridge's subnet 203.0.113.0/24"},
		{name: "a name", url: "https://chat.example.com:8448", bridge: "203.0.113.1/24", want: "chat.example.com is a name, not an address inside the LXC bridge's subnet 203.0.113.0/24"},
		{name: "only an absent member", url: "https://192.0.2.10:8448", bridge: "203.0.113.1/24", absent: true},
		{name: "no member", url: "https://192.0.2.10:8448", bridge: "203.0.113.1/24", none: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			asked := 0
			in := inputWith(t, func(d map[string]any) {
				docTeam(d)["url"] = c.url
				member := lxcEntry("alpha")
				if c.absent {
					member["state"] = "absent"
				}
				d["lxc_members"] = []any{member}
				if c.none {
					delete(d, "lxc_members")
				}
			})
			got, err := in.BridgeURLProblem(bridgeAt(c.bridge, &asked))
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Fatalf("problem %q, want %q", got, c.want)
			}
			if wantAsked := map[bool]int{true: 0, false: 1}[c.absent || c.none]; asked != wantAsked {
				t.Fatalf("the bridge was asked %d times, want %d", asked, wantAsked)
			}
		})
	}
	boom := errors.New("the bridge is gone")
	in := inputWith(t, func(d map[string]any) { d["lxc_members"] = []any{lxcEntry("alpha")} })
	if _, err := in.BridgeURLProblem(func() (netip.Prefix, error) { return netip.Prefix{}, boom }); !errors.Is(err, boom) {
		t.Fatalf("a bridge failure: %v", err)
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
