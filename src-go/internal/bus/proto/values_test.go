package proto

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// esc is a JSON \u escape of the four hex digits, spelled so no editor turns it into the character.
func esc(hex string) string { return string([]byte{'\\', 'u'}) + hex }

func TestPyLowerIsPythonsFullLowercase(t *testing.T) {
	cases := map[string]string{
		"ABC":                        "abc",
		"\U00000130":                 "i\U00000307",
		"A\U000003a3":                "a\U000003c2",
		"A\U000003a3.":               "a\U000003c2.",
		"\U000003a3":                 "\U000003c3",
		"A\U000003a3B":               "a\U000003c3b",
		"A\U000003a3'B":              "a\U000003c3'b",
		"\U0000212a":                 "k",
		"a\xed\xa0\x80B":             "a\xed\xa0\x80b",
		"\U000000c9\U00010400":       "\U000000e9\U00010428",
		"A\U00000301\U000003a3 next": "a\U00000301\U000003c2 next",
	}
	for in, want := range cases {
		if got := pyLower(in); got != want {
			t.Errorf("pyLower(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPyStripIsStrIsspace(t *testing.T) {
	cases := map[string]string{
		" \t\n\v\f\r\x1c\x1d\x1e\x1fa\U00000085\U000000a0\U00001680\U00002000\U0000200a\U00002028" +
			"\U00002029\U0000202f\U0000205f\U00003000": "a",
		"\U0000200ba\U0000200b": "\U0000200ba\U0000200b",
		"\U0000180ea":           "\U0000180ea",
		"":                      "",
		"  ":                    "",
	}
	for in, want := range cases {
		if got := pyStrip(in); got != want {
			t.Errorf("pyStrip(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLengthsCountCodePoints(t *testing.T) {
	if n := charLen("a\xed\xa0\x80\U0001f600"); n != 3 {
		t.Fatalf("charLen = %d, want 3", n)
	}
	if n, ok := utf8Size("a\U000000e9"); !ok || n != 3 {
		t.Fatalf("utf8Size = %d %v", n, ok)
	}
	if _, ok := utf8Size("a\xed\xa0\x80"); ok {
		t.Fatal("a lone surrogate has no UTF-8 size")
	}
}

func TestDumpsMatchesPythonJSON(t *testing.T) {
	cases := []struct {
		value any
		ascii bool
		want  string
	}{
		{"\U000000e9\x7f\x01\"\\\n", false, "\"\U000000e9\x7f" + esc("0001") + `\"\\\n"`},
		{
			"\U000000e9\x7f\U0001f600\xed\xa0\x80", true,
			`"` + esc("00e9") + esc("007f") + esc("d83d") + esc("de00") + esc("d800") + `"`,
		},
		{[]any{pyjson.Null{}, true, pyjson.Number{Lit: "-0", Kind: pyjson.Int}}, true, "[null,true,0]"},
		{
			pyjson.Object{"b": pyjson.Number{Lit: "1e400", Kind: pyjson.Float}, "a": []any{}},
			true,
			`{"a":[],"b":Infinity}`,
		},
	}
	for _, c := range cases {
		if got := dumps(c.value, c.ascii); got != c.want {
			t.Errorf("dumps(%#v, %v) = %q, want %q", c.value, c.ascii, got, c.want)
		}
	}
}

func TestFloatReprIsPythons(t *testing.T) {
	cases := map[string]string{
		"1.0": "1.0", "1e16": "1e+16", "1e15": "1000000000000000.0", "1.5e-05": "1.5e-05",
		"0.0001": "0.0001", "-0.0": "-0.0", "0.1": "0.1", "1e100": "1e+100", "123.456": "123.456",
		"1.23e-7": "1.23e-07", "NaN": "NaN", "Infinity": "Infinity", "-Infinity": "-Infinity",
		"-1e400": "-Infinity", "12345678901234567890.0": "1.2345678901234567e+19",
		"5e-324": "5e-324", "0.000123": "0.000123",
	}
	for lit, want := range cases {
		if got := floatRepr(lit); got != want {
			t.Errorf("floatRepr(%q) = %q, want %q", lit, got, want)
		}
	}
}
