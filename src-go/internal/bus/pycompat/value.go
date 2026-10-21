// Package pycompat is the part of Python 3's semantics that agent-bus v1's readers depend on
// (helpers/agent_bus/declare.py's host_vars line reader and helpers/pingbus/config.py's
// member.json decoder), so the Go readers that replace them (bus/declare, bus/seat) answer
// exactly as v1 does while v1 lives (Plan 00209 SPEC-view.md section 1): python's whitespace,
// line splitting, lower-casing and decimal digits; json.loads with the C scanner's rules and
// error classes, raw_decode of a string and json.dumps; the json values with truthiness, ==,
// `in`, iteration, str() and repr(); and ipaddress.ip_address's canonical form.
//
// Strings are Go strings in WTF-8: valid UTF-8, except that a lone surrogate (which
// json.loads keeps from a \uD800 escape) is its three-byte generalised UTF-8 form, so byte
// order is code point order and every string operation here counts it as one code point.
//
// Every function is proven against the real python3 by its tests. The package goes when v1 is
// retired (stage 8 of the plan): it exists to reproduce v1, not as a general Python runtime.
package pycompat

import (
	"math"
	"math/big"
	"strings"
)

// Value is one value json.loads or the host_vars line reader yields: None, Bool, Int, Float,
// Str, List or *Dict.
type Value interface {
	// TypeName is python's type(v).__name__, as error messages name it.
	TypeName() string
	isValue()
}

// None is python's None (JSON null).
type None struct{}

// Bool is python's bool.
type Bool bool

// Int is python's int, of any size.
type Int struct{ V *big.Int }

// IntOf is the Int of a Go integer.
func IntOf(i int64) Int { return Int{V: big.NewInt(i)} }

// Float is python's float.
type Float float64

// Str is python's str, in WTF-8.
type Str string

// List is python's list.
type List []Value

// Item is one key and its value in a Dict.
type Item struct {
	Key   string
	Value Value
}

// Dict is python's dict with str keys, in insertion order. Setting a key again replaces its
// value and keeps its first position, as json.loads does with a repeated key.
type Dict struct {
	items []Item
	index map[string]int
}

// NewDict is an empty Dict.
func NewDict() *Dict { return &Dict{index: map[string]int{}} }

// Set sets key to v: a new key is appended, an existing one keeps its position.
func (d *Dict) Set(key string, v Value) {
	if i, ok := d.index[key]; ok {
		d.items[i].Value = v
		return
	}
	d.index[key] = len(d.items)
	d.items = append(d.items, Item{Key: key, Value: v})
}

// Get is the value of key.
func (d *Dict) Get(key string) (Value, bool) {
	i, ok := d.index[key]
	if !ok {
		return nil, false
	}
	return d.items[i].Value, true
}

// Len is the number of keys.
func (d *Dict) Len() int { return len(d.items) }

// Keys are the keys in order.
func (d *Dict) Keys() []string {
	keys := make([]string, 0, len(d.items))
	for _, it := range d.items {
		keys = append(keys, it.Key)
	}
	return keys
}

// Items are the keys and values in order.
func (d *Dict) Items() []Item { return append([]Item(nil), d.items...) }

// TypeName is "dict".
func (*Dict) TypeName() string { return "dict" }

func (d *Dict) has(key string) bool {
	_, ok := d.index[key]
	return ok
}

// TypeName is "NoneType".
func (None) TypeName() string { return "NoneType" }

// TypeName is "bool".
func (Bool) TypeName() string { return "bool" }

// TypeName is "int".
func (Int) TypeName() string { return "int" }

// TypeName is "float".
func (Float) TypeName() string { return "float" }

// TypeName is "str".
func (Str) TypeName() string { return "str" }

// TypeName is "list".
func (List) TypeName() string { return "list" }

func (None) isValue()  {}
func (Bool) isValue()  {}
func (Int) isValue()   {}
func (Float) isValue() {}
func (Str) isValue()   {}
func (List) isValue()  {}
func (*Dict) isValue() {}

// ErrorClass is the name of the python exception class an operation raises, as v1 prints it
// (config.py: `type(exc).__name__`).
type ErrorClass string

// The exception classes these operations raise.
const (
	ClassJSONDecodeError    ErrorClass = "JSONDecodeError"
	ClassValueError         ErrorClass = "ValueError"
	ClassRecursionError     ErrorClass = "RecursionError"
	ClassUnicodeDecodeError ErrorClass = "UnicodeDecodeError"
	ClassUnicodeEncodeError ErrorClass = "UnicodeEncodeError"
	ClassTypeError          ErrorClass = "TypeError"
)

// Error is a python exception: its class, and its message as str(exc) gives it. A
// JSONDecodeError also has its position (Pos in code points, Line and Col from 1), which
// str(exc) includes; Pos is -1 for the other classes.
type Error struct {
	Class     ErrorClass
	Msg       string
	Pos       int
	Line, Col int
}

