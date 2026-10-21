package pyjson

import (
	"errors"
	"math"
	"strings"
	"testing"
)

// esc is a JSON \u escape of the four hex digits, spelled so no editor turns it into the character.
func esc(hex string) string { return string([]byte{'\\', 'u'}) + hex }

// The cases below were checked against CPython 3.11's json.loads (SPEC-status-line.md S23).
func TestDecodeAcceptsWhatPythonAccepts(t *testing.T) {
	cases := map[string]any{
		`{"a": NaN, "b": Infinity, "c": -Infinity}`: Object{
			"a": Number{"NaN", Float}, "b": Number{"Infinity", Float}, "c": Number{"-Infinity", Float},
		},
		` 	` + "\n\r{}\r\n":                   Object{},
		`[1 ,2]`:                              []any{Number{"1", Int}, Number{"2", Int}},
		`"` + esc("d800") + `A"`:              "\xed\xa0\x80A",
		`"` + esc("d83d") + esc("de00") + `"`: "\xf0\x9f\x98\x80",
		`"` + esc("d800") + `"`:               "\xed\xa0\x80",
		`"` + esc("d800") + esc("0041") + `"`: "\xed\xa0\x80A",
		`"` + esc("dc00") + esc("d800") + `"`: "\xed\xb0\x80\xed\xa0\x80",
		`"` + esc("00e9") + `"`:               "\xc3\xa9",
		`"\/\"\\\b\f\n\r\t"`:                  "/\"\\\b\f\n\r\t",
		"\"\x7f\"":                            "\x7f",
		`{"a" : 1 , "a":2}`:                   Object{"a": Number{"2", Int}},
		`1e400`:                               Number{"1e400", Float},
		`-0.0`:                                Number{"-0.0", Float},
		`1E5`:                                 Number{"1E5", Float},
		`1.5e-3`:                              Number{"1.5e-3", Float},
		`[true, false, null]`:                 []any{true, false, Null{}},
		strings.Repeat("1", 4300):             Number{strings.Repeat("1", 4300), Int},
		"-" + strings.Repeat("1", 4300):       Number{"-" + strings.Repeat("1", 4300), Int},
	}
	for in, want := range cases {
		got, err := Decode(in, Options{})
		if err != nil {
			t.Errorf("%.40q: %v", in, err)
			continue
		}
		if Canonical(got) != Canonical(want) {
			t.Errorf("%.40q: got %s, want %s", in, Canonical(got), Canonical(want))
		}
	}
}

