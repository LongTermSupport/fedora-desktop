package proto

import (
	"errors"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// ErrUnencodable is a value holding a lone surrogate, which has no UTF-8 form (SPEC P3, D1).
var ErrUnencodable = errors.New("holds a lone surrogate, which has no UTF-8 form")

// points is a string's code points as Python's str holds them, a lone surrogate as one.
func points(s string) []rune {
	rs, err := pyjson.CodePoints(s)
	if err != nil {
		return []rune(s) // not generalised UTF-8: each stray byte is one U+FFFD, as Go reads it
	}
	return rs
}

// charLen is Python's len(str).
func charLen(s string) int {
	if utf8.ValidString(s) {
		return utf8.RuneCountInString(s)
	}
	return len(points(s))
}

// utf8Size is len(s.encode("utf-8")); false where Python raises (a lone surrogate).
func utf8Size(s string) (int, bool) {
	return len(s), utf8.ValidString(s)
}

// pySpace is str.isspace: bidirectional class WS, B or S, or category Zs.
func pySpace(r rune) bool {
	switch {
	case r >= '\t' && r <= '\r', r >= 0x1c && r <= ' ', r == 0x85, r == 0xa0, r == 0x1680:
		return true
	case r >= 0x2000 && r <= 0x200a, r == 0x2028, r == 0x2029, r == 0x202f, r == 0x205f, r == 0x3000:
		return true
	}
	return false
}

// pyStrip is str.strip().
func pyStrip(s string) string { return strings.TrimFunc(s, pySpace) }

// pyLower is str.lower(): the full lowercase, so U+0130 is "i" and a combining dot, and a
// capital sigma at the end of a word is the final form (SPEC P14, D6).
func pyLower(s string) string {
	rs := points(s)
	out := make([]rune, 0, len(rs))
	for i, r := range rs {
		switch {
		case r == 0x130:
			out = append(out, 'i', 0x307)
		case r == 0x3a3:
			out = append(out, sigmaAt(rs, i))
		case r >= 0xd800 && r <= 0xdfff:
			out = append(out, r)
		default:
			out = append(out, unicode.ToLower(r))
		}
	}
	return pyjson.FromCodePoints(out)
}

// sigmaAt is CPython's handle_capital_sigma: final after a cased letter and before none,
// case-ignorable code points skipped both ways.
func sigmaAt(rs []rune, i int) rune {
	j := i - 1
	for j >= 0 && caseIgnorable(rs[j]) {
		j--
	}
	before := j >= 0 && cased(rs[j])
	k := i + 1
	for k < len(rs) && caseIgnorable(rs[k]) {
		k++
	}
	after := k < len(rs) && cased(rs[k])
	if before && !after {
		return 0x3c2
	}
	return 0x3c3
}

func cased(r rune) bool {
	return unicode.IsUpper(r) || unicode.IsLower(r) || unicode.IsTitle(r) ||
		unicode.Is(unicode.Other_Lowercase, r) || unicode.Is(unicode.Other_Uppercase, r)
}

// midLetter holds the Word_Break MidLetter, MidNumLet and Single_Quote code points, which are
// case-ignorable whatever their category.
var midLetter = []rune{
	'\'', '.', ':', 0xb7, 0x387, 0x55f, 0x5f4, 0x2018, 0x2019, 0x2024, 0x2027, 0xfe13, 0xfe52,
	0xfe55, 0xff07, 0xff0e, 0xff1a,
}

func caseIgnorable(r rune) bool {
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf, unicode.Lm, unicode.Sk) ||
		slices.Contains(midLetter, r)
}

// dumps is Python's json.dumps(v, separators=(",", ":"), ensure_ascii=ascii) of a decoded value.
// An Object's keys are written sorted: callers use the text for its size or as a key, never
// for its order.
func dumps(v any, ascii bool) string {
	var b strings.Builder
	writeJSON(&b, v, ascii)
	return b.String()
}

