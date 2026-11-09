package netpolicy

import (
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
)

// listing is `nft -j list table inet ccy_netpolicy` as libnftables-json writes it, for the
// policy's sets (each element in the form elem gives it) and rules rule entries in the chain.
func listing(t *testing.T, p Policy, elem func(netip.Prefix) any, rules int) []byte {
	t.Helper()
	items := make([]any, 0, 9+rules)
	items = append(items,
		map[string]any{"metainfo": map[string]any{"version": "1.1.6", "json_schema_version": 1}},
		map[string]any{"table": map[string]any{"family": "inet", "name": Table, "handle": 1}})
	for _, s := range setsOf(p) {
		set := map[string]any{"family": "inet", "name": s.name, "table": Table, "type": s.kind, "flags": []string{"interval"}}
		if len(s.elems) > 0 {
			elems := make([]any, 0, len(s.elems))
			for _, e := range s.elems {
				elems = append(elems, elem(e))
			}
			set["elem"] = elems
		}
		items = append(items, map[string]any{"set": set})
	}
	items = append(items, map[string]any{"chain": map[string]any{"family": "inet", "table": Table, "name": "output", "type": "filter", "hook": "output", "prio": 0, "policy": "accept"}})
	for i := range rules {
		items = append(items, map[string]any{"rule": map[string]any{"family": "inet", "table": Table, "chain": "output", "handle": 10 + i, "expr": []any{}}})
	}
	out, err := json.Marshal(map[string]any{"nftables": items})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// asNft is an element as nft lists it: a bare address for a host, else a prefix object.
func asNft(p netip.Prefix) any {
	if p.IsSingleIP() {
		return p.Addr().String()
	}
	return map[string]any{"prefix": map[string]any{"addr": p.Addr().String(), "len": p.Bits()}}
}

func verifyPolicy(t *testing.T) Policy {
	t.Helper()
	in := hostShape()
	in.Allow = mustList(t, "203.0.113.7:22")
	return mustCompute(t, in)
}

func TestVerifyAcceptsTheTableAsListed(t *testing.T) {
	p := verifyPolicy(t)
	if err := Verify(p, listing(t, p, asNft, p.RuleCount())); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyAcceptsMergedAndWrappedElements(t *testing.T) {
	p := verifyPolicy(t)
	// auto-merge may list two adjacent prefixes as one range, and an element may come wrapped.
	p.Allow4 = []netip.Prefix{mustPrefix(t, "198.51.100.0/25"), mustPrefix(t, "198.51.100.128/25")}
	out := listing(t, p, asNft, p.RuleCount())
	merged := `[{"range":["198.51.100.0","198.51.100.255"]}]`
	wrapped := `[{"elem":{"val":{"prefix":{"addr":"198.51.100.0","len":24}}}}]`
	for _, elems := range []string{merged, wrapped} {
		text := strings.Replace(string(out), `[{"prefix":{"addr":"198.51.100.0","len":25}},{"prefix":{"addr":"198.51.100.128","len":25}}]`, elems, 1)
		if text == string(out) {
			t.Fatal("the fixture did not change")
		}
		if err := Verify(p, []byte(text)); err != nil {
			t.Errorf("%s: %v", elems, err)
		}
	}
}

func TestVerifyRefuses(t *testing.T) {
	p := verifyPolicy(t)
	missing := p
	missing.Block4 = append([]netip.Prefix{mustPrefix(t, "192.0.2.0/24")}, p.Block4...)
	extra := p
	extra.Allow4 = append([]netip.Prefix{mustPrefix(t, "198.51.100.0/24")}, p.Allow4...)
	cases := map[string]struct {
		listed []byte
		want   string
	}{
		"a computed prefix is not in the table": {listing(t, p, asNft, p.RuleCount()), "block4"},
		"the table allows more than computed":   {listing(t, extra, asNft, extra.RuleCount()), "allow4"},
		"a rule is missing":                     {listing(t, p, asNft, p.RuleCount()-1), "rules"},
		"not JSON":                              {[]byte("table inet x {"), "JSON"},
		"no output chain":                       {[]byte(`{"nftables":[{"table":{"family":"inet","name":"ccy_netpolicy"}}]}`), "output chain"},
		"an element of an unknown form":         {[]byte(strings.Replace(string(listing(t, p, asNft, p.RuleCount())), `"elem":[`, `"elem":[{"set":"x"},`, 1)), "element"},
	}
	for name, c := range cases {
		policy := p
		if name == "a computed prefix is not in the table" {
			policy = missing
		}
		err := Verify(policy, c.listed)
		if err == nil {
			t.Errorf("%s: accepted", name)
		} else if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v does not name %q", name, err, c.want)
		}
	}
}

func TestVerifyAllowSets(t *testing.T) {
	p := verifyPolicy(t)
	if err := VerifyAllow(p, listing(t, p, asNft, 0)); err != nil {
		t.Fatalf("the allow sets alone: %v", err)
	}
	other := p
	other.Allow4 = nil
	if err := VerifyAllow(other, listing(t, p, asNft, 0)); err == nil || !strings.Contains(err.Error(), "allow4") {
		t.Fatalf("a narrowed set still holding the old element: %v", err)
	}
}
