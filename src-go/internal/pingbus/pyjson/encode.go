package pyjson

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Style is one json.dumps call pingbus's Python makes when it writes a state file (Plan 00209
// SPEC-state.md S17). Every style is ensure_ascii.
type Style int

const (
	// Compact is ensure_ascii=True, separators=(",", ":"), keys in insertion order, no newline.
	Compact Style = iota + 1
	// Spaced is ensure_ascii=True with the default separators, insertion order, no newline.
	Spaced
	// Sorted is ensure_ascii=True, sort_keys=True, the default separators, then a newline.
	Sorted
	// SortedBare is sort_keys=True with the default separators and no newline.
	SortedBare
)

// ErrNotEncodable is a value no state file holds: a float, an unordered Object in an
// insertion-order style, text that is not WTF-8, or a Go type with no JSON form.
var ErrNotEncodable = errors.New("not a value a state file holds")

// Member is one key of an Ordered object and its value.
type Member struct {
	Key   string
	Value any
}

// Ordered is a JSON object with its keys in the order they were given, as a Python dict keeps
// them; Options.Ordered decodes objects as it.
type Ordered []Member

// Get is the value of key, and whether it is present.
func (o Ordered) Get(key string) (any, bool) {
	for _, m := range o {
		if m.Key == key {
			return m.Value, true
		}
	}
	return nil, false
}

// Keys are the object's keys in order.
func (o Ordered) Keys() []string {
	keys := make([]string, len(o))
	for i, m := range o {
		keys[i] = m.Key
	}
	return keys
}

// set is a Python dict's assignment: a key already present keeps its place and takes the value.
func (o Ordered) set(key string, v any) Ordered {
	for i := range o {
		if o[i].Key == key {
			o[i].Value = v
			return o
		}
	}
	return append(o, Member{key, v})
}

// Encode is json.dumps(v, ...) in style, as bytes: a value is Null, a bool, a string, an Int
// Number, an int or int64, a []any, an Ordered or (in a sorted style) an Object.
func Encode(v any, style Style) ([]byte, error) {
	e := encoder{sorted: style == Sorted || style == SortedBare, item: ", ", key: ": "}
	switch style {
	case Compact:
		e.item, e.key = ",", ":"
	case Spaced, Sorted, SortedBare:
	default:
		return nil, fmt.Errorf("%w: style %d", ErrNotEncodable, style)
	}
	if err := e.value(v, 0); err != nil {
		return nil, err
	}
	if style == Sorted {
		e.out = append(e.out, '\n')
	}
	return e.out, nil
}

type encoder struct {
	out       []byte
	sorted    bool
	item, key string
}

func (e *encoder) value(v any, depth int) error {
	switch x := v.(type) {
	case Null:
		e.out = append(e.out, "null"...)
	case bool:
		e.out = strconv.AppendBool(e.out, x)
	case string:
		return e.str(x)
	case int:
		e.out = strconv.AppendInt(e.out, int64(x), 10)
	case int64:
		e.out = strconv.AppendInt(e.out, x, 10)
	case Number:
		i, ok := x.bigInt()
		if !ok {
			return fmt.Errorf("%w: the number %q is not an integer", ErrNotEncodable, x.Lit)
		}
		e.out = i.Append(e.out, 10)
	case []any:
		return e.array(x, depth)
	case Ordered:
		return e.object(x, depth)
	case Object:
		if !e.sorted {
			return fmt.Errorf("%w: an unordered object in an insertion-order style", ErrNotEncodable)
		}
		o := make(Ordered, 0, len(x))
		for k, item := range x {
			o = append(o, Member{k, item})
		}
		return e.object(o, depth)
	default:
		return fmt.Errorf("%w: a Go %T", ErrNotEncodable, v)
	}
	return nil
}

// enter refuses a container deeper than the decoder reads.
func enter(depth int) error {
	if depth >= maxDepth {
		return fmt.Errorf("%w: nested too deeply", ErrNotEncodable)
	}
	return nil
}

func (e *encoder) array(list []any, depth int) error {
	if err := enter(depth); err != nil {
		return err
	}
	e.out = append(e.out, '[')
	for i, item := range list {
		if i > 0 {
			e.out = append(e.out, e.item...)
		}
		if err := e.value(item, depth+1); err != nil {
			return err
		}
	}
	e.out = append(e.out, ']')
	return nil
}

func (e *encoder) object(o Ordered, depth int) error {
	if err := enter(depth); err != nil {
		return err
	}
	if e.sorted {
		o = slices.Clone(o)
		slices.SortFunc(o, func(a, b Member) int { return strings.Compare(a.Key, b.Key) })
	}
	e.out = append(e.out, '{')
	for i, m := range o {
		if i > 0 {
			if slices.ContainsFunc(o[:i], func(p Member) bool { return p.Key == m.Key }) {
				return fmt.Errorf("%w: a key given twice", ErrNotEncodable)
			}
			e.out = append(e.out, e.item...)
		}
		if err := e.str(m.Key); err != nil {
			return err
		}
		e.out = append(e.out, e.key...)
		if err := e.value(m.Value, depth+1); err != nil {
			return err
		}
	}
	e.out = append(e.out, '}')
	return nil
}

var shortEscapes = map[rune]string{'"': `\"`, '\\': `\\`, '\n': `\n`, '\r': `\r`, '\t': `\t`, '\b': `\b`, '\f': `\f`}

// str is Python's ensure_ascii string: printable ASCII as itself, the short escapes, and every
// other code point as \uXXXX in lowercase hex, a pair above U+FFFF. A lone surrogate (kept as
// its generalised UTF-8 form) is its own escape, as Python writes it.
func (e *encoder) str(s string) error {
	runes, err := decodeWTF8([]byte(s))
	if err != nil {
		return fmt.Errorf("%w: text that is not UTF-8", ErrNotEncodable)
	}
	e.out = append(e.out, '"')
	for _, r := range runes {
		if short, ok := shortEscapes[r]; ok {
			e.out = append(e.out, short...)
			continue
		}
		switch {
		case r >= ' ' && r <= '~':
			e.out = append(e.out, byte(r))
		case r > 0xffff:
			r -= 0x10000
			e.out = appendUnit(e.out, 0xd800+(r>>10))
			e.out = appendUnit(e.out, 0xdc00+(r&0x3ff))
		default:
			e.out = appendUnit(e.out, r)
		}
	}
	e.out = append(e.out, '"')
	return nil
}

func appendUnit(b []byte, unit rune) []byte {
	return fmt.Appendf(b, `\u%04x`, unit)
}
