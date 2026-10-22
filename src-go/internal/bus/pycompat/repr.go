package pycompat

import (
	"math"
	"strconv"
	"strings"
	"unicode"
)

// PyStr is python's str(v): a str is itself, every other value is its repr.
func PyStr(v Value) string {
	if s, ok := v.(Str); ok {
		return string(s)
	}
	return PyRepr(v)
}

// PyRepr is python's repr(v): True/False/None, an int in decimal, a float as float.__repr__
// writes it, a str quoted and escaped, and a list or dict of its items' reprs.
func PyRepr(v Value) string {
	var b strings.Builder
	writeRepr(&b, v)
	return b.String()
}

func writeRepr(b *strings.Builder, v Value) {
	switch x := v.(type) {
	case None:
		b.WriteString("None")
	case Bool:
		if x {
			b.WriteString("True")
		} else {
			b.WriteString("False")
		}
	case Int:
		b.WriteString(x.V.String())
	case Float:
		b.WriteString(floatRepr(float64(x)))
	case Str:
		b.WriteString(strRepr(string(x)))
	case List:
		b.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				b.WriteString(", ")
			}
			writeRepr(b, item)
		}
		b.WriteByte(']')
	case *Dict:
		b.WriteByte('{')
		for i, it := range x.items {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(strRepr(it.Key))
			b.WriteString(": ")
			writeRepr(b, it.Value)
		}
		b.WriteByte('}')
	}
}

// shortest is |f|'s shortest round-trip decimal digits and the exponent of the first one
// (f = 0.d1d2... x 10^(exp+1)), from strconv's d.ddde±XX form.
func shortest(f float64) (string, int) {
	sci := strconv.FormatFloat(math.Abs(f), 'e', -1, 64)
	at := strings.IndexByte(sci, 'e')
	digits := strings.Replace(sci[:at], ".", "", 1)
	exp := 0
	for _, c := range sci[at+2:] {
		exp = exp*10 + int(c-'0')
	}
	if sci[at+1] == '-' {
		exp = -exp
	}
	return digits, exp
}

// floatRepr is float.__repr__: the shortest digits that read back to f, in fixed notation
// when the decimal point position is in (-4, 16], else in exponent form with at least two
// exponent digits; an integral fixed form gets ".0".
func floatRepr(f float64) string {
	switch {
	case math.IsNaN(f):
		return "nan"
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	}
	sign := ""
	if math.Signbit(f) {
		sign = "-"
	}
	if f == 0 {
		return sign + "0.0"
	}
	digits, exp := shortest(f)
	decpt := exp + 1
	if decpt <= -4 || decpt > 16 {
		out := digits[:1]
		if len(digits) > 1 {
			out += "." + digits[1:]
		}
		expSign := "+"
		if exp < 0 {
			expSign, exp = "-", -exp
		}
		expDigits := strconv.Itoa(exp)
		if len(expDigits) < 2 {
			expDigits = "0" + expDigits
		}
		return sign + out + "e" + expSign + expDigits
	}
	switch {
	case decpt <= 0:
		return sign + "0." + strings.Repeat("0", -decpt) + digits
	case decpt >= len(digits):
		return sign + digits + strings.Repeat("0", decpt-len(digits)) + ".0"
	}
	return sign + digits[:decpt] + "." + digits[decpt:]
}

// isPrintable is python's str.isprintable for one code point: not in categories Cc, Cf, Cs,
// Co, Cn, Zl, Zp or Zs, except the space.
func isPrintable(r rune) bool {
	if r == ' ' {
		return true
	}
	if unicode.In(r, unicode.Cc, unicode.Cf, unicode.Cs, unicode.Co, unicode.Zl, unicode.Zp, unicode.Zs) {
		return false
	}
	return unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.P, unicode.S)
}

const hexDigits = "0123456789abcdef"

func writeHex(b *strings.Builder, prefix string, r rune, width int) {
	b.WriteString(prefix)
	for shift := (width - 1) * 4; shift >= 0; shift -= 4 {
		b.WriteByte(hexDigits[(r>>shift)&0xF])
	}
}

// strRepr is repr(str): single quotes unless the text has a ' and no "; the quote, the
// backslash, tab, newline and carriage return escaped; other non-printable code points as
// \xNN, \uNNNN or \UNNNNNNNN.
func strRepr(s string) string {
	quote := '\''
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}
	var b strings.Builder
	b.WriteRune(quote)
	for _, r := range CodePoints(s) {
		if r == quote || r == '\\' {
			b.WriteByte('\\')
			b.WriteRune(r)
			continue
		}
		writeReprRune(&b, r)
	}
	b.WriteRune(quote)
	return b.String()
}

// reprEscapes are the characters repr writes as a backslash and a letter.
var reprEscapes = map[rune]string{'\t': `\t`, '\n': `\n`, '\r': `\r`}

func writeReprRune(b *strings.Builder, r rune) {
	if esc, ok := reprEscapes[r]; ok {
		b.WriteString(esc)
		return
	}
	switch {
	case r < ' ' || r == 0x7F:
		writeHex(b, `\x`, r, 2)
	case r < 0x7F || isPrintable(r):
		b.WriteRune(r)
	case r <= 0xFF:
		writeHex(b, `\x`, r, 2)
	case r <= 0xFFFF:
		writeHex(b, `\u`, r, 4)
	default:
		writeHex(b, `\U`, r, 8)
	}
}
