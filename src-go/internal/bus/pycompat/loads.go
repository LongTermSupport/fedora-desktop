package pycompat

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"unicode/utf8"
)

// maxDepth is how deeply arrays and objects may nest before json.loads raises RecursionError.
// CPython's limit is its recursion limit less the frames already on the stack (python 3.11
// refuses near 996 at the top level), so this is an approximation of the same refusal.
const maxDepth = 990

// LoadOptions changes json.loads.
type LoadOptions struct {
	// NoDuplicateKeys is member.json's object_pairs_hook (config.py _no_duplicate_keys): a
	// repeated key raises ValueError("duplicate key") when its object closes. Without it the
	// last value wins, at the key's first position.
	NoDuplicateKeys bool
}

// errNotString is RawDecodeString handed text that does not open a JSON string.
var errNotString = errors.New("pycompat: RawDecodeString needs text that starts with a double quote")

type decoder struct {
	s     []rune
	noDup bool
	depth int
}

// decodeError is a JSONDecodeError at code point pos, with python's line and column.
func (d *decoder) decodeError(msg string, pos int) *Error {
	line, lastNL := 1, -1
	for i := 0; i < pos && i < len(d.s); i++ {
		if d.s[i] == '\n' {
			line++
			lastNL = i
		}
	}
	return &Error{Class: ClassJSONDecodeError, Msg: msg, Pos: pos, Line: line, Col: pos - lastNL}
}

func (d *decoder) skipSpace(i int) int {
	for i < len(d.s) && (d.s[i] == ' ' || d.s[i] == '\t' || d.s[i] == '\n' || d.s[i] == '\r') {
		i++
	}
	return i
}

// notUTF8 is the UnicodeDecodeError that config.py's raw.decode("utf-8") raises before
// json.loads runs. The message names the first bad byte; python's own words also say why
// (invalid start byte, unexpected end of data), which only the class is compared on.
func notUTF8(text string) *Error {
	at := 0
	for at < len(text) {
		r, n := utf8.DecodeRuneInString(text[at:])
		if r == utf8.RuneError && n == 1 {
			break
		}
		at += n
	}
	return &Error{
		Class: ClassUnicodeDecodeError, Pos: -1,
		Msg: fmt.Sprintf("'utf-8' codec can't decode byte 0x%02x in position %d", text[at], at),
	}
}

// Loads is json.loads(text) with the C scanner's rules: whitespace is space, tab, newline and
// carriage return; NaN, Infinity and -Infinity are accepted; an integer has any size (up to
// python's 4300 digits, beyond which it is a ValueError); control characters in a string are
// refused; a lone surrogate escape is kept (as WTF-8); text after the value is "Extra data";
// a leading U+FEFF is refused. text must be UTF-8: anything else is the UnicodeDecodeError
// that decoding the bytes raises first. Every error is an *Error.
func Loads(text string, opts LoadOptions) (Value, error) {
	if !utf8.ValidString(text) {
		return nil, notUTF8(text)
	}
	d := &decoder{s: []rune(text), noDup: opts.NoDuplicateKeys}
	if len(d.s) > 0 && d.s[0] == 0xFEFF {
		return nil, d.decodeError("Unexpected UTF-8 BOM (decode using utf-8-sig)", 0)
	}
	v, end, err := d.scan(d.skipSpace(0))
	if err != nil {
		return nil, err
	}
	if end = d.skipSpace(end); end != len(d.s) {
		return nil, d.decodeError("Extra data", end)
	}
	return v, nil
}

// RawDecodeString is json.JSONDecoder().raw_decode(text) for text that starts with '"': the
// string and the end of it as a BYTE offset into text (python returns the code point index;
// text[end:] is the same tail). Text that does not start with '"' is refused.
func RawDecodeString(text string) (string, int, error) {
	if text == "" || text[0] != '"' {
		return "", 0, errNotString
	}
	if !utf8.ValidString(text) {
		return "", 0, notUTF8(text)
	}
	d := &decoder{s: []rune(text)}
	s, end, err := d.scanString(0)
	if err != nil {
		return "", 0, err
	}
	return s, len(string(d.s[:end])), nil
}

// matches reports whether word is at i.
func (d *decoder) matches(i int, word string) bool {
	for _, r := range word {
		if i >= len(d.s) || d.s[i] != r {
			return false
		}
		i++
	}
	return true
}

var constants = []struct {
	word string
	v    Value
}{
	{"null", None{}},
	{"true", Bool(true)},
	{"false", Bool(false)},
	{"NaN", Float(math.NaN())},
	{"Infinity", Float(math.Inf(1))},
	{"-Infinity", Float(math.Inf(-1))},
}

