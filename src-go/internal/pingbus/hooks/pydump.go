package hooks

// Future home: bus/state (the three Python JSON writer forms) and bus/proto (content_size).
// pyjson reads JSON as Python does; this writes it as Python's json.dumps does (H6, H88).

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// errNotEncodable is text holding a lone surrogate, which Python's str.encode("utf-8") refuses.
var errNotEncodable = errors.New("a lone surrogate cannot be encoded as UTF-8")

// pyobj is a JSON object in insertion order, as a Python dict literal writes it.
type pyobj []pykv

// pykv is one key of a pyobj.
type pykv struct {
	key   string
	value any
}

// dumpStyle is json.dumps's arguments: ensure_ascii, the separators and sort_keys.
type dumpStyle struct {
	ascii           bool
	itemSep, keySep string
	sortKeys        bool
}

var (
	// styleDefault is json.dumps(v, ensure_ascii=True).
	styleDefault = dumpStyle{ascii: true, itemSep: ", ", keySep: ": "}
	// styleCompact is write_json_file's json.dumps(v, ensure_ascii=True, separators=(",", ":")).
	styleCompact = dumpStyle{ascii: true, itemSep: ",", keySep: ":"}
	// styleSorted is the membership writers' json.dumps(v, ensure_ascii=True, sort_keys=True).
	styleSorted = dumpStyle{ascii: true, itemSep: ", ", keySep: ": ", sortKeys: true}
	// styleUnicode is content_size's json.dumps(v, ensure_ascii=False, separators=(",", ":")),
	// whose text is then encoded as UTF-8.
	styleUnicode = dumpStyle{itemSep: ",", keySep: ":"}
)

// pyDump is json.dumps(value) in style. A string holding a lone surrogate is errNotEncodable
// without ensure_ascii (the UTF-8 encode that follows refuses it).
func pyDump(value any, style dumpStyle) (string, error) {
	var b strings.Builder
	if err := style.write(&b, value); err != nil {
		return "", err
	}
	return b.String(), nil
}

func (s dumpStyle) write(b *strings.Builder, value any) error {
	switch v := value.(type) {
	case nil, pyjson.Null:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(v))
	case int:
		b.WriteString(strconv.Itoa(v))
	case int64:
		b.WriteString(strconv.FormatInt(v, 10))
	case pyjson.Number:
		b.WriteString(numberRepr(v))
	case string:
		return s.writeString(b, v)
	case []string:
		return s.writeList(b, len(v), func(i int) any { return v[i] })
	case []any:
		return s.writeList(b, len(v), func(i int) any { return v[i] })
	case pyobj:
		return s.writeObject(b, v)
	case pyjson.Object:
		object := make(pyobj, 0, len(v))
		for key, item := range v {
			object = append(object, pykv{key, item})
		}
		slices.SortFunc(object, func(x, y pykv) int { return strings.Compare(x.key, y.key) })
		return s.writeObject(b, object)
	default:
		return fmt.Errorf("json.dumps cannot write a %T", value)
	}
	return nil
}

func (s dumpStyle) writeList(b *strings.Builder, n int, item func(int) any) error {
	b.WriteByte('[')
	for i := range n {
		if i > 0 {
			b.WriteString(s.itemSep)
		}
		if err := s.write(b, item(i)); err != nil {
			return err
		}
	}
	b.WriteByte(']')
	return nil
}

func (s dumpStyle) writeObject(b *strings.Builder, object pyobj) error {
	if s.sortKeys {
		object = slices.Clone(object)
		slices.SortStableFunc(object, func(x, y pykv) int { return strings.Compare(x.key, y.key) })
	}
	b.WriteByte('{')
	for i, kv := range object {
		if i > 0 {
			b.WriteString(s.itemSep)
		}
		if err := s.writeString(b, kv.key); err != nil {
			return err
		}
		b.WriteString(s.keySep)
		if err := s.write(b, kv.value); err != nil {
			return err
		}
	}
	b.WriteByte('}')
	return nil
}