func (e *Error) Error() string {
	if e.Class != ClassJSONDecodeError {
		return e.Msg
	}
	return e.Msg + ": line " + itoa(e.Line) + " column " + itoa(e.Col) + " (char " + itoa(e.Pos) + ")"
}

func itoa(i int) string { return big.NewInt(int64(i)).String() }

func typeError(msg string) *Error { return &Error{Class: ClassTypeError, Msg: msg, Pos: -1} }

// Truthy is python's bool(v): None, False, 0, 0.0, "", [] and {} are false.
func Truthy(v Value) bool {
	switch x := v.(type) {
	case Bool:
		return bool(x)
	case Int:
		return x.V.Sign() != 0
	case Float:
		return x != 0
	case Str:
		return x != ""
	case List:
		return len(x) > 0
	case *Dict:
		return x.Len() > 0
	}
	return false
}

// number is a numeric value as an exact integer or a float; bool is the integer 0 or 1.
type number struct {
	i       *big.Int
	f       float64
	isFloat bool
}

func asNumber(v Value) (number, bool) {
	switch x := v.(type) {
	case Bool:
		if x {
			return number{i: big.NewInt(1)}, true
		}
		return number{i: big.NewInt(0)}, true
	case Int:
		return number{i: x.V}, true
	case Float:
		return number{f: float64(x), isFloat: true}, true
	}
	return number{}, false
}

func numberEqual(a, b number) bool {
	switch {
	case a.isFloat && b.isFloat:
		return a.f == b.f
	case !a.isFloat && !b.isFloat:
		return a.i.Cmp(b.i) == 0
	case a.isFloat:
		a, b = b, a
	}
	// a is the integer, b the float: python compares them exactly, not by converting.
	if math.IsNaN(b.f) || math.IsInf(b.f, 0) {
		return false
	}
	return new(big.Float).SetInt(a.i).Cmp(big.NewFloat(b.f)) == 0
}

func isNaN(v Value) bool {
	f, ok := v.(Float)
	return ok && math.IsNaN(float64(f))
}

// Equal is python's a == b: 1 == 1.0 == True, a str equals only a str, lists and dicts
// compare deeply. A NaN never equals itself at the top, but inside a list or dict two NaNs are
// equal: json.loads returns one NaN object for every NaN, and python compares container items
// by identity first.
func Equal(a, b Value) bool { return equal(a, b, false) }

func equal(a, b Value, nested bool) bool {
	if nested && isNaN(a) && isNaN(b) {
		return true
	}
	if na, ok := asNumber(a); ok {
		nb, isNum := asNumber(b)
		return isNum && numberEqual(na, nb)
	}
	switch x := a.(type) {
	case None:
		_, ok := b.(None)
		return ok
	case Str:
		y, ok := b.(Str)
		return ok && x == y
	case List:
		y, ok := b.(List)
		return ok && equalLists(x, y)
	case *Dict:
		y, ok := b.(*Dict)
		return ok && equalDicts(x, y)
	}
	return false
}

func equalLists(x, y List) bool {
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if !equal(x[i], y[i], true) {
			return false
		}
	}
	return true
}

func equalDicts(x, y *Dict) bool {
	if x.Len() != y.Len() {
		return false
	}
	for _, it := range x.items {
		other, has := y.Get(it.Key)
		if !has || !equal(it.Value, other, true) {
			return false
		}
	}
	return true
}

// Contains is python's `item in container`: a list's elements by == (identity first), a str's
// substrings (a non-str item is a TypeError), a dict's keys (an unhashable list or dict item is
// a TypeError). Any other container is a TypeError: v1 crashed there (SPEC-view.md B-S3).
func Contains(container, item Value) (bool, error) {
	switch c := container.(type) {
	case List:
		for _, x := range c {
			if equal(x, item, true) {
				return true, nil
			}
		}
		return false, nil
	case Str:
		s, ok := item.(Str)
		if !ok {
			return false, typeError("'in <string>' requires string as left operand, not " + item.TypeName())
		}
		return strings.Contains(string(c), string(s)), nil
	case *Dict:
		switch k := item.(type) {
		case List, *Dict:
			return false, typeError("unhashable type: '" + item.TypeName() + "'")
		case Str:
			_, ok := c.Get(string(k))
			return ok, nil
		}
		return false, nil
	}
	return false, typeError("argument of type '" + container.TypeName() + "' is not iterable")
}

// Iterate is python's iter(v): a str's characters, a list's items, a dict's keys. Any other
// value is a TypeError.
func Iterate(v Value) ([]Value, error) {
	switch x := v.(type) {
	case Str:
		rs := CodePoints(string(x))
		out := make([]Value, 0, len(rs))
		for _, r := range rs {
			out = append(out, Str(appendRune(nil, r)))
		}
		return out, nil
	case List:
		return append([]Value(nil), x...), nil
	case *Dict:
		out := make([]Value, 0, x.Len())
		for _, it := range x.items {
			out = append(out, Str(it.Key))
		}
		return out, nil
	}
	return nil, typeError("'" + v.TypeName() + "' object is not iterable")
}
