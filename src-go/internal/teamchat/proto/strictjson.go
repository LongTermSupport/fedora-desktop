package proto

import (
	"fmt"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// MaxDepth is how deeply arrays and objects may nest. Matrix content never comes close, and
// the bound keeps a hostile document from exhausting the stack.
const MaxDepth = 64

// maxSafeInt is 2^53-1, the largest integer Matrix's canonical JSON allows (A 2.1).
const maxSafeInt = 1<<53 - 1

// Kind is the type of a JSON value.
type Kind int

// The kinds of JSON value. A number is always an integer (A 2.1).
const (
	KindNull Kind = iota
	KindBool
	KindInt
	KindString
	KindArray
	KindObject
)

// Value is one strictly decoded JSON value: an object keeps its members in input order, and
// its keys are distinct.
type Value struct {
	kind    Kind
	b       bool
	i       int64
	s       string
	items   []Value
	members []Member
}

// Member is one key and value of an object.
type Member struct {
	Key string
	Val Value
}

// SyntaxError is a document the strict decoder refuses (A 2.1). Its text names the offset and
// the rule, never the input, so a refused value is never rendered (A 2.2).
type SyntaxError struct {
	Offset int
	Rule   string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("strict JSON: %s at byte %d", e.Rule, e.Offset)
}

// Null is the JSON null.
func Null() Value { return Value{kind: KindNull} }

// Bool is a JSON boolean.
func Bool(b bool) Value { return Value{kind: KindBool, b: b} }

// Int is a JSON integer.
func Int(i int64) Value { return Value{kind: KindInt, i: i} }

// Str is a JSON string.
func Str(s string) Value { return Value{kind: KindString, s: s} }

// Arr is a JSON array.
func Arr(items ...Value) Value { return Value{kind: KindArray, items: items} }

// Obj is a JSON object. It does not check that its keys are distinct: the builders are for
// content this package writes.
func Obj(members ...Member) Value { return Value{kind: KindObject, members: members} }

// M is one object member.
func M(key string, val Value) Member { return Member{Key: key, Val: val} }

// Kind is the value's type.
func (v Value) Kind() Kind { return v.kind }

// Bool is a boolean's value; false for any other kind.
func (v Value) Bool() bool { return v.b }

// Int is an integer's value; 0 for any other kind.
func (v Value) Int() int64 { return v.i }

// Str is a string's value; empty for any other kind.
func (v Value) Str() string { return v.s }

// Items are an array's elements; nil for any other kind.
func (v Value) Items() []Value { return v.items }

// Members are an object's members in input order; nil for any other kind.
func (v Value) Members() []Member { return v.members }

// Get is an object's value under key.
func (v Value) Get(key string) (Value, bool) {
	for _, m := range v.members {
		if m.Key == key {
			return m.Val, true
		}
	}
	return Value{}, false
}

// Equal is JSON equality: objects compare as sets of members, whatever their order.
func (v Value) Equal(o Value) bool {
	if v.kind != o.kind {
		return false
	}
	switch v.kind {
	case KindNull:
		return true
	case KindBool:
		return v.b == o.b
	case KindInt:
		return v.i == o.i
	case KindString:
		return v.s == o.s
	case KindArray:
		if len(v.items) != len(o.items) {
			return false
		}
		for i := range v.items {
			if !v.items[i].Equal(o.items[i]) {
				return false
			}
		}
		return true
	case KindObject:
		if len(v.members) != len(o.members) {
			return false
		}
		for _, m := range v.members {
			other, ok := o.Get(m.Key)
			if !ok || !m.Val.Equal(other) {
				return false
			}
		}
		return true
	}
	return false
}

// Compact is the value's compact UTF-8 JSON: no spaces, members in their order, strings
// escaping only the quote, the backslash and C0 controls (A 2.1).
func (v Value) Compact() []byte {
	return v.appendCompact(nil)
}

// Size is the byte length of Compact, the size every content cap of A is measured in.
func (v Value) Size() int {
	return len(v.Compact())
}

func (v Value) appendCompact(out []byte) []byte {
	switch v.kind {
	case KindNull:
		return append(out, "null"...)
	case KindBool:
		return strconv.AppendBool(out, v.b)
	case KindInt:
		return strconv.AppendInt(out, v.i, 10)
	case KindString:
		return appendJSONString(out, v.s)
	case KindArray:
		out = append(out, '[')
		for i, item := range v.items {
			if i > 0 {
				out = append(out, ',')
			}
			out = item.appendCompact(out)
		}
		return append(out, ']')
	case KindObject:
		out = append(out, '{')
		for i, m := range v.members {
			if i > 0 {
				out = append(out, ',')
			}
			out = appendJSONString(out, m.Key)
			out = append(out, ':')
			out = m.Val.appendCompact(out)
		}
		return append(out, '}')
	}
	return out
}

func appendJSONString(out []byte, s string) []byte {
	const hex = "0123456789abcdef"
	out = append(out, '"')
	for i := range len(s) {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			out = append(out, '\\', c)
		case c == '\b':
			out = append(out, '\\', 'b')
		case c == '\f':
			out = append(out, '\\', 'f')
		case c == '\n':
			out = append(out, '\\', 'n')
		case c == '\r':
			out = append(out, '\\', 'r')
		case c == '\t':
			out = append(out, '\\', 't')
		case c < 0x20:
			out = append(out, '\\', 'u', '0', '0', hex[c>>4], hex[c&0xf])
		default:
			out = append(out, c)
		}
	}
	return append(out, '"')
}