// scan is scan_once: one value at i, and the index after it.
func (d *decoder) scan(i int) (Value, int, error) {
	if i >= len(d.s) {
		return nil, 0, d.decodeError("Expecting value", i)
	}
	switch c := d.s[i]; c {
	case '"':
		s, end, err := d.scanString(i)
		return Str(s), end, err
	case '{', '[':
		d.depth++
		defer func() { d.depth-- }()
		if d.depth > maxDepth {
			kind := "array"
			if c == '{' {
				kind = "object"
			}
			return nil, 0, &Error{
				Class: ClassRecursionError, Pos: -1,
				Msg: "maximum recursion depth exceeded while decoding a JSON " + kind + " from a unicode string",
			}
		}
		if c == '{' {
			return d.scanObject(i + 1)
		}
		return d.scanArray(i + 1)
	}
	for _, k := range constants {
		if d.matches(i, k.word) {
			return k.v, i + len(k.word), nil
		}
	}
	return d.scanNumber(i)
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

func (d *decoder) digitsFrom(i int) int {
	for i < len(d.s) && isDigit(d.s[i]) {
		i++
	}
	return i
}

// scanNumber is _match_number_unicode: -?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][-+]?[0-9]+)?, an int
// without a fraction or exponent, else a float (out of range is inf or 0, as float() gives).
func (d *decoder) scanNumber(start int) (Value, int, error) {
	i := start
	if i < len(d.s) && d.s[i] == '-' {
		i++
	}
	switch {
	case i < len(d.s) && d.s[i] >= '1' && d.s[i] <= '9':
		i = d.digitsFrom(i + 1)
	case i < len(d.s) && d.s[i] == '0':
		i++
	default:
		return nil, 0, d.decodeError("Expecting value", start)
	}
	end, isFloat := d.fractionAndExponent(i)
	v, err := numberValue(string(d.s[start:end]), isFloat)
	return v, end, err
}

// fractionAndExponent reads an optional ".digits" and "e[sign]digits" from i: the end of the
// number, and whether either made it a float. An exponent with no digits is not read.
func (d *decoder) fractionAndExponent(i int) (int, bool) {
	isFloat := false
	if i+1 < len(d.s) && d.s[i] == '.' && isDigit(d.s[i+1]) {
		isFloat, i = true, d.digitsFrom(i+2)
	}
	if i < len(d.s) && (d.s[i] == 'e' || d.s[i] == 'E') {
		j := i + 1
		if j < len(d.s) && (d.s[j] == '-' || d.s[j] == '+') {
			j++
		}
		if end := d.digitsFrom(j); end > j {
			isFloat, i = true, end
		}
	}
	return i, isFloat
}

// numberValue is the int or float of a number's text, as python's int() and float() read it.
func numberValue(text string, isFloat bool) (Value, error) {
	if isFloat {
		f, err := strconv.ParseFloat(text, 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return nil, fmt.Errorf("pycompat: float %q: %w", text, err)
		}
		return Float(f), nil
	}
	digits := len(text)
	if text[0] == '-' {
		digits--
	}
	if digits > maxIntDigits {
		return nil, &Error{
			Class: ClassValueError, Pos: -1,
			Msg: fmt.Sprintf("Exceeds the limit (%d digits) for integer string conversion: value has %d digits; "+
				"use sys.set_int_max_str_digits() to increase the limit", maxIntDigits, digits),
		}
	}
	n, ok := new(big.Int).SetString(text, 10)
	if !ok {
		return nil, fmt.Errorf("pycompat: integer %q", text)
	}
	return Int{V: n}, nil
}

// simpleEscapes are the one-character escapes after a backslash.
var simpleEscapes = map[rune]byte{'"': '"', '\\': '\\', '/': '/', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t'}

func (d *decoder) hex4(i int) (rune, bool) {
	var r rune
	for _, c := range d.s[i : i+4] {
		var v rune
		switch {
		case c >= '0' && c <= '9':
			v = c - '0'
		case c >= 'a' && c <= 'f':
			v = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			v = c - 'A' + 10
		default:
			return 0, false
		}
		r = r<<4 | v
	}
	return r, true
}

// scanString is scanstring_unicode (strict) for the string whose quote is at quote: its value
// and the index after its closing quote. A high surrogate escape followed by a low one is
// joined; any other surrogate is kept alone.
func (d *decoder) scanString(quote int) (string, int, error) {
	var b []byte
	end := quote + 1
	for {
		next := end
		for next < len(d.s) && d.s[next] != '"' && d.s[next] != '\\' {
			if d.s[next] <= 0x1F {
				return "", 0, d.decodeError("Invalid control character at", next)
			}
			next++
		}
		if next >= len(d.s) {
			return "", 0, d.decodeError("Unterminated string starting at", quote)
		}
		b = append(b, string(d.s[end:next])...)
		if d.s[next] == '"' {
			return string(b), next + 1, nil
		}
		next++
		if next >= len(d.s) {
			return "", 0, d.decodeError("Unterminated string starting at", quote)
		}
		if c := d.s[next]; c != 'u' {
			esc, ok := simpleEscapes[c]
			if !ok {
				return "", 0, d.decodeError(`Invalid \escape`, next-1)
			}
			b = append(b, esc)
			end = next + 1
			continue
		}
		r, after, err := d.unicodeEscape(next + 1)
		if err != nil {
			return "", 0, err
		}
		b = appendRune(b, r)
		end = after
	}
}

// unicodeEscape reads the four hex digits at i (after "\u"), joining a following low surrogate
// escape to a high surrogate, and returns the code point and the index after the escape(s).
func (d *decoder) unicodeEscape(i int) (rune, int, error) {
	end := i + 4
	if end >= len(d.s) {
		return 0, 0, d.decodeError(`Invalid \uXXXX escape`, i-1)
	}
	r, ok := d.hex4(i)
	if !ok {
		return 0, 0, d.decodeError(`Invalid \uXXXX escape`, i-1)
	}
	if r >= 0xD800 && r <= 0xDBFF && end+6 < len(d.s) && d.s[end] == '\\' && d.s[end+1] == 'u' {
		low, ok := d.hex4(end + 2)
		if !ok {
			return 0, 0, d.decodeError(`Invalid \uXXXX escape`, end+1)
		}
		if low >= 0xDC00 && low <= 0xDFFF {
			return 0x10000 + (r-0xD800)<<10 + (low - 0xDC00), end + 6, nil
		}
	}
	return r, end, nil
}

// scanObject is _parse_object_unicode from just after the '{'.
func (d *decoder) scanObject(i int) (Value, int, error) {
	dict := NewDict()
	dup := false
	i = d.skipSpace(i)
	closed := i < len(d.s) && d.s[i] == '}'
	for !closed {
		key, v, next, err := d.member(i)
		if err != nil {
			return nil, 0, err
		}
		dup = dup || dict.has(key)
		dict.Set(key, v)
		if i, closed, err = d.afterItem(next, '}'); err != nil {
			return nil, 0, err
		}
	}
	if dup && d.noDup {
		return nil, 0, &Error{Class: ClassValueError, Msg: "duplicate key", Pos: -1}
	}
	return dict, i + 1, nil
}

// member is one `"key": value` of an object at i, and the index after the value.
func (d *decoder) member(i int) (string, Value, int, error) {
	if i >= len(d.s) || d.s[i] != '"' {
		return "", nil, 0, d.decodeError("Expecting property name enclosed in double quotes", i)
	}
	key, next, err := d.scanString(i)
	if err != nil {
		return "", nil, 0, err
	}
	if i = d.skipSpace(next); i >= len(d.s) || d.s[i] != ':' {
		return "", nil, 0, d.decodeError("Expecting ':' delimiter", i)
	}
	v, next, err := d.scan(d.skipSpace(i + 1))
	return key, v, next, err
}

// afterItem is what follows an item ending at i: the closing bracket (its index, closed), or
// a ',' and the start of the next item (whitespace skipped; a closing bracket there is the
// next item's error, as python reports it); anything else is "Expecting ',' delimiter".
func (d *decoder) afterItem(i int, closing rune) (int, bool, error) {
	if i = d.skipSpace(i); i < len(d.s) && d.s[i] == closing {
		return i, true, nil
	}
	if i >= len(d.s) || d.s[i] != ',' {
		return 0, false, d.decodeError("Expecting ',' delimiter", i)
	}
	return d.skipSpace(i + 1), false, nil
}

// scanArray is _parse_array_unicode from just after the '['.
func (d *decoder) scanArray(i int) (Value, int, error) {
	list := List{}
	i = d.skipSpace(i)
	closed := i < len(d.s) && d.s[i] == ']'
	for !closed {
		v, next, err := d.scan(i)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, v)
		if i, closed, err = d.afterItem(next, ']'); err != nil {
			return nil, 0, err
		}
	}
	return list, i + 1, nil
}
