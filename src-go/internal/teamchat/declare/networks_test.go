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

func TestEffectiveNetworksNameABridgeFailure(t *testing.T) {
	boom := errors.New("the bridge is gone")
	in := inputWith(t, func(d map[string]any) { d["lxc_members"] = []any{lxcEntry("alpha")} })
	_, err := in.EffectiveNetworks(func() (netip.Prefix, error) { return netip.Prefix{}, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}
