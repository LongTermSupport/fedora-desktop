package state

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

func num(lit string) pyjson.Number {
	kind := pyjson.Int
	if strings.ContainsAny(lit, ".eE") || lit == "NaN" {
		kind = pyjson.Float
	}
	return pyjson.Number{Lit: lit, Kind: kind}
}

func TestCheckKeysClassesUnknownAndMissingInV1sOrder(t *testing.T) {
	o := pyjson.Ordered{{Key: "v", Value: num("1")}, {Key: "x", Value: num("1")}}
	wantClass(t, "unknown first", checkKeys("/f", o, []string{"v", "y"}, nil, unknownFirst), ClassUnknownKey)
	wantClass(t, "missing first", checkKeys("/g", o, []string{"v", "y"}, nil, missingFirst), ClassMissingKey)
	if err := checkKeys("/f", o, []string{"v"}, []string{"x", "z"}, missingFirst); err != nil {
		t.Errorf("an optional key: %v", err)
	}
	if err := checkKeys("/f", o, []string{"v", "x"}, nil, unknownFirst); err != nil {
		t.Errorf("exact keys: %v", err)
	}
}

// S5, SB6: `v` is the integer 1; another integer is a version, anything else a type.
func TestVersionIsTheIntegerOne(t *testing.T) {
	if err := version("/f", num("1")); err != nil {
		t.Error(err)
	}
	wantClass(t, "v 2", version("/f", num("2")), ClassVersion)
	for _, v := range []any{true, num("1.0"), "1", pyjson.Null{}} {
		wantClass(t, "v not an int", version("/f", v), ClassType)
	}
}

func TestIntFieldIsPythonsIntWithABound(t *testing.T) {
	if n, err := intField("/f", "n", num("-0"), 0); err != nil || n != 0 {
		t.Errorf("-0: %d %v", n, err)
	}
	wantClass(t, "below the bound", errOf(intField("/g", "m", num("-1"), 0)), ClassGrammar)
	wantClass(t, "past int64", errOf(intField("/f", "n", num("99999999999999999999"), 0)), ClassGrammar)
	for _, v := range []any{true, num("1.0"), "1", pyjson.Null{}} {
		wantClass(t, "not an int", errOf(intField("/f", "n", v, 0)), ClassType)
	}
	if n, err := intField("/f", "generation", num("1"), 1); err != nil || n != 1 {
		t.Errorf("a generation: %d %v", n, err)
	}
	if n, err := anyInt("/f", "n", num("-5")); err != nil || n != -5 {
		t.Errorf("any int: %d %v", n, err)
	}
}

func errOf[T any](_ T, err error) error { return err }

func TestStringFields(t *testing.T) {
	isA := func(s string) bool { return s == "a" }
	if s, err := strField("/f", "s", "a", isA); err != nil || s != "a" {
		t.Errorf("%q %v", s, err)
	}
	wantClass(t, "grammar", errOf(strField("/f", "s", "b", isA)), ClassGrammar)
	wantClass(t, "type", errOf(strField("/f", "s", num("1"), isA)), ClassType)
	if s, set, err := nullableStr("/f", "s", pyjson.Null{}, isA); err != nil || set || ptr(s, set) != nil {
		t.Errorf("null: %v %v", set, err)
	}
	if s, set, err := nullableStr("/f", "s", "a", isA); err != nil || !set || *ptr(s, set) != "a" {
		t.Errorf("a: %v %v", s, err)
	}
	if _, _, err := nullableStr("/f", "s", "b", isA); classOf(err) != ClassGrammar {
		t.Errorf("nullable grammar: %v", err)
	}
}

func TestListMapAndBoolFields(t *testing.T) {
	if l, err := listField("/f", "l", []any{num("1")}, 1); err != nil || len(l) != 1 {
		t.Errorf("%v %v", l, err)
	}
	wantClass(t, "list size", errOf(listField("/f", "l", []any{num("1"), num("2")}, 1)), ClassSize)
	wantClass(t, "list type", errOf(listField("/f", "l", pyjson.Ordered{}, 1)), ClassType)
	if m, err := mapField("/f", "m", pyjson.Ordered{}, 1); err != nil || m == nil {
		t.Errorf("%v %v", m, err)
	}
	wantClass(t, "map size", errOf(mapField("/f", "m", pyjson.Ordered{{Key: "a"}, {Key: "b"}}, 1)), ClassSize)
	wantClass(t, "map type", errOf(mapField("/f", "m", []any{}, 1)), ClassType)
	if b, err := boolField("/f", "b", true); err != nil || !b {
		t.Errorf("%v %v", b, err)
	}
	wantClass(t, "bool type", errOf(boolField("/f", "b", num("1"))), ClassType)
}

// The encoders build values back for pyjson.Encode.
func TestValueBuilders(t *testing.T) {
	s := "x"
	out, err := pyjson.Encode([]any{nullable(nil), nullable(&s), strings2list([]string{"a"}), intOrNull(nil)}, pyjson.Compact)
	if err != nil || string(out) != `[null,"x",["a"],null]` {
		t.Errorf("%s %v", out, err)
	}
	var n int64 = 3
	if out, err = pyjson.Encode(intOrNull(&n), pyjson.Compact); err != nil || string(out) != "3" {
		t.Errorf("%s %v", out, err)
	}
}
