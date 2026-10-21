// Package pyjson reads JSON as CPython's json module reads it (Plan 00213, SPEC-status-line.md
// S23), for the files pingbus's Python writes and the Go status line must read the same way.
// encoding/json differs from it: it refuses NaN and Infinity, cannot tell 1 from 1.0, accepts
// invalid UTF-8 in a string, and turns a lone surrogate escape into U+FFFD.
//
// A value is Null, a bool, a string, a Number, a []any or an Object. A string is UTF-8, except
// that a lone surrogate (Python keeps "\ud800" as one) is kept as its three-byte generalised
// UTF-8 form, so two different lone surrogates stay different strings. A duplicate key takes its
// last value, as in Python, unless Options.NoDuplicateKeys refuses it.
//
// Departures, each where Python itself does not return a value: nesting deeper than maxDepth is
// refused (Python raises RecursionError near depth 1000 and its caller crashes), and an integer
// of more than 4300 digits is refused as Python 3.11 refuses it (a ValueError, read as not JSON).
package pyjson

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// maxDepth is the deepest nesting of arrays and objects read.
	maxDepth = 10000
	// maxIntDigits is CPython's default int_max_str_digits: a longer integer is a ValueError.
	maxIntDigits = 4300
)

var (
	// ErrDuplicateKey is a key given twice in one object, under Options.NoDuplicateKeys.
	ErrDuplicateKey = errors.New("a key appears twice in one object")
	// ErrNotUTF8 is input DecodeUTF8 cannot decode (Python's UnicodeDecodeError).
	ErrNotUTF8 = errors.New("not UTF-8")
	// ErrNotUnicode is input DecodeBytes cannot decode in the encoding it detected.
	ErrNotUnicode = errors.New("not text in the encoding its bytes show")
)

// SyntaxError is text that is not JSON as Python reads it.
type SyntaxError struct {
	Offset int
	Msg    string
}

func (e *SyntaxError) Error() string { return fmt.Sprintf("%s at character %d", e.Msg, e.Offset) }

// Kind is a Number's Python type.
type Kind int

const (
	// Int is a literal Python reads as an int: no fraction and no exponent.
	Int Kind = iota + 1
	// Float is a literal Python reads as a float, NaN and Infinity included.
	Float
)

// Number is a JSON number with its literal text, so 1 and 1.0 stay apart as in Python.
type Number struct {
	Lit  string
	Kind Kind
}

// Null is JSON's null (Python's None).
type Null struct{}

// Object is a JSON object.
type Object map[string]any

// Options are the choices Python's callers make.
type Options struct {
	// NoDuplicateKeys refuses a key given twice in any object (config._no_duplicate_keys).
	NoDuplicateKeys bool
}

// Decode is json.loads of a str: text must be the decoded document.
func Decode(text string, opts Options) (any, error) {
	return decodeRunes([]rune(text), opts)
}

// DecodeUTF8 is json.loads(raw.decode("utf-8")): strict UTF-8, which refuses an encoded surrogate.
func DecodeUTF8(raw []byte, opts Options) (any, error) {
	if !utf8.Valid(raw) {
		return nil, ErrNotUTF8
	}
	return Decode(string(raw), opts)
}

// DecodeBytes is json.loads of bytes: the encoding is UTF-8, UTF-16 or UTF-32 as its byte
// pattern shows (json.detect_encoding), decoded with surrogatepass.
func DecodeBytes(raw []byte) (any, error) {
	text, err := decodeDetected(raw)
	if err != nil {
		return nil, err
	}
	return decodeRunes(text, Options{})
}

type parser struct {
	s     []rune
	i     int
	opts  Options
	depth int
}

func decodeRunes(s []rune, opts Options) (any, error) {
	p := &parser{s: s, opts: opts}
	p.space()
	v, err := p.value()
	if err != nil {
		return nil, err
	}
	p.space()
	if p.i != len(p.s) {
		return nil, p.fail("extra data")
	}
	return v, nil
}

func (p *parser) fail(msg string) error { return &SyntaxError{Offset: p.i, Msg: msg} }

func (p *parser) space() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t' || p.s[p.i] == '\n' || p.s[p.i] == '\r') {
		p.i++
	}
}

func (p *parser) at(c rune) bool { return p.i < len(p.s) && p.s[p.i] == c }

// word consumes w when the text at the cursor starts with it.
func (p *parser) word(w string) bool {
	runes := []rune(w)
	if len(p.s)-p.i < len(runes) || !slices.Equal(p.s[p.i:p.i+len(runes)], runes) {
		return false
	}
	p.i += len(runes)
	return true
}

func isDigit(c rune) bool { return c >= '0' && c <= '9' }

