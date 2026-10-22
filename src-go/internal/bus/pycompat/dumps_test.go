package pycompat

import (
	"errors"
	"testing"
)

var dumpsCases = []string{
	`null`, `true`, `false`, `[1, 2.5, "x"]`, `{"a": {"b": []}}`, `"^u0000^u001f^u007f^u0080"`,
	"\"\U000000e9\U0001f600\U00002028\"", `"^ud800"`, `"a^ud800b"`, `"^udfff^ud800"`, `"^udbff^udfff"`,
	`[NaN, Infinity, -Infinity, -0.0, 1e16, 1e-5, 123.456, 1e22, 5e-324, 1.7976931348623157e308]`,
	`"^"^^^/^b^f^n^r^t"`, `{}`, `[]`, "{\"\U000000e9\": \"\U000000e9\"}", `123456789012345678901234567890`,
	`[[], {}, [{}], {"": ""}]`, `-0`, `{"b": 1, "a": 2, "b": 3}`, `[1e15, 1e17, 0.0001, 0.00001]`,
}

func TestPythonDumps(t *testing.T) {
	reqs := make([]pyReq, 0, 2*len(dumpsCases))
	for _, c := range dumpsCases {
		reqs = append(reqs, pyReq{"dumps", []any{cps(j(c)), true}}, pyReq{"dumps", []any{cps(j(c)), false}})
	}
	out := python(t, reqs)
	for i, c := range dumpsCases {
		v := mustLoads(t, j(c))
		for k, ascii := range []bool{true, false} {
			s, err := Dumps(v, ascii)
			var got any
			if err == nil {
				got = cps(s)
			}
			compareResult(t, "json.dumps("+j(c)+")", got, err, out[2*i+k])
		}
	}
}

func TestDumpsRefusesALoneSurrogateOnlyUnescaped(t *testing.T) {
	v := List{Str("a"), Str("b" + loneHigh + loneLow)}
	if s, err := Dumps(v, true); err != nil || s != j(`["a", "b^ud800^udc00"]`) {
		t.Fatalf("ascii: %q %v", s, err)
	}
	_, err := Dumps(v, false)
	var pe *Error
	if !errors.As(err, &pe) || pe.Class != ClassUnicodeEncodeError ||
		err.Error() != j("'utf-8' codec can't encode characters in position 8-9: surrogates not allowed") {
		t.Fatalf("unescaped: %v", err)
	}
}