// Parse decodes one JSON document by A 2.1's rules: valid UTF-8, no lone surrogate escape, no
// repeated key, integers only within plus or minus 2^53-1, JSON whitespace only, nothing after
// the value. It repairs nothing.
func Parse(data []byte) (Value, error) {
	p := parser{data: data}
	p.space()
	v, err := p.value(0)
	if err != nil {
		return Value{}, err
	}
	p.space()
	if p.pos != len(p.data) {
		return Value{}, p.fail("content after the value")
	}
	return v, nil
}

type parser struct {
	data []byte
	pos  int
}

func (p *parser) fail(rule string) error {
	return &SyntaxError{Offset: p.pos, Rule: rule}
}

func (p *parser) space() {
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *parser) value(depth int) (Value, error) {
	if p.pos >= len(p.data) {
		return Value{}, p.fail("a value is missing")
	}
	switch c := p.data[p.pos]; {
	case c == '{':
		return p.object(depth + 1)
	case c == '[':
		return p.array(depth + 1)
	case c == '"':
		s, err := p.str()
		return Str(s), err
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	default:
		return p.literal()
	}
}

func (p *parser) literal() (Value, error) {
	for _, lit := range []struct {
		text string
		val  Value
	}{{"true", Bool(true)}, {"false", Bool(false)}, {"null", Null()}} {
		if len(p.data)-p.pos >= len(lit.text) && string(p.data[p.pos:p.pos+len(lit.text)]) == lit.text {
			p.pos += len(lit.text)
			return lit.val, nil
		}
	}
	return Value{}, p.fail("not a JSON value")
}

func (p *parser) number() (Value, error) {
	start := p.pos
	if p.data[p.pos] == '-' {
		p.pos++
	}
	digits := p.pos
	for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
		p.pos++
	}
	switch {
	case p.pos == digits:
		return Value{}, p.fail("a number has no digits")
	case p.data[digits] == '0' && p.pos-digits > 1:
		return Value{}, p.fail("a number has a leading zero")
	case p.pos < len(p.data) && (p.data[p.pos] == '.' || p.data[p.pos] == 'e' || p.data[p.pos] == 'E'):
		return Value{}, &SyntaxError{Offset: start, Rule: "a number is not an integer"}
	}
	n, err := strconv.ParseInt(string(p.data[start:p.pos]), 10, 64)
	if err != nil || n > maxSafeInt || n < -maxSafeInt {
		return Value{}, &SyntaxError{Offset: start, Rule: "an integer outside plus or minus 2^53-1"}
	}
	return Int(n), nil
}

func (p *parser) array(depth int) (Value, error) {
	if depth > MaxDepth {
		return Value{}, p.fail("nesting too deep")
	}
	p.pos++
	p.space()
	items := []Value{}
	if p.pos < len(p.data) && p.data[p.pos] == ']' {
		p.pos++
		return Arr(items...), nil
	}
	for {
		p.space()
		item, err := p.value(depth)
		if err != nil {
			return Value{}, err
		}
		items = append(items, item)
		p.space()
		if p.pos >= len(p.data) {
			return Value{}, p.fail("an array is not closed")
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return Arr(items...), nil
		default:
			return Value{}, p.fail("an array element is not followed by a comma or a bracket")
		}
	}
}

func (p *parser) object(depth int) (Value, error) {
	if depth > MaxDepth {
		return Value{}, p.fail("nesting too deep")
	}
	p.pos++
	p.space()
	members := []Member{}
	if p.pos < len(p.data) && p.data[p.pos] == '}' {
		p.pos++
		return Obj(members...), nil
	}
	seen := map[string]bool{}
	for {
		p.space()
		m, err := p.member(depth, seen)
		if err != nil {
			return Value{}, err
		}
		members = append(members, m)
		p.space()
		if p.pos >= len(p.data) {
			return Value{}, p.fail("an object is not closed")
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return Obj(members...), nil
		default:
			return Value{}, p.fail("an object member is not followed by a comma or a brace")
		}
	}
}

func (p *parser) member(depth int, seen map[string]bool) (Member, error) {
	if p.pos >= len(p.data) || p.data[p.pos] != '"' {
		return Member{}, p.fail("an object key is not a string")
	}
	keyAt := p.pos
	key, err := p.str()
	if err != nil {
		return Member{}, err
	}
	if seen[key] {
		return Member{}, &SyntaxError{Offset: keyAt, Rule: "a repeated object key"}
	}
	seen[key] = true
	p.space()
	if p.pos >= len(p.data) || p.data[p.pos] != ':' {
		return Member{}, p.fail("an object key is not followed by a colon")
	}
	p.pos++
	p.space()
	val, err := p.value(depth)
	if err != nil {
		return Member{}, err
	}
	return M(key, val), nil
}