func writeJSON(b *strings.Builder, v any, ascii bool) {
	switch x := v.(type) {
	case pyjson.Null, nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case string:
		writeString(b, x, ascii)
	case pyjson.Number:
		b.WriteString(numberText(x))
	case []any:
		b.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSON(b, item, ascii)
		}
		b.WriteByte(']')
	case []string:
		items := make([]any, len(x))
		for i, s := range x {
			items[i] = s
		}
		writeJSON(b, items, ascii)
	case pyjson.Object:
		writeObject(b, x, ascii)
	}
}

func writeObject(b *strings.Builder, o pyjson.Object, ascii bool) {
	keys := make([]string, 0, len(o))
	for k := range o {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		writeString(b, k, ascii)
		b.WriteByte(':')
		writeJSON(b, o[k], ascii)
	}
	b.WriteByte('}')
}

const hexDigits = "0123456789abcdef"

func writeU(b *strings.Builder, u rune) {
	b.WriteString(`\u`)
	for shift := 12; shift >= 0; shift -= 4 {
		b.WriteByte(hexDigits[(u>>shift)&0xf])
	}
}

var shortEscapes = map[rune]string{
	'"': `\"`, '\\': `\\`, '\n': `\n`, '\r': `\r`, '\t': `\t`, '\b': `\b`, '\f': `\f`,
}

// writeString is json's encoder for a str: ensure_ascii escapes everything outside space to
// "~", in lowercase hex, an astral code point as its surrogate pair.
func writeString(b *strings.Builder, s string, ascii bool) {
	b.WriteByte('"')
	for _, r := range points(s) {
		if esc, ok := shortEscapes[r]; ok {
			b.WriteString(esc)
			continue
		}
		switch {
		case r > 0xffff && ascii:
			hi, lo := utf16Pair(r)
			writeU(b, hi)
			writeU(b, lo)
		case r < 0x20 || (ascii && r > '~'):
			writeU(b, r)
		default:
			b.WriteString(pyjson.FromCodePoints([]rune{r}))
		}
	}
	b.WriteByte('"')
}

func utf16Pair(r rune) (rune, rune) {
	r -= 0x10000
	return 0xd800 + (r >> 10), 0xdc00 + (r & 0x3ff)
}

// numberText is json.dumps of the Python int or float the literal decodes to.
func numberText(n pyjson.Number) string {
	if n.Kind == pyjson.Int {
		if v, ok := new(big.Int).SetString(n.Lit, 10); ok {
			return v.String()
		}
	}
	return floatRepr(n.Lit)
}

// floatRepr is json's float text: repr, with NaN, Infinity and -Infinity for the rest.
func floatRepr(lit string) string {
	f, err := strconv.ParseFloat(lit, 64)
	switch {
	case err != nil && !errors.Is(err, strconv.ErrRange), math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	sci := strconv.FormatFloat(f, 'e', -1, 64)
	at := strings.IndexByte(sci, 'e')
	mant := sci[:at]
	exp, err := strconv.Atoi(sci[at+1:])
	if err != nil {
		return sci
	}
	sign := ""
	if strings.HasPrefix(mant, "-") {
		sign, mant = "-", mant[1:]
	}
	digits := strings.Replace(mant, ".", "", 1)
	if exp >= -4 && exp < 16 {
		return sign + fixed(digits, exp)
	}
	out := digits[:1]
	if len(digits) > 1 {
		out += "." + digits[1:]
	}
	expSign := "+"
	if exp < 0 {
		expSign, exp = "-", -exp
	}
	return sign + out + "e" + expSign + twoDigits(exp)
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// fixed places the decimal point exp+1 digits in, with at least one digit each side.
func fixed(digits string, exp int) string {
	if exp < 0 {
		return "0." + strings.Repeat("0", -exp-1) + digits
	}
	point := exp + 1
	if len(digits) <= point {
		return digits + strings.Repeat("0", point-len(digits)) + ".0"
	}
	return digits[:point] + "." + digits[point:]
}

// ContentSize is SPEC P3: the UTF-8 byte length of the compact JSON of a decoded value, with
// ErrUnencodable for a lone surrogate anywhere in it.
func ContentSize(v any) (int, error) {
	n, ok := utf8Size(dumps(v, false))
	if !ok {
		return 0, ErrUnencodable
	}
	return n, nil
}
