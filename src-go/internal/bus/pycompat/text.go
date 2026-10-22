package pycompat

import (
	"math/big"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxIntDigits is CPython's default int_max_str_digits: int() of a longer decimal string
// raises ValueError.
const maxIntDigits = 4300

// decodeRune is utf8.DecodeRuneInString that also reads a WTF-8 surrogate (ED A0..BF xx) as its
// code point, the form Loads keeps a lone surrogate in.
func decodeRune(s string) (rune, int) {
	r, n := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError && n == 1 && len(s) >= 3 && s[0] == 0xED && s[1] >= 0xA0 && s[1] <= 0xBF &&
		s[2] >= 0x80 && s[2] <= 0xBF {
		return 0xD000 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), 3
	}
	return r, n
}

// appendRune is utf8.AppendRune that writes a surrogate as its WTF-8 bytes instead of U+FFFD.
// A surrogate plus 0x1000 is a valid code point (U+E800..U+EFFF) with the same continuation
// bytes, encoded EE xx xx; its lead byte ED is the surrogate's.
func appendRune(b []byte, r rune) []byte {
	if r >= 0xD800 && r <= 0xDFFF {
		b = utf8.AppendRune(b, r+0x1000)
		b[len(b)-3] = 0xED
		return b
	}
	return utf8.AppendRune(b, r)
}

// CodePoints is a string's code points as python counts and indexes them (len, slicing,
// iteration), a WTF-8 lone surrogate being one.
func CodePoints(s string) []rune {
	out := make([]rune, 0, len(s))
	for len(s) > 0 {
		r, n := decodeRune(s)
		out = append(out, r)
		s = s[n:]
	}
	return out
}

// Len is python's len() of a string: its code points.
func Len(s string) int {
	n := 0
	for len(s) > 0 {
		_, size := decodeRune(s)
		s = s[size:]
		n++
	}
	return n
}

// IsSpace is python's str.isspace for one code point: bidirectional class WS, B or S, or
// category Zs. It differs from unicode.IsSpace at U+001C..U+001F.
func IsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', 0x1C, 0x1D, 0x1E, 0x1F, ' ', 0x85, 0xA0, 0x1680,
		0x2028, 0x2029, 0x202F, 0x205F, 0x3000:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// LStrip is python's str.lstrip(): leading python whitespace removed.
func LStrip(s string) string {
	for len(s) > 0 {
		r, n := decodeRune(s)
		if !IsSpace(r) {
			break
		}
		s = s[n:]
	}
	return s
}

// RStrip is python's str.rstrip(): trailing python whitespace removed.
func RStrip(s string) string {
	end := 0
	for i := 0; i < len(s); {
		r, n := decodeRune(s[i:])
		i += n
		if !IsSpace(r) {
			end = i
		}
	}
	return s[:end]
}

// Strip is python's str.strip().
func Strip(s string) string { return RStrip(LStrip(s)) }

// isLineBreak is a code point str.splitlines splits at.
func isLineBreak(r rune) bool {
	switch r {
	case '\n', '\r', '\v', '\f', 0x1C, 0x1D, 0x1E, 0x85, 0x2028, 0x2029:
		return true
	}
	return false
}

// SplitLines is python's str.splitlines(keepends): split at \n, \r, \r\n, \v, \f, \x1c, \x1d,
// \x1e, \x85, U+2028 and U+2029, with no empty last line for a trailing break.
func SplitLines(s string, keepends bool) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); {
		r, n := decodeRune(s[i:])
		if !isLineBreak(r) {
			i += n
			continue
		}
		eol := i + n
		if r == '\r' && eol < len(s) && s[eol] == '\n' {
			eol++
		}
		if keepends {
			out = append(out, s[start:eol])
		} else {
			out = append(out, s[start:i])
		}
		start, i = eol, eol
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// sigma is a code point's part in the Final_Sigma rule of str.lower.
type sigma int

const (
	sigmaNeither sigma = iota
	sigmaCased
	sigmaIgnorable
)

// wordBreakMid is Word_Break MidLetter, MidNumLet and Single_Quote, the part of
// Case_Ignorable that is not a general category.
func wordBreakMid(r rune) bool {
	switch r {
	case 0x27, 0x2E, 0x3A, 0xB7, 0x387, 0x55F, 0x5F4, 0x2018, 0x2019, 0x2024, 0x2027,
		0xFE13, 0xFE52, 0xFE55, 0xFF07, 0xFF0E, 0xFF1A:
		return true
	}
	return false
}

// sigmaClass is how python's final-sigma scan treats a code point: a Case_Ignorable one is
// skipped (checked first, so one both cased and ignorable is ignorable), a Cased one decides.
func sigmaClass(r rune) sigma {
	if unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf, unicode.Lm, unicode.Sk) || wordBreakMid(r) {
		return sigmaIgnorable
	}
	if unicode.In(r, unicode.Lu, unicode.Ll, unicode.Lt, unicode.Other_Lowercase, unicode.Other_Uppercase) {
		return sigmaCased
	}
	return sigmaNeither
}

// finalSigma is whether the capital sigma at rs[i] lowers to the final form: a cased letter
// before it and none after it, case-ignorable code points skipped on both sides.
func finalSigma(rs []rune, i int) bool {
	before := sigmaNeither
	for j := i - 1; j >= 0; j-- {
		if before = sigmaClass(rs[j]); before != sigmaIgnorable {
			break
		}
	}
	if before != sigmaCased {
		return false
	}
	for j := i + 1; j < len(rs); j++ {
		if c := sigmaClass(rs[j]); c != sigmaIgnorable {
			return c != sigmaCased
		}
	}
	return true
}

// Lower is python's str.lower(): the full lower-case mapping, so U+0130 becomes "i" and
// U+0307, and a capital sigma at the end of a word becomes the final sigma.
func Lower(s string) string {
	rs := CodePoints(s)
	b := make([]byte, 0, len(s))
	for i, r := range rs {
		switch {
		case r == 0x130:
			b = append(b, 'i', 0xCC, 0x87)
		case r == 0x3A3 && finalSigma(rs, i):
			b = appendRune(b, 0x3C2)
		default:
			b = appendRune(b, unicode.ToLower(r))
		}
	}
	return string(b)
}

// IsDigitNd is whether a code point is a decimal digit (category Nd), the digits python's
// int() converts. str.isdigit is wider (superscripts, circled digits), and int() refuses those.
func IsDigitNd(r rune) bool { return unicode.Is(unicode.Nd, r) }

// DigitValue is a decimal digit's value. Unicode encodes every Nd digit in a run from 0 to 9,
// so the value is the distance from the start of its run, modulo 10.
func DigitValue(r rune) (int, bool) {
	if !IsDigitNd(r) {
		return 0, false
	}
	start := r
	for start > 0 && IsDigitNd(start-1) {
		start--
	}
	return int((r - start) % 10), true
}

// ParseDigits is python's int(s) for a string of decimal digits only (any script, mixed): its
// value, or false when s is empty, has another code point, or exceeds the 4300-digit limit
// (python's ValueError).
func ParseDigits(s string) (*big.Int, bool) {
	rs := CodePoints(s)
	if len(rs) == 0 || len(rs) > maxIntDigits {
		return nil, false
	}
	var ascii strings.Builder
	for _, r := range rs {
		d, ok := DigitValue(r)
		if !ok {
			return nil, false
		}
		ascii.WriteByte("0123456789"[d])
	}
	return new(big.Int).SetString(ascii.String(), 10)
}