func TestDecodeRefusesWhatPythonRefuses(t *testing.T) {
	for _, in := range []string{
		`{} }`, `[1]]`, `01`, `1.`, `.5`, `+1`, `-`, `1e`, `1e+`, "\"a\x01\"", `"\x"`, `"\u12G4"`,
		`nan`, `-NaN`, ``, `  `, "\f{}", " {}", `[1,]`, `{"a":1,}`, strings.Repeat("1", 4301),
		`"` + esc("d800") + esc("ZZZZ") + `"`, `"` + esc("d800") + esc("") + `"`, `"` + esc("12") + `"`, `[01]`, `tru`, `truex`, `[true false]`, `{"a" 1}`, `{1:2}`,
		`"open`, `[`, `{"a":`, `"\`, "\xef\xbb\xbf{}", `{"a":1 "b":2}`, `[1 2]`, `-x`, `{"a"}`,
	} {
		if got, err := Decode(in, Options{}); err == nil {
			t.Errorf("%.40q: accepted as %s", in, Canonical(got))
		}
	}
}

// member.json refuses a duplicate key in any object (config._no_duplicate_keys).
func TestDecodeRefusesADuplicateKeyWhenAsked(t *testing.T) {
	for _, in := range []string{`{"a":1,"a":1}`, `{"x":[{"b":1,"b":2}]}`, `[{"k":{},"k":[]}]`} {
		if _, err := Decode(in, Options{NoDuplicateKeys: true}); !errors.Is(err, ErrDuplicateKey) {
			t.Errorf("%s: want ErrDuplicateKey, got %v", in, err)
		}
	}
	if _, err := Decode(`{"a":{"a":1},"b":[{"a":2}]}`, Options{NoDuplicateKeys: true}); err != nil {
		t.Errorf("the same key in different objects: %v", err)
	}
}

func TestDecodeRefusesNestingPastTheLimit(t *testing.T) {
	deep := strings.Repeat("[", maxDepth+1) + strings.Repeat("]", maxDepth+1)
	if _, err := Decode(deep, Options{}); err == nil {
		t.Fatal("accepted nesting past the limit")
	}
	ok := strings.Repeat("[", maxDepth) + strings.Repeat("]", maxDepth)
	if _, err := Decode(ok, Options{}); err != nil {
		t.Fatalf("refused nesting at the limit: %v", err)
	}
}

// json.loads(bytes) finds UTF-8, UTF-16 or UTF-32 from the bytes and decodes with surrogatepass.
func TestDecodeBytesDetectsTheEncodingAsPythonDoes(t *testing.T) {
	accepted := map[string]any{
		"\xff\xfe{\x00}\x00":                         Object{},
		"\x00{\x00}":                                 Object{},
		"{\x00\x00\x00}\x00\x00\x00":                 Object{},
		"\xef\xbb\xbf{}":                             Object{},
		"\"\xed\xa0\x80\"":                           "\xed\xa0\x80",
		"\xfe\xff\x00{\x00}":                         Object{},
		"\x00\x00\xfe\xff\x00\x00\x00{\x00\x00\x00}": Object{},
		"\xff\xfe\x00\x00{\x00\x00\x00}\x00\x00\x00": Object{},
		"\xff\xfe\"\x00\x00\xd8\"\x00":               "\xed\xa0\x80",
		"1":                                          Number{"1", Int},
		"12":                                         Number{"12", Int},
		"\x001":                                      Number{"1", Int},
		"1\x00":                                      Number{"1", Int},
		"\"\xed\xa0\x80\xed\xb0\x80\"":               "\xed\xa0\x80\xed\xb0\x80",
		"[\x00\x00\x00]\x00\x00\x00":                 []any{},
		"\x00\x00\x00[\x00\x00\x00]":                 []any{},
		"\"\xf0\x9f\x98\x80\"":                       "😀",
		"\xff\xfe\"\x00=\xd8\x00\xde\"\x00":          "😀",
	}
	for in, want := range accepted {
		got, err := DecodeBytes([]byte(in))
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if Canonical(got) != Canonical(want) {
			t.Errorf("%q: got %s, want %s", in, Canonical(got), Canonical(want))
		}
	}
	for _, in := range []string{
		"\xff\xfe{\x00}", "\"\xff\"", "\xef\xbb\xbf\xef\xbb\xbf{}", "\xfe\xff\x00{\x00}\x00", "\"\xed\xa0\"",
		"\"\xc0\x80\"", "\x00\x00\x00\xff\x00\x00\x00\xff", "\x00\x00\x00", "\xe2\x82",
	} {
		if got, err := DecodeBytes([]byte(in)); err == nil {
			t.Errorf("%q: accepted as %s", in, Canonical(got))
		}
	}
}

// member.json, the settings, the payload and the membership are decoded as UTF-8 first, which
// refuses an encoded surrogate.
func TestDecodeUTF8RefusesWhatPythonsUTF8DecoderRefuses(t *testing.T) {
	for _, in := range []string{"\"\xed\xa0\x80\"", "\"\xff\"", "\xff\xfe{\x00}\x00"} {
		if _, err := DecodeUTF8([]byte(in), Options{}); !errors.Is(err, ErrNotUTF8) {
			t.Errorf("%q: want ErrNotUTF8, got %v", in, err)
		}
	}
	got, err := DecodeUTF8([]byte(`{"k":"é"}`), Options{})
	if err != nil || Canonical(got) != Canonical(Object{"k": "é"}) {
		t.Fatalf("got %v, %v", got, err)
	}
}

// _list_of compares entries by json.dumps(x, sort_keys=True).
func TestCanonicalIsPythonsSortedDump(t *testing.T) {
	same := [][2]any{
		{Number{"1.0", Float}, Number{"1.00", Float}},
		{Number{"100.0", Float}, Number{"1e2", Float}},
		{Number{"-0", Int}, Number{"0", Int}},
		{Number{"NaN", Float}, Number{"NaN", Float}},
		{Number{"1e400", Float}, Number{"Infinity", Float}},
		{Object{"a": true, "b": nil}, Object{"b": nil, "a": true}},
	}
	for _, pair := range same {
		if Canonical(pair[0]) != Canonical(pair[1]) {
			t.Errorf("%v and %v differ: %s, %s", pair[0], pair[1], Canonical(pair[0]), Canonical(pair[1]))
		}
	}
	differ := [][2]any{
		{Number{"1", Int}, Number{"1.0", Float}},
		{Number{"-0.0", Float}, Number{"0.0", Float}},
		{true, Number{"1", Int}},
		{"1", Number{"1", Int}},
		{[]any{"a"}, "a"},
		{Object{"a": "b"}, []any{"a", "b"}},
		{"\xed\xa0\x80", "�"},
	}
	for _, pair := range differ {
		if Canonical(pair[0]) == Canonical(pair[1]) {
			t.Errorf("%v and %v are the same: %s", pair[0], pair[1], Canonical(pair[0]))
		}
	}
}

func TestNumberComparesAsPython(t *testing.T) {
	if !(Number{"1", Int}).IsInt(1) || (Number{"1.0", Float}).IsInt(1) || (Number{"2", Int}).IsInt(1) {
		t.Error("IsInt is `type(v) is int and v == want`")
	}
	if !(Number{"-0", Int}).IsInt(0) || (Number{strings.Repeat("9", 30), Int}).IsInt(1) {
		t.Error("IsInt compares the value, not the text")
	}
	for _, v := range []any{Number{"1", Int}, Number{"1.0", Float}, Number{"1e0", Float}, true} {
		if !EqualsInt(v, 1) {
			t.Errorf("%v == 1 holds in Python", v)
		}
	}
	for _, v := range []any{Number{"NaN", Float}, Number{"2", Int}, false, "1", nil, Number{"1.5", Float}} {
		if EqualsInt(v, 1) {
			t.Errorf("%v == 1 does not hold in Python", v)
		}
	}
	n, ok := (Number{"60", Int}).Int64()
	if !ok || n != 60 {
		t.Errorf("Int64: %d %v", n, ok)
	}
	if _, ok := (Number{strings.Repeat("9", 30), Int}).Int64(); ok {
		t.Error("Int64 of a value past int64")
	}
	if _, ok := (Number{"6.0", Float}).Int64(); ok {
		t.Error("Int64 of a float")
	}
	if f := (Number{"-Infinity", Float}).float(); !math.IsInf(f, -1) {
		t.Errorf("-Infinity is %v", f)
	}
}