func (p *parser) value() (any, error) {
	if p.i >= len(p.s) {
		return nil, p.fail("expecting value")
	}
	switch c := p.s[p.i]; {
	case c == '"':
		p.i++
		return p.str()
	case c == '{':
		return p.object()
	case c == '[':
		return p.array()
	case p.word("null"):
		return Null{}, nil
	case p.word("true"):
		return true, nil
	case p.word("false"):
		return false, nil
	case p.word("NaN"):
		return Number{"NaN", Float}, nil
	case p.word("Infinity"):
		return Number{"Infinity", Float}, nil
	case p.word("-Infinity"):
		return Number{"-Infinity", Float}, nil
	case c == '-' || isDigit(c):
		return p.number()
	}
	return nil, p.fail("expecting value")
}

// number is CPython's _match_number: -?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][-+]?[0-9]+)?, a fraction
// or exponent making it a float. What follows a backtracked "." or "e" is left to the caller.
func (p *parser) number() (any, error) {
	start := p.i
	if p.at('-') {
		p.i++
	}
	if p.i >= len(p.s) || !isDigit(p.s[p.i]) {
		return nil, p.fail("expecting value")
	}
	if p.at('0') {
		p.i++
	} else {
		p.digits()
	}
	digits := p.i - start
	if p.s[start] == '-' {
		digits--
	}
	kind := Int
	fraction := p.fraction()
	if exponent := p.exponent(); fraction || exponent {
		kind = Float
	}
	if kind == Int && digits > maxIntDigits {
		return nil, p.fail("an integer of more than 4300 digits")
	}
	return Number{string(p.s[start:p.i]), kind}, nil
}

// fraction consumes "." and its digits; a "." with no digit after it is left alone.
func (p *parser) fraction() bool {
	if p.i+1 >= len(p.s) || p.s[p.i] != '.' || !isDigit(p.s[p.i+1]) {
		return false
	}
	p.i++
	p.digits()
	return true
}

// exponent consumes "e", its sign and its digits; an "e" with no digit after it is left alone.
func (p *parser) exponent() bool {
	if !p.at('e') && !p.at('E') {
		return false
	}
	mark := p.i
	p.i++
	if p.at('+') || p.at('-') {
		p.i++
	}
	if begun := p.i; p.digits() == begun {
		p.i = mark
		return false
	}
	return true
}

// digits consumes a run of digits and returns the cursor after it.
func (p *parser) digits() int {
	for p.i < len(p.s) && isDigit(p.s[p.i]) {
		p.i++
	}
	return p.i
}

// str reads a string's body after its opening quote, as CPython's strict scanstring.
func (p *parser) str() (string, error) {
	var b []byte
	for {
		if p.i >= len(p.s) {
			return "", p.fail("unterminated string")
		}
		c := p.s[p.i]
		p.i++
		switch {
		case c == '"':
			return string(b), nil
		case c < 0x20:
			return "", p.fail("invalid control character")
		case c != '\\':
			b = appendWTF8(b, c)
			continue
		}
		r, err := p.escape()
		if err != nil {
			return "", err
		}
		b = appendWTF8(b, r)
	}
}

