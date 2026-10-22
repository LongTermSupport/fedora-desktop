package pycompat

import (
	"errors"
	"strings"
	"testing"
)

// bs is one backslash: the cases below write it as a caret so a JSON escape reads plainly here.
const bs = "\x5c"

func j(s string) string { return strings.ReplaceAll(s, "^", bs) }

// loadsCases are documents the C scanner accepts or refuses in some particular way.
var loadsCases = []string{
	`null`, ` true `, `false`, "\t\n\r [ 1 , 2 ] \n", `[1,2]`, `{"a":1,"a":2}`, `{"a":1,"b":2,"a":3}`,
	`{"x":{"a":1,"a":2}}`, `"^u00e9"`, `"^ud83d^ude00"`, `"^ud800"`, `"^udc00^ud800"`, `"^ud800^ud800"`,
	`"^ud800x"`, `"^ud800^u0041"`, `"^"^^^/^b^f^n^r^t"`, `-0`, `-0.0`, `1e400`, `-1e400`, `1e-400`, `1E5`,
	`1e+5`, `2.5e-3`, `0.1`, `123456789012345678901234567890`, `[NaN, Infinity, -Infinity]`, "\"\x7f\"",
	"\"\U000000e9\U0001f600\U00002028\"", `{"": []}`, `[[[[]]]]`, `{"a": {"b": [1, {"c": null}]}}`,
	"1" + strings.Repeat("0", 4299), "1" + strings.Repeat("0", 4300), "-1" + strings.Repeat("0", 4299),
	"-1" + strings.Repeat("0", 4300), "1" + strings.Repeat("0", 4300) + ".5", "0" + strings.Repeat("0", 4400),
	``, ` `, `nul`, `nulls`, `tru`, `True`, `None`, `-`, `-I`, `-Inf`, `-NaN`, `+1`, `01`, `1.`, `.5`, `1e`,
	`1e+`, `1.5e`, `1.5e-`, `0x10`, `[1,]`, `{"a":1,}`, `{"a" 1}`, `{a:1}`, `{"a":}`, `{`, `[`, `[1 2]`,
	`{"a":1 "b":2}`, `"abc`, `"ab^`, `"^x"`, `"^u12"`, `"^u12G4"`, `"^ud800^u12G4"`, `"^ud800^"`,
	`"^ud800^u"`, `"^u00e9`, `"^u00e9"`, "\"a\tb\"", "\"a\nb\"", "\"a\x00\"", "\U0000feff1", " 1 x",
	`[] []`, `{"a":1}}`, "\f1", "\U000000a01", "1\n\n x", "\"x\"\n\"y\"", strings.Repeat("[", 2000),
	// 300 deep loads on both sides; deeper would overflow the oracle's own enc() recursion.
	strings.Repeat(`{"a":`, 2000), strings.Repeat("[", 300) + strings.Repeat("]", 300),
	`[1, {"a":1,"a":2}, ]`, `{"a":1,"a":2`, `{"a":1,"a":2} x`, `{"a":[1,2],"a":{"b":1,"b":2}}`,
	`[1, 2, 3, ]`, `{"a": 1, "b": }`, "[\"\U000000e9\", x]", "\"\U0001f600\" \U0001f600",
}

func TestPythonLoads(t *testing.T) {
	reqs := make([]pyReq, 0, 2*len(loadsCases))
	for _, c := range loadsCases {
		reqs = append(reqs, pyReq{"loads", []any{cps(j(c))}}, pyReq{"loads_nodup", []any{cps(j(c))}})
	}
	out := python(t, reqs)
	for i, c := range loadsCases {
		text := j(c)
		for k, opts := range []LoadOptions{{}, {NoDuplicateKeys: true}} {
			v, err := Loads(text, opts)
			var got any
			if err == nil {
				got = enc(v)
			}
			compareResult(t, "json.loads("+abbrev(text)+")", got, err, out[2*i+k])
		}
	}
}

func abbrev(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}

var rawCases = []string{
	`"ab" x`, `"a^u00e9b"rest`, "\"\U000000e9\"x", `""`, `"abc`, `"^q"`, `"a"  # c`, `"^ud800"tail`,
	`"^ud83d^ude00"!`, "\"\U0001f600\U0001f600\"\U0001f600",
}

func TestPythonRawDecode(t *testing.T) {
	reqs := make([]pyReq, 0, len(rawCases))
	for _, c := range rawCases {
		reqs = append(reqs, pyReq{"raw", []any{cps(j(c))}})
	}
	out := python(t, reqs)
	for i, c := range rawCases {
		text := j(c)
		s, end, err := RawDecodeString(text)
		var got any
		if err == nil {
			got = []any{enc(Str(s)), Len(text[:end])}
		}
		compareResult(t, "raw_decode("+text+")", got, err, out[i])
	}
}

func TestLoadsErrorClasses(t *testing.T) {
	_, err := Loads(`{"a":1,"a":2}`, LoadOptions{NoDuplicateKeys: true})
	var pe *Error
	if !errors.As(err, &pe) || pe.Class != ClassValueError || err.Error() != "duplicate key" || pe.Pos != -1 {
		t.Fatalf("duplicate: %#v", err)
	}
}

func TestLoadsDecodeErrorPosition(t *testing.T) {
	_, err := Loads("[1,\n 2,\n x]", LoadOptions{})
	var pe *Error
	if !errors.As(err, &pe) || pe.Class != ClassJSONDecodeError || pe.Msg != "Expecting value" ||
		pe.Pos != 9 || pe.Line != 3 || pe.Col != 2 {
		t.Fatalf("decode: %#v", err)
	}
	if err.Error() != "Expecting value: line 3 column 2 (char 9)" {
		t.Fatalf("message %q", err.Error())
	}
}

func TestLoadsBytesAndDepthClasses(t *testing.T) {
	_, err := Loads("\"a\xff\"", LoadOptions{})
	var pe *Error
	if !errors.As(err, &pe) || pe.Class != ClassUnicodeDecodeError {
		t.Fatalf("bytes: %#v", err)
	}
	_, err = Loads(strings.Repeat("[", maxDepth+1), LoadOptions{})
	if !errors.As(err, &pe) || pe.Class != ClassRecursionError {
		t.Fatalf("depth: %#v", err)
	}
	if v, err := Loads(strings.Repeat("[", maxDepth)+strings.Repeat("]", maxDepth), LoadOptions{}); err != nil || v == nil {
		t.Fatalf("depth %d is allowed: %v", maxDepth, err)
	}
}

func TestRawDecodeStringRefusals(t *testing.T) {
	for _, text := range []string{"", "1", " \"a\"", "\"\xff\""} {
		if _, _, err := RawDecodeString(text); err == nil {
			t.Errorf("RawDecodeString(%q) accepted", text)
		}
	}
	s, end, err := RawDecodeString(j(`"^ud800" x`))
	if err != nil || s != loneHigh || end != 8 {
		t.Fatalf("%q %d %v", s, end, err)
	}
}
