package pycompat

import (
	"errors"
	"math"
	"math/big"
	"reflect"
	"testing"
)

func mustLoads(t *testing.T, text string) Value {
	t.Helper()
	v, err := Loads(text, LoadOptions{})
	if err != nil {
		t.Fatalf("Loads(%q): %v", text, err)
	}
	return v
}

func TestDictKeepsFirstPositionLastValue(t *testing.T) {
	d := NewDict()
	d.Set("b", IntOf(1))
	d.Set("a", IntOf(2))
	d.Set("b", IntOf(3))
	if !reflect.DeepEqual(d.Keys(), []string{"b", "a"}) || d.Len() != 2 {
		t.Fatalf("keys %v", d.Keys())
	}
	v, ok := d.Get("b")
	if !ok || !Equal(v, IntOf(3)) {
		t.Fatalf("b = %v", v)
	}
	if _, ok := d.Get("z"); ok {
		t.Fatal("z is not a key")
	}
	items := d.Items()
	if len(items) != 2 || items[1].Key != "a" || !Equal(items[1].Value, IntOf(2)) {
		t.Fatalf("items %v", items)
	}
}

func TestTypeNames(t *testing.T) {
	names := map[string]Value{
		"NoneType": None{}, "bool": Bool(true), "int": IntOf(1), "float": Float(1),
		"str": Str("x"), "list": List{}, "dict": NewDict(),
	}
	for want, v := range names {
		if v.TypeName() != want {
			t.Errorf("%T is %s", v, v.TypeName())
		}
	}
}

func TestTruthy(t *testing.T) {
	falsy := []Value{None{}, Bool(false), IntOf(0), Float(0), Float(math.Copysign(0, -1)), Str(""), List{}, NewDict()}
	for _, v := range falsy {
		if Truthy(v) {
			t.Errorf("%s is truthy", PyRepr(v))
		}
	}
	truthy := []Value{Bool(true), IntOf(-1), Float(math.NaN()), Str(" "), List{None{}}}
	for _, v := range truthy {
		if !Truthy(v) {
			t.Errorf("%s is falsy", PyRepr(v))
		}
	}
}

func TestEqualNumbersAcrossKinds(t *testing.T) {
	big53 := Int{V: new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 53), big.NewInt(1))}
	cases := []struct {
		a, b Value
		want bool
	}{
		{IntOf(1), Float(1), true},
		{Bool(true), IntOf(1), true},
		{Bool(true), Float(1), true},
		{Bool(false), Float(0), true},
		{big53, Float(1 << 53), false},
		{Float(math.Inf(1)), IntOf(1), false},
		{Float(math.NaN()), Float(math.NaN()), false},
		{Float(0), Float(math.Copysign(0, -1)), true},
		{Str("1"), IntOf(1), false},
		{None{}, None{}, true},
		{None{}, Bool(false), false},
		{List{IntOf(1)}, List{Float(1)}, true},
		{List{IntOf(1)}, List{IntOf(1), IntOf(2)}, false},
		{List{Float(math.NaN())}, List{Float(math.NaN())}, true},
		{List{}, NewDict(), false},
		{Str("a"), Str("a"), true},
	}
	for _, c := range cases {
		if got := Equal(c.a, c.b); got != c.want {
			t.Errorf("%s == %s: %v", PyRepr(c.a), PyRepr(c.b), got)
		}
	}
}

