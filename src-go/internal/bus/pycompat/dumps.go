package pycompat

import (
	"math"
	"strings"
)

// Dumps is json.dumps(v, ensure_ascii=ensureASCII) with the default separators (", " and
// ": "): floats as float.__repr__ (NaN, Infinity, -Infinity for the non-finite), dicts in
// insertion order. With ensureASCII every code point outside printable ASCII is a \uXXXX
// escape (lowercase hex, a surrogate pair beyond the BMP); without it only the quote, the
// backslash and the control characters are escaped. A lone surrogate left unescaped is the
// UnicodeEncodeError that printing it raises in v1, so Dumps refuses it.
func Dumps(v Value, ensureASCII bool) (string, error) {
	var b strings.Builder
	writeJSON(&b, v, ensureASCII)
	out := b.String()
	if !ensureASCII {
		if err := checkEncodable(out); err != nil {
			return "", err
		}
	}
	return out, nil
}

func writeJSON(b *strings.Builder, v Value, ascii bool) {
	switch x := v.(type) {
	case None:
		b.WriteString("null")
	case Bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case Int:
		b.WriteString(x.V.String())
	case Float:
		b.WriteString(jsonFloat(float64(x)))
	case Str:
		writeJSONString(b, string(x), ascii)
	case List:
		b.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				b.WriteString(", ")
			}
			writeJSON(b, item, ascii)
		}
		b.WriteByte(']')
	case *Dict:
		b.WriteByte('{')
		for i, it := range x.items {
			if i > 0 {
				b.WriteString(", ")
			}
			writeJSONString(b, it.Key, ascii)
			b.WriteString(": ")
			writeJSON(b, it.Value, ascii)
		}
		b.WriteByte('}')
	}
}

// jsonFloat is float.__repr__ with json's names for the non-finite values.
func jsonFloat(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	return floatRepr(f)
}

var jsonEscapes = map[rune]string{'"': `\"`, '\\': `\\`, '\b': `\b`, '\f': `\f`, '\n': `\n`, '\r': `\r`, '\t': `\t`}

func writeJSONString(b *strings.Builder, s string, ascii bool) {
	b.WriteByte('"')
	for _, r := range CodePoints(s) {
		if esc, ok := jsonEscapes[r]; ok {
			b.WriteString(esc)
			continue
		}
		switch {
		case r < ' ' || (ascii && r > '~'):
			if r >= 0x10000 {
				r -= 0x10000
				writeHex(b, `\u`, 0xD800+(r>>10), 4)
				r = 0xDC00 + r&0x3FF
			}
			writeHex(b, `\u`, r, 4)
		default:
			b.Write(appendRune(nil, r))
		}
	}
	b.WriteByte('"')
}

// checkEncodable is str.encode("utf-8") of out: a run of surrogates is the UnicodeEncodeError
// python raises, with its position in code points.
func checkEncodable(out string) error {
	rs := CodePoints(out)
	for i, r := range rs {
		if r < 0xD800 || r > 0xDFFF {
			continue
		}
		end := i + 1
		for end < len(rs) && rs[end] >= 0xD800 && rs[end] <= 0xDFFF {
			end++
		}
		var msg strings.Builder
		msg.WriteString("'utf-8' codec can't encode ")
		if end == i+1 {
			msg.WriteString("character '")
			writeHex(&msg, `\u`, r, 4)
			msg.WriteString("' in position " + itoa(i))
		} else {
			msg.WriteString("characters in position " + itoa(i) + "-" + itoa(end-1))
		}
		msg.WriteString(": surrogates not allowed")
		return &Error{Class: ClassUnicodeEncodeError, Msg: msg.String(), Pos: -1}
	}
	return nil
}
