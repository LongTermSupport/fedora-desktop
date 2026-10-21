package hooks

import (
	"errors"
	"math"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// H6: the hook's answer is Python's json.dumps(result, ensure_ascii=True): ", " and ": ",
// insertion order, the two-character escapes, \u with lowercase hex for the rest, surrogate
// pairs above U+FFFF, a lone surrogate as itself, "/" not escaped.
func TestDumpIsPythonsDefaultJSONDumps(t *testing.T) {
	lone, err := pyjson.Decode(`"\ud800"`, pyjson.Options{})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		value any
		want  string
	}{
		{pyobj{}, `{}`},
		{pyobj{{"b", 1}, {"a", "x"}}, `{"b": 1, "a": "x"}`},
		{"a\"b\\c\n\r\t\b\f/", `"a\"b\\c\n\r\t\b\f/"`},
		{"\x00\x1f\x7f é", `"\u0000\u001f\u007f \u00e9"`},
		{"👥", `"\ud83d\udc65"`},
		{lone, `"\ud800"`},
		{[]any{true, false, nil, pyjson.Null{}}, `[true, false, null, null]`},
		{[]string{"a", "b"}, `["a", "b"]`},
		{pyobj{{"hookSpecificOutput", pyobj{{"hookEventName", "Stop"}}}}, `{"hookSpecificOutput": {"hookEventName": "Stop"}}`},
		{int64(-7), `-7`},
		{pyjson.Number{Lit: "-0", Kind: pyjson.Int}, `0`},
		{pyjson.Number{Lit: "1.0", Kind: pyjson.Float}, `1.0`},
		{pyjson.Object{"z": 1, "a": 2}, `{"a": 2, "z": 1}`},
	}
	for _, c := range cases {
		got, err := pyDump(c.value, styleDefault)
		if err != nil || got != c.want {
			t.Errorf("pyDump(%#v) = %q, %v; want %q", c.value, got, err, c.want)
		}
	}
}

// The other forms the hooks write: compact (write_json_file, H88), sorted with default
// separators (the membership writers, H41, H83), and compact without ensure_ascii (the
// validator's content_size, which then encodes UTF-8: a lone surrogate cannot be).
func TestDumpStyles(t *testing.T) {
	value := pyobj{{"v", 1}, {"b", []any{"é", 2}}, {"a", nil}}
	for style, want := range map[dumpStyle]string{
		styleCompact: `{"v":1,"b":["\u00e9",2],"a":null}`,
		styleSorted:  `{"a": null, "b": ["\u00e9", 2], "v": 1}`,
		styleUnicode: `{"v":1,"b":["é",2],"a":null}`,
	} {
		got, err := pyDump(value, style)
		if err != nil || got != want {
			t.Errorf("style %+v: %q, %v; want %q", style, got, err, want)
		}
	}
	lone, err := pyjson.Decode(`"\udfff"`, pyjson.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pyDump([]any{lone}, styleUnicode); !errors.Is(err, errNotEncodable) {
		t.Errorf("a lone surrogate without ensure_ascii: %v, want errNotEncodable", err)
	}
	if _, err := pyDump(struct{}{}, styleDefault); err == nil {
		t.Error("a value JSON cannot hold must be an error")
	}
}

// Python's float repr: the shortest digits, fixed notation for a decimal exponent from -5
// to 15, else d.ddde+XX; NaN and the infinities as json.dumps writes them.
func TestPyFloatRepr(t *testing.T) {
	cases := map[float64]string{
		1:          "1.0",
		-2.5:       "-2.5",
		0:          "0.0",
		1e15:       "1000000000000000.0",
		1e16:       "1e+16",
		1.5e16:     "1.5e+16",
		0.0001:     "0.0001",
		0.00001:    "1e-05",
		1.25e-7:    "1.25e-07",
		1e100:      "1e+100",
		0.1:        "0.1",
		123456.789: "123456.789",
		math.Inf(1): "Infinity",
		math.Inf(-1): "-Infinity",
		math.NaN():  "NaN",
	}
	for f, want := range cases {
		if got := pyFloatRepr(f); got != want {
			t.Errorf("pyFloatRepr(%v) = %q, want %q", f, got, want)
		}
	}
	if got := pyFloatRepr(math.Copysign(0, -1)); got != "-0.0" {
		t.Errorf("negative zero: %q", got)
	}
	for lit, want := range map[string]string{"1E400": "Infinity", "-1e400": "-Infinity", "NaN": "NaN", "2.50": "2.5"} {
		got, err := pyDump(pyjson.Number{Lit: lit, Kind: pyjson.Float}, styleDefault)
		if err != nil || got != want {
			t.Errorf("float %s: %q, %v; want %q", lit, got, err, want)
		}
	}
}