func TestContainsAndIterateErrors(t *testing.T) {
	for _, c := range []struct {
		container, item Value
		msg             string
	}{
		{IntOf(1), Str("a"), "argument of type 'int' is not iterable"},
		{None{}, Str("a"), "argument of type 'NoneType' is not iterable"},
		{Str("abc"), IntOf(1), "'in <string>' requires string as left operand, not int"},
		{NewDict(), List{}, "unhashable type: 'list'"},
		{NewDict(), NewDict(), "unhashable type: 'dict'"},
	} {
		_, err := Contains(c.container, c.item)
		var pe *Error
		if !errors.As(err, &pe) || pe.Class != ClassTypeError || err.Error() != c.msg {
			t.Errorf("%s in %s: %v", PyRepr(c.item), PyRepr(c.container), err)
		}
	}
	for _, v := range []Value{IntOf(1), Float(1), Bool(true), None{}} {
		_, err := Iterate(v)
		var pe *Error
		if !errors.As(err, &pe) || pe.Class != ClassTypeError || err.Error() != "'"+v.TypeName()+"' object is not iterable" {
			t.Errorf("iter(%s): %v", PyRepr(v), err)
		}
	}
	items, err := Iterate(Str("a" + loneHigh))
	if err != nil || len(items) != 2 || items[1] != Str(loneHigh) {
		t.Fatalf("iter(str) = %v %v", items, err)
	}
}

// valueCases are JSON texts each loaded the same way on both sides, then combined.
var valueCases = []string{
	`null`, `true`, `false`, `0`, `1`, `-1`, `0.0`, `-0.0`, `1.0`, `1.5`, `NaN`, `Infinity`, `-Infinity`,
	`9007199254740993`, `9007199254740992.0`, `""`, `"a"`, `"abc"`, `"b"`, `"\ud800"`, `"x\ud800y"`,
	`[]`, `[1]`, `[1.0]`, `[true]`, `[NaN]`, `["a", "b"]`, `[[1], {"a": 1}]`, `[null]`, `["abc"]`,
	`{}`, `{"a": 1}`, `{"a": 1.0}`, `{"a": true}`, `{"b": 1, "a": 2}`, `{"a": 2, "b": 1}`, `{"a": [NaN]}`,
	`{"abc": null, "": 0}`, `"été"`, `[0, false, 0.0, "", [], {}]`,
}

func TestPythonValueOps(t *testing.T) {
	reqs := make([]pyReq, 0, (2+2*len(valueCases))*len(valueCases))
	for _, a := range valueCases {
		reqs = append(reqs, pyReq{"truthy", []any{cps(a)}}, pyReq{"iterate", []any{cps(a)}})
		for _, b := range valueCases {
			reqs = append(reqs, pyReq{"equal", []any{cps(a), cps(b)}}, pyReq{"contains", []any{cps(a), cps(b)}})
		}
	}
	out := python(t, reqs)
	i := 0
	next := func() pyResult { r := out[i]; i++; return r }
	for _, a := range valueCases {
		va := mustLoads(t, a)
		if r := next(); !sameJSON(t, Truthy(va), r.OK) {
			t.Errorf("bool(%s): python %s", a, r.OK)
		}
		items, err := Iterate(va)
		compareResult(t, "iter("+a+")", encList(items), err, next())
		for _, b := range valueCases {
			vb := mustLoads(t, b)
			if r := next(); !sameJSON(t, Equal(va, vb), r.OK) {
				t.Errorf("%s == %s: python %s", a, b, r.OK)
			}
			in, err := Contains(va, vb)
			compareResult(t, b+" in "+a, in, err, next())
		}
	}
}

func encList(vs []Value) any {
	out := make([]any, 0, len(vs))
	for _, v := range vs {
		out = append(out, enc(v))
	}
	return out
}

// compareResult checks a Go answer (value or *Error) against python's (value or exception):
// the same value, or the same exception class with the same message.
func compareResult(t *testing.T, what string, got any, err error, r pyResult) {
	t.Helper()
	if r.failed() {
		var pe *Error
		if !errors.As(err, &pe) || string(pe.Class) != r.Err || err.Error() != fromCPs(r.Msg) {
			t.Errorf("%s: python %s, Go %.200v %v", what, pyErrText(r), got, err)
		}
		return
	}
	if err != nil {
		t.Errorf("%s: python %.200s, Go %v", what, r.OK, err)
		return
	}
	if !sameJSON(t, got, r.OK) {
		t.Errorf("%s: python %.200s, Go %.200v", what, r.OK, got)
	}
}
