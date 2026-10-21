package pyjson

import (
	"errors"
	"strings"
	"testing"
)

func n(lit string) Number { return Number{Lit: lit, Kind: Int} }

// sample is one value in every shape the state files hold, with insertion order unlike sorted
// order. The expected texts were printed by CPython 3.11's json.dumps (SPEC-state.md S17).
func sample() Ordered {
	return Ordered{
		{"b", []any{n("1"), Ordered{{"z", Null{}}, {"a", true}}}},
		{"a", "é\U0001F600\xed\xa0\x80\x7f\x00\n\"\\/ \t\b\f\r\x1f"},
		{"c", Ordered{}},
		{"d", []any{}},
	}
}

var sampleString = `"` + esc("00e9") + esc("d83d") + esc("de00") + esc("d800") + esc("007f") + esc("0000") +
	`\n\"\\/ \t\b\f\r` + esc("001f") + `"`

func TestEncodeWritesWhatPythonWrites(t *testing.T) {
	cases := []struct {
		style Style
		want  string
	}{
		{Compact, `{"b":[1,{"z":null,"a":true}],"a":` + sampleString + `,"c":{},"d":[]}`},
		{Spaced, `{"b": [1, {"z": null, "a": true}], "a": ` + sampleString + `, "c": {}, "d": []}`},
		{Sorted, `{"a": ` + sampleString + `, "b": [1, {"a": true, "z": null}], "c": {}, "d": []}` + "\n"},
		{SortedBare, `{"a": ` + sampleString + `, "b": [1, {"a": true, "z": null}], "c": {}, "d": []}`},
	}
	for _, c := range cases {
		got, err := Encode(sample(), c.style)
		if err != nil || string(got) != c.want {
			t.Errorf("style %d: got %s, %v\nwant %s", c.style, got, err, c.want)
		}
	}
}

func TestEncodeWritesIntegersAsPythonsInt(t *testing.T) {
	cases := map[string]any{
		"0":                               n("-0"),
		"1000000000000000000000000000000": n("1000000000000000000000000000000"),
		"-12":                             int64(-12),
		"7":                               7,
		"false":                           false,
		"null":                            Null{},
	}
	for want, v := range cases {
		got, err := Encode(v, Compact)
		if err != nil || string(got) != want {
			t.Errorf("%v: got %s, %v, want %s", v, got, err, want)
		}
	}
}

// A sorted style sorts an unordered Object too; an insertion-order style cannot know an
// Object's order and refuses it.
func TestEncodeSortsAnObjectAndRefusesItUnsorted(t *testing.T) {
	got, err := Encode(Object{"b": n("1"), "a": Object{"y": Null{}, "x": n("2")}}, SortedBare)
	if want := `{"a": {"x": 2, "y": null}, "b": 1}`; err != nil || string(got) != want {
		t.Errorf("got %s, %v, want %s", got, err, want)
	}
	for _, style := range []Style{Compact, Spaced} {
		if _, err := Encode(Object{"a": n("1")}, style); !errors.Is(err, ErrNotEncodable) {
			t.Errorf("style %d: an Object in insertion order: %v", style, err)
		}
	}
}

// No state file holds a float, a value Python cannot write, or text that is not WTF-8.
func TestEncodeRefusesWhatNoStateFileHolds(t *testing.T) {
	for _, v := range []any{
		Number{"1.0", Float},
		Number{"NaN", Float},
		Number{"x", Int},
		1.5,
		nil,
		struct{}{},
		"\xff",
		[]any{"\xed\xa0"},
		Ordered{{"\xc0", n("1")}},
		Ordered{{"a", 1.5}},
		Ordered{{"a", n("1")}, {"a", n("2")}},
	} {
		if got, err := Encode(v, Compact); !errors.Is(err, ErrNotEncodable) {
			t.Errorf("%#v: got %s, %v", v, got, err)
		}
	}
	if _, err := Encode(n("1"), Style(0)); !errors.Is(err, ErrNotEncodable) {
		t.Errorf("an unknown style: %v", err)
	}
}

func TestEncodeRefusesNestingPastTheLimit(t *testing.T) {
	var v any = []any{}
	for range maxDepth - 1 {
		v = []any{v}
	}
	if _, err := Encode(v, Compact); err != nil {
		t.Errorf("nesting at the limit: %v", err)
	}
	if _, err := Encode(Ordered{{"a", v}}, Compact); !errors.Is(err, ErrNotEncodable) {
		t.Errorf("nesting past the limit: %v", err)
	}
}

// Options.Ordered keeps each object's keys in the order the text gives them, as Python's dict
// does, so a read-modify-write writes them back in that order.
func TestDecodeOrderedKeepsKeyOrder(t *testing.T) {
	text := `{"z": 1, "a": {"y": [], "b": {}}, "m": [{"k": 2, "c": 3}]}`
	got, err := Decode(text, Options{Ordered: true})
	if err != nil {
		t.Fatal(err)
	}
	out, err := Encode(got, Spaced)
	if err != nil || string(out) != text {
		t.Fatalf("got %s, %v", out, err)
	}
	obj, ok := got.(Ordered)
	if !ok {
		t.Fatalf("decoded %T", got)
	}
	if v, found := obj.Get("z"); !found || Canonical(v) != Canonical(n("1")) {
		t.Errorf("Get z: %v %v", v, found)
	}
	if _, found := obj.Get("q"); found {
		t.Error("Get of an absent key")
	}
	if keys := strings.Join(obj.Keys(), ","); keys != "z,a,m" {
		t.Errorf("Keys: %s", keys)
	}
}

// A repeated key keeps its first place and its last value, as a Python dict does; under
// NoDuplicateKeys it is refused.
func TestDecodeOrderedRepeatedKeyKeepsItsFirstPlace(t *testing.T) {
	got, err := Decode(`{"a": 1, "b": 2, "a": 3}`, Options{Ordered: true})
	if err != nil {
		t.Fatal(err)
	}
	if out, err := Encode(got, Compact); err != nil || string(out) != `{"a":3,"b":2}` {
		t.Errorf("got %s, %v", out, err)
	}
	if _, err := Decode(`{"a": 1, "a": 3}`, Options{Ordered: true, NoDuplicateKeys: true}); !errors.Is(err, ErrDuplicateKey) {
		t.Errorf("a repeated key under NoDuplicateKeys: %v", err)
	}
}

func TestCanonicalOfAnOrderedObjectIsItsSortedDump(t *testing.T) {
	a := Ordered{{"b", n("1")}, {"a", Null{}}}
	if Canonical(a) != Canonical(Object{"a": Null{}, "b": n("1")}) {
		t.Errorf("%s", Canonical(a))
	}
}

// CodePoints is Python's view of a decoded string: a lone surrogate is one code point.
func TestCodePointsCountsAsPythonsLen(t *testing.T) {
	if r, ok := CodePoints("a\xed\xa0\x80é😀"); !ok || len(r) != 4 || r[1] != 0xd800 {
		t.Errorf("%v %v", r, ok)
	}
	if _, ok := CodePoints("\xff"); ok {
		t.Error("text that is not WTF-8")
	}
}
