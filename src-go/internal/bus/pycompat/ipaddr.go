package pycompat

import (
	"strconv"
	"strings"
)

// IPAddress is str(ipaddress.ip_address(s)), or false where python raises ValueError. IPv4 is
// four dotted decimal octets, strictly (ASCII digits, at most three, no leading zero, at most
// 255). IPv6 follows _BaseV6._ip_int_from_string (an IPv4 tail allowed, one "::"), with an
// optional "%scope" that is non-empty and has no second '%', and no '/' anywhere. The answer
// is lower-case with RFC 5952's compression (the longest run of two or more zero groups, the
// first on a tie), the scope kept, and an IPv4-mapped address written dotted as python 3.13
// and later write it (SPEC-view.md B-S5; python 3.11 wrote hex groups).
func IPAddress(s string) (string, bool) {
	if v4, ok := parseIPv4(s); ok {
		return formatIPv4(v4), true
	}
	if strings.Contains(s, "/") {
		return "", false
	}
	addr, scope, hasScope := strings.Cut(s, "%")
	if hasScope && (scope == "" || strings.Contains(scope, "%")) {
		return "", false
	}
	groups, ok := parseIPv6(addr)
	if !ok {
		return "", false
	}
	out := formatIPv6(groups)
	if hasScope {
		out += "%" + scope
	}
	return out, true
}

func parseIPv4(s string) ([4]byte, bool) {
	var out [4]byte
	octets := strings.Split(s, ".")
	if len(octets) != len(out) {
		return out, false
	}
	for i, o := range octets {
		if o == "" || len(o) > 3 || (len(o) > 1 && o[0] == '0') {
			return out, false
		}
		n := 0
		for _, c := range []byte(o) {
			if c < '0' || c > '9' {
				return out, false
			}
			n = n*10 + int(c-'0')
		}
		if n > 255 {
			return out, false
		}
		out[i] = byte(n)
	}
	return out, true
}

func formatIPv4(b [4]byte) string {
	parts := make([]string, 0, len(b))
	for _, o := range b {
		parts = append(parts, strconv.Itoa(int(o)))
	}
	return strings.Join(parts, ".")
}

func parseHextet(h string) (uint16, bool) {
	if h == "" || len(h) > 4 {
		return 0, false
	}
	n, err := strconv.ParseUint(h, 16, 16)
	if err != nil {
		return 0, false
	}
	return uint16(n), true
}

// layout is how many groups come before (hi) and after (lo) the "::" of an address, and how
// many zero groups it stands for.
type layout struct{ hi, lo, skipped int }

// ipv6Layout is the layout of parts, following _ip_int_from_string's checks.
func ipv6Layout(parts []string) (layout, bool) {
	skip := -1
	for i := 1; i < len(parts)-1; i++ {
		if parts[i] == "" {
			if skip >= 0 {
				return layout{}, false
			}
			skip = i
		}
	}
	if skip < 0 {
		ok := len(parts) == 8 && parts[0] != "" && parts[len(parts)-1] != ""
		return layout{hi: len(parts)}, ok
	}
	l := layout{hi: skip, lo: len(parts) - skip - 1}
	if parts[0] == "" {
		if l.hi--; l.hi != 0 {
			return layout{}, false
		}
	}
	if parts[len(parts)-1] == "" {
		if l.lo--; l.lo != 0 {
			return layout{}, false
		}
	}
	l.skipped = 8 - (l.hi + l.lo)
	return l, l.skipped >= 1
}

func parseIPv6(s string) ([8]uint16, bool) {
	var groups [8]uint16
	parts := strings.Split(s, ":")
	if len(parts) < 3 {
		return groups, false
	}
	if last := parts[len(parts)-1]; strings.Contains(last, ".") {
		v4, ok := parseIPv4(last)
		if !ok {
			return groups, false
		}
		parts = append(parts[:len(parts)-1],
			strconv.FormatUint(uint64(v4[0])<<8|uint64(v4[1]), 16), strconv.FormatUint(uint64(v4[2])<<8|uint64(v4[3]), 16))
	}
	if len(parts) > 9 {
		return groups, false
	}
	l, ok := ipv6Layout(parts)
	if !ok {
		return groups, false
	}
	at := 0
	for _, h := range parts[:l.hi] {
		if groups[at], ok = parseHextet(h); !ok {
			return groups, false
		}
		at++
	}
	at += l.skipped
	for _, h := range parts[len(parts)-l.lo:] {
		if groups[at], ok = parseHextet(h); !ok {
			return groups, false
		}
		at++
	}
	return groups, true
}

func formatIPv6(g [8]uint16) string {
	if g[0] == 0 && g[1] == 0 && g[2] == 0 && g[3] == 0 && g[4] == 0 && g[5] == 0xFFFF {
		return "::ffff:" + strconv.Itoa(int(g[6]>>8)) + "." + strconv.Itoa(int(g[6]&0xFF)) + "." +
			strconv.Itoa(int(g[7]>>8)) + "." + strconv.Itoa(int(g[7]&0xFF))
	}
	bestStart, bestLen, runStart, runLen := -1, 0, -1, 0
	for i, h := range g {
		if h != 0 {
			runStart, runLen = -1, 0
			continue
		}
		if runStart < 0 {
			runStart = i
		}
		if runLen++; runLen > bestLen {
			bestStart, bestLen = runStart, runLen
		}
	}
	hex := make([]string, 0, len(g))
	for _, h := range g {
		hex = append(hex, strconv.FormatUint(uint64(h), 16))
	}
	if bestLen < 2 {
		return strings.Join(hex, ":")
	}
	return strings.Join(hex[:bestStart], ":") + "::" + strings.Join(hex[bestStart+bestLen:], ":")
}