// writeString is Python's encoder: a lone surrogate is held as its three-byte WTF-8 form
// (pyjson's), which ensure_ascii writes as \udXXX.
func (s dumpStyle) writeString(b *strings.Builder, text string) error {
	b.WriteByte('"')
	for i := 0; i < len(text); {
		r, size := decodeWTF8(text[i:])
		i += size
		if (r == utf8.RuneError && size == 1) || (isSurrogate(r) && !s.ascii) {
			return errNotEncodable
		}
		writeEscaped(b, r, s.ascii)
	}
	b.WriteByte('"')
	return nil
}

func writeEscaped(b *strings.Builder, r rune, ascii bool) {
	switch r {
	case '"':
		b.WriteString(`\"`)
	case '\\':
		b.WriteString(`\\`)
	case '\n':
		b.WriteString(`\n`)
	case '\r':
		b.WriteString(`\r`)
	case '\t':
		b.WriteString(`\t`)
	case '\b':
		b.WriteString(`\b`)
	case '\f':
		b.WriteString(`\f`)
	default:
		switch {
		case r < 0x20 || (ascii && r == 0x7f):
			fmt.Fprintf(b, `\u%04x`, r)
		case !ascii || r < 0x80:
			b.WriteRune(r)
		case r > 0xffff:
			hi, lo := utf16Pair(r)
			fmt.Fprintf(b, `\u%04x\u%04x`, hi, lo)
		default:
			fmt.Fprintf(b, `\u%04x`, r)
		}
	}
}

func utf16Pair(r rune) (rune, rune) {
	r -= 0x10000
	return 0xd800 + (r>>10)&0x3ff, 0xdc00 + r&0x3ff
}

func isSurrogate(r rune) bool { return r >= 0xd800 && r <= 0xdfff }

// decodeWTF8 is the first code point of text, a three-byte surrogate included; an invalid
// byte is (RuneError, 1).
func decodeWTF8(text string) (rune, int) {
	if len(text) >= 3 && text[0] == 0xed && text[1] >= 0xa0 && text[1] <= 0xbf && text[2] >= 0x80 && text[2] <= 0xbf {
		return 0xd000 | rune(text[1]&0x3f)<<6 | rune(text[2]&0x3f), 3
	}
	return utf8.DecodeRuneInString(text)
}

// pyLen is Python's len of a decoded string: its code points, a lone surrogate counting one.
func pyLen(text string) int {
	n := 0
	for i := 0; i < len(text); n++ {
		_, size := decodeWTF8(text[i:])
		i += size
	}
	return n
}

// hasSurrogate is whether text holds a lone surrogate (its UTF-8 encode would fail).
func hasSurrogate(text string) bool {
	for i := 0; i < len(text); {
		r, size := decodeWTF8(text[i:])
		if isSurrogate(r) {
			return true
		}
		i += size
	}
	return false
}

// numberRepr is how json.dumps writes the int or float Python read from the literal.
func numberRepr(n pyjson.Number) string {
	if n.Kind == pyjson.Int {
		if v, ok := new(big.Int).SetString(n.Lit, 10); ok {
			return v.String()
		}
	}
	f, err := strconv.ParseFloat(n.Lit, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return "NaN"
	}
	return pyFloatRepr(f)
}

// pyFloatRepr is float.__repr__ as json.dumps writes it: the shortest round-tripping digits,
// fixed notation when the decimal point falls from -3 to 16 places, else an exponent of at
// least two digits.
func pyFloatRepr(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	text := strconv.FormatFloat(f, 'e', -1, 64)
	sign := ""
	if rest, negative := strings.CutPrefix(text, "-"); negative {
		sign, text = "-", rest
	}
	mantissa, expText, found := strings.Cut(text, "e")
	exp, err := strconv.Atoi(expText)
	if !found || err != nil {
		return "NaN"
	}
	digits := strings.Replace(mantissa, ".", "", 1)
	point := exp + 1
	switch {
	case point > -4 && point <= 0:
		return sign + "0." + strings.Repeat("0", -point) + digits
	case point > 0 && point <= 16 && point >= len(digits):
		return sign + digits + strings.Repeat("0", point-len(digits)) + ".0"
	case point > 0 && point <= 16:
		return sign + digits[:point] + "." + digits[point:]
	}
	out := sign + digits[:1]
	if len(digits) > 1 {
		out += "." + digits[1:]
	}
	expSign := "+"
	if exp < 0 {
		expSign, exp = "-", -exp
	}
	return fmt.Sprintf("%se%s%02d", out, expSign, exp)
}