func (p *parser) str() (string, error) {
	p.pos++
	var out []byte
	for {
		if p.pos >= len(p.data) {
			return "", p.fail("a string is not closed")
		}
		c := p.data[p.pos]
		switch {
		case c == '"':
			p.pos++
			return string(out), nil
		case c == '\\':
			r, err := p.escape()
			if err != nil {
				return "", err
			}
			out = utf8.AppendRune(out, r)
		case c < 0x20:
			return "", p.fail("a control character inside a string")
		case c < utf8.RuneSelf:
			out = append(out, c)
			p.pos++
		default:
			r, size := utf8.DecodeRune(p.data[p.pos:])
			if r == utf8.RuneError && size <= 1 {
				return "", p.fail("invalid UTF-8")
			}
			out = append(out, p.data[p.pos:p.pos+size]...)
			p.pos += size
		}
	}
}

// escape reads one escape at p.pos (the backslash) and the low half that must follow a high
// surrogate.
func (p *parser) escape() (rune, error) {
	if p.pos+1 >= len(p.data) {
		return 0, p.fail("a string is not closed")
	}
	simple := map[byte]rune{'"': '"', '\\': '\\', '/': '/', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t'}
	if r, ok := simple[p.data[p.pos+1]]; ok {
		p.pos += 2
		return r, nil
	}
	first, err := p.hex4()
	if err != nil {
		return 0, err
	}
	switch {
	case first >= 0xDC00 && first <= 0xDFFF:
		return 0, p.fail("a lone low surrogate escape")
	case first < 0xD800 || first > 0xDBFF:
		return first, nil
	}
	if p.pos+1 >= len(p.data) || p.data[p.pos] != '\\' || p.data[p.pos+1] != 'u' {
		return 0, p.fail("a lone high surrogate escape")
	}
	second, err := p.hex4()
	if err != nil {
		return 0, err
	}
	if second < 0xDC00 || second > 0xDFFF {
		return 0, p.fail("a lone high surrogate escape")
	}
	return utf16.DecodeRune(first, second), nil
}

// hex4 reads `\uXXXX` at p.pos.
func (p *parser) hex4() (rune, error) {
	if p.data[p.pos+1] != 'u' {
		return 0, p.fail("an unknown escape")
	}
	if p.pos+6 > len(p.data) {
		return 0, p.fail("a short unicode escape")
	}
	n, err := strconv.ParseUint(string(p.data[p.pos+2:p.pos+6]), 16, 16)
	if err != nil {
		return 0, p.fail("a short unicode escape")
	}
	p.pos += 6
	return rune(n), nil
}

// fields reads one object's members by key and keeps the first rule a member broke, so a
// decoder reads every field in a straight line and asks once whether all were well formed. A
// member of the wrong kind breaks its rule; null is never taken for absent.
type fields struct {
	obj  Value
	rule string
}

func readFields(v Value, rule string) *fields {
	f := &fields{obj: v}
	if v.kind != KindObject {
		f.rule = rule
	}
	return f
}

func (f *fields) fail(rule string) {
	if f.rule == "" {
		f.rule = rule
	}
}

func (f *fields) has(key string) bool {
	_, ok := f.obj.Get(key)
	return ok
}

func (f *fields) get(key string, kind Kind, required bool, rule string) (Value, bool) {
	v, ok := f.obj.Get(key)
	if !ok {
		if required {
			f.fail(rule)
		}
		return Value{}, false
	}
	if v.kind != kind {
		f.fail(rule)
		return Value{}, false
	}
	return v, true
}

func (f *fields) integer(key string, required bool, def int64, rule string) int64 {
	if v, ok := f.get(key, KindInt, required, rule); ok {
		return v.i
	}
	return def
}

func (f *fields) str(key string, required bool, rule string) string {
	if v, ok := f.get(key, KindString, required, rule); ok {
		return v.s
	}
	return ""
}

func (f *fields) boolean(key string, def bool, rule string) bool {
	if v, ok := f.get(key, KindBool, false, rule); ok {
		return v.b
	}
	return def
}

func (f *fields) object(key string, required bool, rule string) (Value, bool) {
	return f.get(key, KindObject, required, rule)
}

// need is a required member of kind: null when it is missing or of another kind, which breaks
// rule here and again in any reader of the null.
func (f *fields) need(key string, kind Kind, rule string) Value {
	if v, ok := f.get(key, kind, true, rule); ok {
		return v
	}
	return Null()
}

// strs is an array of strings; nil when absent.
func (f *fields) strs(key string, required bool, rule string) []string {
	v, ok := f.get(key, KindArray, required, rule)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(v.items))
	for _, item := range v.items {
		if item.kind != KindString {
			f.fail(rule)
			return nil
		}
		out = append(out, item.s)
	}
	return out
}
