package netpolicy

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"slices"
)

// outputChain is the chain the policy hooks, and the hook it is on.
const outputChain = "output"

// interval is a run of addresses, both ends included.
type interval struct{ lo, hi netip.Addr }

type listed struct {
	sets   map[string][]interval
	output bool
	rules  int
}

type nftItem struct {
	Set *struct {
		Name  string            `json:"name"`
		Table string            `json:"table"`
		Elem  []json.RawMessage `json:"elem"`
	} `json:"set"`
	Chain *struct {
		Name   string `json:"name"`
		Table  string `json:"table"`
		Hook   string `json:"hook"`
		Policy string `json:"policy"`
	} `json:"chain"`
	Rule *struct {
		Table string `json:"table"`
		Chain string `json:"chain"`
	} `json:"rule"`
}

// Verify is N5: the table read back from the container's namespace (`nft -j list table`)
// holds exactly the computed sets, the output chain, and every rule.
func Verify(p Policy, out []byte) error {
	l, err := parseListing(out)
	if err != nil {
		return err
	}
	if !l.output {
		return errors.New("the table read back has no output chain hooked on output with policy accept")
	}
	if l.rules != p.RuleCount() {
		return fmt.Errorf("the output chain holds %d rules, %d were applied", l.rules, p.RuleCount())
	}
	return matchSets(setsOf(p), l)
}

// VerifyAllow is N9's check after a refresh: both allow sets hold exactly what was computed.
func VerifyAllow(p Policy, out []byte) error {
	l, err := parseListing(out)
	if err != nil {
		return err
	}
	return matchSets(setsOf(p)[:2], l)
}

func matchSets(want []namedSet, l listed) error {
	for _, s := range want {
		got, ok := l.sets[s.name]
		if !ok {
			return fmt.Errorf("the table read back has no set %s", s.name)
		}
		computed := make([]interval, 0, len(s.elems))
		for _, p := range s.elems {
			computed = append(computed, prefixInterval(p))
		}
		if x, ok := uncovered(computed, got); !ok {
			return fmt.Errorf("set %s read back does not hold %s-%s, which was applied", s.name, x.lo, x.hi)
		}
		if x, ok := uncovered(got, computed); !ok {
			return fmt.Errorf("set %s read back holds %s-%s, which was not applied", s.name, x.lo, x.hi)
		}
	}
	return nil
}

func parseListing(out []byte) (listed, error) {
	var doc struct {
		Nftables []nftItem `json:"nftables"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return listed{}, fmt.Errorf("the table read back is not nft's JSON: %w", err)
	}
	l := listed{sets: map[string][]interval{}}
	for _, item := range doc.Nftables {
		switch {
		case item.Set != nil && item.Set.Table == Table:
			ivs := []interval{}
			for _, raw := range item.Set.Elem {
				iv, err := parseElem(raw)
				if err != nil {
					return listed{}, fmt.Errorf("set %s: %w", item.Set.Name, err)
				}
				ivs = append(ivs, iv)
			}
			l.sets[item.Set.Name] = ivs
		case item.Chain != nil && item.Chain.Table == Table && item.Chain.Name == outputChain:
			l.output = item.Chain.Hook == outputChain && item.Chain.Policy == "accept"
		case item.Rule != nil && item.Rule.Table == Table && item.Rule.Chain == outputChain:
			l.rules++
		}
	}
	return l, nil
}

// parseElem reads one set element in any form libnftables-json gives it: an address, a
// prefix object, a range, or any of those wrapped in an elem object.
func parseElem(raw json.RawMessage) (interval, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		a, err := netip.ParseAddr(text)
		if err != nil {
			return interval{}, fmt.Errorf("an element %s is not an address", text)
		}
		return interval{a, a}, nil
	}
	var obj struct {
		Prefix *struct {
			Addr string `json:"addr"`
			Len  int    `json:"len"`
		} `json:"prefix"`
		Range []string `json:"range"`
		Elem  *struct {
			Val json.RawMessage `json:"val"`
		} `json:"elem"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return interval{}, fmt.Errorf("an element %s is unreadable: %w", raw, err)
	}
	switch {
	case obj.Prefix != nil:
		a, err := netip.ParseAddr(obj.Prefix.Addr)
		if err != nil {
			return interval{}, fmt.Errorf("an element %s is not a prefix", raw)
		}
		p, err := a.Prefix(obj.Prefix.Len)
		if err != nil {
			return interval{}, fmt.Errorf("an element %s is not a prefix", raw)
		}
		return prefixInterval(p), nil
	case len(obj.Range) == 2:
		lo, errLo := netip.ParseAddr(obj.Range[0])
		hi, errHi := netip.ParseAddr(obj.Range[1])
		if errLo != nil || errHi != nil || hi.Less(lo) {
			return interval{}, fmt.Errorf("an element %s is not a range", raw)
		}
		return interval{lo, hi}, nil
	case obj.Elem != nil:
		return parseElem(obj.Elem.Val)
	}
	return interval{}, fmt.Errorf("an element %s is of a form this check does not read", raw)
}

func prefixInterval(p netip.Prefix) interval {
	p = p.Masked()
	b := p.Addr().AsSlice()
	for i := p.Bits(); i < len(b)*8; i++ {
		b[i/8] |= 0x80 >> (i % 8)
	}
	hi, ok := netip.AddrFromSlice(b)
	if !ok {
		return interval{p.Addr(), p.Addr()}
	}
	return interval{p.Addr(), hi}
}

// merge joins overlapping and adjacent intervals, sorted.
func merge(in []interval) []interval {
	s := slices.Clone(in)
	slices.SortFunc(s, func(a, b interval) int { return a.lo.Compare(b.lo) })
	var out []interval
	for _, iv := range s {
		if n := len(out); n > 0 && out[n-1].lo.BitLen() == iv.lo.BitLen() {
			last := &out[n-1]
			next := last.hi.Next()
			if !next.IsValid() || !next.Less(iv.lo) {
				if last.hi.Less(iv.hi) {
					last.hi = iv.hi
				}
				continue
			}
		}
		out = append(out, iv)
	}
	return out
}

// uncovered is the first interval of xs that no run of cover holds, false when there is one.
func uncovered(xs, cover []interval) (interval, bool) {
	runs := merge(cover)
	for _, x := range xs {
		if !slices.ContainsFunc(runs, func(r interval) bool {
			return r.lo.BitLen() == x.lo.BitLen() && !x.lo.Less(r.lo) && !r.hi.Less(x.hi)
		}) {
			return x, false
		}
	}
	return interval{}, true
}
