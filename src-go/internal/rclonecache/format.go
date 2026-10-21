package rclonecache

import (
	"fmt"
	"strconv"
)

// IEC is n bytes as `numfmt --to=iec --suffix=B` writes them (E15): below 1024 the number, else
// the largest power of 1024 that leaves at least 1, rounded up, one decimal below 10.
func IEC(n int64) string {
	if n < 1024 {
		return strconv.FormatInt(n, 10) + "B"
	}
	const units = "KMGTPE"
	u := uint64(n)
	base, p := uint64(1024), 0
	for p < len(units)-1 && u/base >= 1024 {
		base *= 1024
		p++
	}
	whole, rest := u/base, u%base
	if whole < 10 {
		// Tenths, rounded up: whole*10 plus the remainder's tenths. rest < base <= 2^60, so
		// rest*10 fits in a uint64.
		tenths := whole*10 + ceilDiv(rest*10, base)
		if tenths < 100 {
			return fmt.Sprintf("%d.%d%cB", tenths/10, tenths%10, units[p])
		}
		return fmt.Sprintf("10%cB", units[p])
	}
	value := whole + ceilDiv(rest, base)
	if value >= 1024 && p < len(units)-1 {
		return fmt.Sprintf("1.0%cB", units[p+1])
	}
	return fmt.Sprintf("%d%cB", value, units[p])
}

func ceilDiv(a, b uint64) uint64 {
	q := a / b
	if a%b != 0 {
		q++
	}
	return q
}

// Age is a cache max-age in seconds, coarsely (E14).
func Age(s int64) string {
	switch {
	case s <= 0:
		return "off"
	case s >= 86400:
		return fmt.Sprintf("%dd", s/86400)
	case s >= 3600:
		return fmt.Sprintf("%dh", s/3600)
	default:
		return fmt.Sprintf("%ds", s)
	}
}