var simpleEscapes = map[rune]rune{'"': '"', '\\': '\\', '/': '/', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t'}

// escape reads the escape after a backslash. A high surrogate escape followed by a low one is
// one code point; any other surrogate stays alone.
func (p *parser) escape() (rune, error) {
	if p.i >= len(p.s) {
		return 0, p.fail("unterminated string")
	}
	e := p.s[p.i]
	p.i++
	if r, ok := simpleEscapes[e]; ok {
		return r, nil
	}
	if e != 'u' {
		return 0, p.fail("invalid escape")
	}
	r, ok := p.hex4(p.i)
	if !ok {
		return 0, p.fail(`invalid \uXXXX escape`)
	}
	p.i += 4
	if r >= 0xd800 && r <= 0xdbff && p.i+6 <= len(p.s) && p.s[p.i] == '\\' && p.s[p.i+1] == 'u' {
		low, valid := p.hex4(p.i + 2)
		if !valid {
			return 0, p.fail(`invalid \uXXXX escape`)
		}
		if low >= 0xdc00 && low <= 0xdfff {
			p.i += 6
			return 0x10000 + (r-0xd800)<<10 + (low - 0xdc00), nil
		}
	}
	return r, nil
}

// hex4 is the four hex digits from index at, false when there are not four.
func (p *parser) hex4(at int) (rune, bool) {
	if at+4 > len(p.s) {
		return 0, false
	}
	var r rune
	for _, c := range p.s[at : at+4] {
		var d rune
		switch {
		case isDigit(c):
			d = c - '0'
		case c >= 'a' && c <= 'f':
			d = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			d = c - 'A' + 10
		default:
			return 0, false
		}
		r = r<<4 | d
	}
	return r, true
}

// continuation[i] is the UTF-8 continuation byte 0x80+i.
const continuation = "\x80\x81\x82\x83\x84\x85\x86\x87\x88\x89\x8a\x8b\x8c\x8d\x8e\x8f\x90\x91\x92\x93\x94\x95\x96\x97\x98\x99\x9a\x9b\x9c\x9d\x9e\x9f\xa0\xa1\xa2\xa3\xa4\xa5\xa6\xa7\xa8\xa9\xaa\xab\xac\xad\xae\xaf\xb0\xb1\xb2\xb3\xb4\xb5\xb6\xb7\xb8\xb9\xba\xbb\xbc\xbd\xbe\xbf"

// appendWTF8 is UTF-8, with a surrogate written in the three-byte form UTF-8 forbids: every
// surrogate (U+D800 to U+DFFF) starts with 0xED.
func appendWTF8(b []byte, r rune) []byte {
	if r >= 0xd800 && r <= 0xdfff {
		return append(b, 0xed, continuation[(r>>6)&0x3f], continuation[r&0x3f])
	}
	return utf8.AppendRune(b, r)
}

func (p *parser) enter() error {
	p.depth++
	if p.depth > maxDepth {
		return p.fail("nested too deeply")
	}
	return nil
}

func (p *parser) object() (any, error) {
	p.i++
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer func() { p.depth-- }()
	obj := Object{}
	p.space()
	if p.at('}') {
		p.i++
		return obj, nil
	}
	for {
		if !p.at('"') {
			return nil, p.fail("expecting property name enclosed in double quotes")
		}
		p.i++
		key, err := p.str()
		if err != nil {
			return nil, err
		}
		p.space()
		if !p.at(':') {
			return nil, p.fail("expecting ':' delimiter")
		}
		p.i++
		p.space()
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		if _, dup := obj[key]; dup && p.opts.NoDuplicateKeys {
			return nil, ErrDuplicateKey
		}
		obj[key] = v
		p.space()
		if p.at('}') {
			p.i++
			return obj, nil
		}
		if !p.at(',') {
			return nil, p.fail("expecting ',' delimiter")
		}
		p.i++
		p.space()
	}
}

func (p *parser) array() (any, error) {
	p.i++
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer func() { p.depth-- }()
	list := []any{}
	p.space()
	if p.at(']') {
		p.i++
		return list, nil
	}
	for {
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		list = append(list, v)
		p.space()
		if p.at(']') {
			p.i++
			return list, nil
		}
		if !p.at(',') {
			return nil, p.fail("expecting ',' delimiter")
		}
		p.i++
		p.space()
	}
}

// IsInt is Python's `type(v) is int and v == want`.
func (n Number) IsInt(want int64) bool {
	v, ok := n.bigInt()
	return ok && v.Cmp(big.NewInt(want)) == 0
}

// Int64 is the value of an integer literal that fits in an int64.
func (n Number) Int64() (int64, bool) {
	v, ok := n.bigInt()
	if !ok || !v.IsInt64() {
		return 0, false
	}
	return v.Int64(), true
}

func (n Number) bigInt() (*big.Int, bool) {
	if n.Kind != Int {
		return nil, false
	}
	return new(big.Int).SetString(n.Lit, 10)
}

// float is the literal as Python's float(): an overflow is an infinity, not an error.
func (n Number) float() float64 {
	f, err := strconv.ParseFloat(n.Lit, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return math.NaN()
	}
	return f
}

// EqualsInt is Python's `v == want` for a decoded value: an int or float of that value, or a
// bool (True is 1, False is 0).
func EqualsInt(v any, want int64) bool {
	switch x := v.(type) {
	case Number:
		if x.Kind == Int {
			return x.IsInt(want)
		}
		return x.float() == float64(want)
	case bool:
		return (x && want == 1) || (!x && want == 0)
	}
	return false
}

// Canonical is a text that two values share exactly when Python's json.dumps(v, sort_keys=True)
// gives them the same text: the identity `_list_of` compares list entries by.
func Canonical(v any) string {
	switch x := v.(type) {
	case Null:
		return "null"
	case bool:
		return strconv.FormatBool(x)
	case string:
		return strconv.Quote(x)
	case Number:
		return x.canonical()
	case []any:
		parts := make([]string, len(x))
		for i, item := range x {
			parts[i] = Canonical(item)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case Object:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = strconv.Quote(k) + ":" + Canonical(x[k])
		}
		return "{" + strings.Join(parts, ",") + "}"
	}
	return fmt.Sprintf("<%T>", v)
}

func (n Number) canonical() string {
	if v, ok := n.bigInt(); ok {
		return "i" + v.String()
	}
	f := n.float()
	if math.IsNaN(f) {
		return "fNaN"
	}
	return "f" + strconv.FormatFloat(f, 'g', -1, 64)
}

// decodeDetected is bytes.decode(json.detect_encoding(raw), "surrogatepass") as code points.
func decodeDetected(raw []byte) ([]rune, error) {
	for _, bom := range boms {
		if strings.HasPrefix(string(raw), bom.mark) {
			return bom.encoding.decode(raw[len(bom.mark):])
		}
	}
	return patternEncoding(raw).decode(raw)
}

// encoding is a codec detect_encoding names.
type encoding int

const (
	utf8Encoding encoding = iota
	utf16BE
	utf16LE
	utf32BE
	utf32LE
)

func (e encoding) decode(raw []byte) ([]rune, error) {
	switch e {
	case utf16BE, utf16LE:
		return decodeUTF16(raw, e == utf16BE)
	case utf32BE, utf32LE:
		return decodeUTF32(raw, e == utf32BE)
	case utf8Encoding:
	}
	return decodeWTF8(raw)
}

// boms are the byte order marks detect_encoding looks for, in its order; the codec it names
// drops the mark.
var boms = []struct {
	mark     string
	encoding encoding
}{
	{"\xff\xfe\x00\x00", utf32LE},
	{"\x00\x00\xfe\xff", utf32BE},
	{"\xff\xfe", utf16LE},
	{"\xfe\xff", utf16BE},
	{"\xef\xbb\xbf", utf8Encoding},
}

// patternEncoding is detect_encoding's guess from the zero bytes of a text with no mark.
func patternEncoding(raw []byte) encoding {
	switch {
	case len(raw) >= 4:
		return patternOfFour(raw)
	case len(raw) == 2 && raw[0] == 0:
		return utf16BE
	case len(raw) == 2 && raw[1] == 0:
		return utf16LE
	}
	return utf8Encoding
}

// patternOfFour is detect_encoding's guess for a text of four bytes or more.
func patternOfFour(raw []byte) encoding {
	switch {
	case raw[0] == 0 && raw[1] != 0:
		return utf16BE
	case raw[0] == 0:
		return utf32BE
	case raw[1] == 0 && (raw[2] != 0 || raw[3] != 0):
		return utf16LE
	case raw[1] == 0:
		return utf32LE
	}
	return utf8Encoding
}

// decodeWTF8 is UTF-8 with surrogatepass: an encoded surrogate is its code point.
func decodeWTF8(raw []byte) ([]rune, error) {
	out := make([]rune, 0, len(raw))
	for len(raw) > 0 {
		r, size := utf8.DecodeRune(raw)
		if r == utf8.RuneError && size == 1 {
			if len(raw) < 3 || raw[0] != 0xed || raw[1] < 0xa0 || raw[1] > 0xbf || raw[2] < 0x80 || raw[2] > 0xbf {
				return nil, ErrNotUnicode
			}
			r, size = rune(raw[0]&0x0f)<<12|rune(raw[1]&0x3f)<<6|rune(raw[2]&0x3f), 3
		}
		out = append(out, r)
		raw = raw[size:]
	}
	return out, nil
}

// decodeUTF16 is UTF-16 with surrogatepass: a pair is one code point, a lone unit its own.
func decodeUTF16(raw []byte, bigEndian bool) ([]rune, error) {
	if len(raw)%2 != 0 {
		return nil, ErrNotUnicode
	}
	units := make([]rune, len(raw)/2)
	for i := range units {
		hi, lo := raw[2*i], raw[2*i+1]
		if !bigEndian {
			hi, lo = lo, hi
		}
		units[i] = rune(hi)<<8 | rune(lo)
	}
	out := make([]rune, 0, len(units))
	for i := 0; i < len(units); i++ {
		u := units[i]
		if u >= 0xd800 && u <= 0xdbff && i+1 < len(units) && units[i+1] >= 0xdc00 && units[i+1] <= 0xdfff {
			u = 0x10000 + (u-0xd800)<<10 + (units[i+1] - 0xdc00)
			i++
		}
		out = append(out, u)
	}
	return out, nil
}

// decodeUTF32 is UTF-32 with surrogatepass: a unit past U+10FFFF is an error.
func decodeUTF32(raw []byte, bigEndian bool) ([]rune, error) {
	if len(raw)%4 != 0 {
		return nil, ErrNotUnicode
	}
	out := make([]rune, 0, len(raw)/4)
	for i := 0; i < len(raw); i += 4 {
		b := raw[i : i+4]
		if !bigEndian {
			b = []byte{b[3], b[2], b[1], b[0]}
		}
		u := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
		if u > utf8.MaxRune {
			return nil, ErrNotUnicode
		}
		out = append(out, rune(u))
	}
	return out, nil
}
