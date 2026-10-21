package statusline

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	ipv4Octets   = 4
	hextetCount  = 8
	maxHextetLen = 4
)

// pyIP is str(ipaddress.ip_address(value)) for a value with no '%': the address in Python
// 3.11's canonical form, false where it raises. Python prints an IPv4-mapped address in hex
// (::ffff:102:304), which netip does not, so both parsers and the printer follow ipaddress.
func pyIP(value string) (string, bool) {
	if v4, ok := parseIPv4(value); ok {
		return fmt.Sprintf("%d.%d.%d.%d", v4[0], v4[1], v4[2], v4[3]), true
	}
	v6, ok := parseIPv6(value)
	if !ok {
		return "", false
	}
	return formatIPv6(v6), true
}

// parseIPv4 is IPv4Address._ip_int_from_string: four dot-separated ASCII decimal octets of at
// most three digits, no leading zero, none above 255.
func parseIPv4(value string) ([ipv4Octets]byte, bool) {
	var out [ipv4Octets]byte
	octets := strings.Split(value, ".")
	if value == "" || len(octets) != ipv4Octets {
		return out, false
	}
	for i, octet := range octets {
		if octet == "" || len(octet) > 3 || !allDigits(octet) || (octet != "0" && octet[0] == '0') {
			return out, false
		}
		n, err := strconv.Atoi(octet)
		if err != nil || n > 255 {
			return out, false
		}
		out[i] = byte(n)
	}
	return out, true
}

func allDigits(s string) bool {
	for _, c := range []byte(s) {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// parseIPv6 is IPv6Address._ip_int_from_string (no scope id): the eight hextets.
func parseIPv6(value string) ([hextetCount]uint16, bool) {
	var out [hextetCount]uint16
	parts := strings.Split(value, ":")
	if value == "" || len(parts) < 3 {
		return out, false
	}
	if last := parts[len(parts)-1]; strings.Contains(last, ".") {
		v4, ok := parseIPv4(last)
		if !ok {
			return out, false
		}
		parts = append(parts[:len(parts)-1],
			strconv.FormatUint(uint64(v4[0])<<8|uint64(v4[1]), 16),
			strconv.FormatUint(uint64(v4[2])<<8|uint64(v4[3]), 16))
	}
	if len(parts) > hextetCount+1 {
		return out, false
	}
	hi, lo, ok := hextetSpans(parts)
	if !ok {
		return out, false
	}
	for i := range hi {
		if out[i], ok = parseHextet(parts[i]); !ok {
			return out, false
		}
	}
	for i := range lo {
		if out[hextetCount-lo+i], ok = parseHextet(parts[len(parts)-lo+i]); !ok {
			return out, false
		}
	}
	return out, true
}

// hextetSpans is how many parts come before a "::" and after it (all parts are "before" when
// there is none), by ipaddress's rules for the ends.
func hextetSpans(parts []string) (int, int, bool) {
	skip := -1
	for i := 1; i < len(parts)-1; i++ {
		if parts[i] == "" {
			if skip >= 0 {
				return 0, 0, false
			}
			skip = i
		}
	}
	if skip < 0 {
		ok := len(parts) == hextetCount && parts[0] != "" && parts[len(parts)-1] != ""
		return len(parts), 0, ok
	}
	hi, lo := skip, len(parts)-skip-1
	if parts[0] == "" {
		if hi--; hi != 0 {
			return 0, 0, false
		}
	}
	if parts[len(parts)-1] == "" {
		if lo--; lo != 0 {
			return 0, 0, false
		}
	}
	return hi, lo, hextetCount-(hi+lo) >= 1
}

// parseHextet is _parse_hextet: one to four hex digits (base 16 ParseUint takes no sign, prefix
// or underscore).
func parseHextet(text string) (uint16, bool) {
	if text == "" || len(text) > maxHextetLen {
		return 0, false
	}
	n, err := strconv.ParseUint(text, 16, 16)
	if err != nil {
		return 0, false
	}
	return uint16(n), true
}

// formatIPv6 is _string_from_ip_int: lowercase hextets without leading zeros, the longest run
// of two or more zero hextets (the first, on a tie) written as "::".
func formatIPv6(hextets [hextetCount]uint16) string {
	bestStart, bestLen, start := -1, 0, -1
	for i, h := range hextets {
		if h != 0 {
			start = -1
			continue
		}
		if start < 0 {
			start = i
		}
		if i-start+1 > bestLen {
			bestStart, bestLen = start, i-start+1
		}
	}
	text := make([]string, hextetCount)
	for i, h := range hextets {
		text[i] = strconv.FormatUint(uint64(h), 16)
	}
	if bestLen < 2 {
		return strings.Join(text, ":")
	}
	return strings.Join(text[:bestStart], ":") + "::" + strings.Join(text[bestStart+bestLen:], ":")
}
