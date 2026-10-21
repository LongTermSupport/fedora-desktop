package pycompat

import (
	"math"
	"math/big"
	"strconv"
	"testing"
)

func TestReprAndStrForms(t *testing.T) {
	d := NewDict()
	d.Set("repo", Str("x"))
	d.Set("n", List{IntOf(1), Bool(true), None{}, Float(2)})
	cases := []struct {
		v         Value
		str, repr string
	}{
		{None{}, "None", "None"},
		{Bool(false), "False", "False"},
		{Int{V: new(big.Int).Lsh(big.NewInt(1), 70)}, "1180591620717411303424", "1180591620717411303424"},
		{Float(1e16), "1e+16", "1e+16"},
		{Float(math.Inf(-1)), "-inf", "-inf"},
		{Float(math.NaN()), "nan", "nan"},
		{Str("it's"), "it's", `"it's"`},
		{Str(`a'b"c`), `a'b"c`, j(`'a^'b"c'`)},
		{Str("t\tn\n" + loneHigh), "t\tn\n" + loneHigh, j(`'t^tn^n^ud800'`)},
		{d, "{'repo': 'x', 'n': [1, True, None, 2.0]}", "{'repo': 'x', 'n': [1, True, None, 2.0]}"},
		{List{Str("a")}, "['a']", "['a']"},
		{List{}, "[]", "[]"},
		{NewDict(), "{}", "{}"},
	}
	for _, c := range cases {
		if got := PyStr(c.v); got != c.str {
			t.Errorf("str: %q, want %q", got, c.str)
		}
		if got := PyRepr(c.v); got != c.repr {
			t.Errorf("repr: %q, want %q", got, c.repr)
		}
	}
}

// splitmix64 is a fixed pseudo-random sequence: the same floats on every run.
func splitmix64(state *uint64) uint64 {
	*state += 0x9e3779b97f4a7c15
	z := *state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

var floatSpecials = []float64{
	0, 1, 2.5, 100, 1e15, 1e16, 1e17, 9999999999999998, 0.0001, 0.00001, 1e-7, 5e-324, 2.2250738585072014e-308,
	1.7976931348623157e308, 1e22, 1e23, 123456789, 0.1, 0.3, 1.0 / 3, 1234567890123456.7, 12345678901234567.0,
	-1.5, -1e-300, 4.35, 0.5e-4, 99999.99999,
}

func TestPythonFloatRepr(t *testing.T) {
	floats := append([]float64(nil), floatSpecials...)
	state := uint64(20261021)
	for len(floats) < 3000 {
		f := math.Float64frombits(splitmix64(&state))
		if math.IsNaN(f) || math.IsInf(f, 0) {
			continue
		}
		floats = append(floats, f)
	}
	for e := -330; e <= 310; e += 7 {
		floats = append(floats, math.Pow(10, float64(e)), 3*math.Pow(10, float64(e)))
	}
	reqs := make([]pyReq, 0, len(floats))
	for _, f := range floats {
		reqs = append(reqs, pyReq{"floatrepr", []any{strconv.FormatFloat(f, 'g', -1, 64)}})
	}
	out := python(t, reqs)
	for i, f := range floats {
		if !sameJSON(t, PyRepr(Float(f)), out[i].OK) {
			t.Errorf("repr(%v) = %s, python %s", f, PyRepr(Float(f)), out[i].OK)
		}
	}
}

var reprStrings = []string{
	"it's", `say ^"hi^"`, `both ' and ^"`, "^u0000^u0007^u001f^u007f^u0080^u00a0^u00ad^u2028^ue000^uffff",
	"^ud800", "^udb40^udc01", "\U0001f600", "^^", "tab^t", "^n^r", "^u0378", "^ud83f^udffe", "^udbff^udfff",
	"\U000000ff\U00000100", "^u00ff", "'", `^"`, `'^"`, "",
}

func TestPythonStrRepr(t *testing.T) {
	reqs := make([]pyReq, 0, 2*len(valueCases)+2*len(reprStrings)+len(textCases)+1)
	for _, c := range valueCases {
		reqs = append(reqs, pyReq{"str", []any{cps(c)}}, pyReq{"repr", []any{cps(c)}})
	}
	for _, s := range reprStrings {
		reqs = append(reqs, pyReq{"str", []any{cps(j(`"` + s + `"`))}}, pyReq{"repr", []any{cps(j(`"` + s + `"`))}})
	}
	for _, s := range textCases {
		reqs = append(reqs, pyReq{"reprstr", []any{cps(s)}})
	}
	reqs = append(reqs, pyReq{"repr", []any{cps(`["a", 1, null, true, 1.5, {"k": "v", "e": []}, [[]]]`)}})
	out := python(t, reqs)
	i := 0
	check := func(what, got string) {
		t.Helper()
		if !sameJSON(t, cps(got), out[i].OK) {
			t.Errorf("%s = %q, python %s", what, got, out[i].OK)
		}
		i++
	}
	for _, c := range valueCases {
		v := mustLoads(t, c)
		check("str("+c+")", PyStr(v))
		check("repr("+c+")", PyRepr(v))
	}
	for _, s := range reprStrings {
		v := mustLoads(t, j(`"`+s+`"`))
		check("str("+s+")", PyStr(v))
		check("repr("+s+")", PyRepr(v))
	}
	for _, s := range textCases {
		check("repr", PyRepr(Str(s)))
	}
	check("repr(list)", PyRepr(mustLoads(t, `["a", 1, null, true, 1.5, {"k": "v", "e": []}, [[]]]`)))
}
