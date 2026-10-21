package pycompat

import (
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"unicode"
)

// Lone surrogates as WTF-8, the form Loads keeps them in.
const (
	loneHigh = "\xed\xa0\x80" // U+D800
	loneLow  = "\xed\xb0\x80" // U+DC00
)

// textCases are strings whose whitespace, line breaks and case python decides differently from Go.
var textCases = []string{
	"", " ", "a", "  a b  ", "\t\n\v\f\r x \x1c\x1d\x1e\x1f", "\U00000085a\U000000a0",
	"\U00001680\U00002000\U0000200a\U00002028\U00002029\U0000202f\U0000205f\U00003000",
	"\U0000200b zero width is not space \U0000200b", "\U0000180e", "\U0000feffbom", "a\r\nb\rc\nd", "a\r\r\nb",
	"\r\n", "\n", "\n\n", "x\n", "x\v\f\x1c\x1d\x1e\U00000085\U00002028\U00002029y", "x\x1fy", "tail\r", "a\n\r",
	"\U00000130stanbul", "\U00000391\U000003a3", "\U00000391\U000003a3 \U00000392\U000003a3", "\U000003a3",
	"\U00000391\U000003a3\U000003b1", "\U00000391.\U000003a3", "\U00000391\U000003a3.", "\U00000391'\U000003a3'",
	"\U00001e9e", "\U000001c5", "\U0000216b", "\U000024d0\U000024b6", "\U000003a3\U00000391\U000003a3\U00000301",
	"A\U000000ad\U000003a3", "OWNER/Repo", "\U000000dcn\U000000efc\U000000f6d\U000000e9", "\U0000fb00",
	"\U0001041c\U00010400", "\U0000ab70", "\U000013f8", loneHigh, "a" + loneLow + " ",
	" " + loneHigh + loneLow + " ", "\U00000663\U00000664", "\U000000b2", "x\U000000b2",
	"\U00000661\U00000662\U00000663", "\U0001d7d8\U0001d7d9", "\U00000130\U00000130",
}

func TestCodePointsAndLen(t *testing.T) {
	got := CodePoints("a" + loneHigh + "\U000000e9\U0001f600")
	want := []rune{'a', 0xD800, 0xE9, 0x1F600}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CodePoints = %U", got)
	}
	if n := Len("a" + loneHigh + "\U000000e9\U0001f600"); n != 4 {
		t.Fatalf("Len = %d", n)
	}
	if s := fromCPs([]rune{0xDFFF, 'x', 0xDBFF}); s != "\xed\xbf\xbfx\xed\xaf\xbf" {
		t.Fatalf("WTF-8 encoding %q", s)
	}
	// Bytes that are neither UTF-8 nor WTF-8 are one U+FFFD each, as Go decodes them.
	if got := CodePoints("\xff\xed\x80"); !reflect.DeepEqual(got, []rune{0xFFFD, 0xFFFD, 0xFFFD}) {
		t.Fatalf("invalid bytes %U", got)
	}
}

func TestStripFamily(t *testing.T) {
	s := "\U00003000\t a b \x1c\U00000085"
	if got := Strip(s); got != "a b" {
		t.Fatalf("Strip %q", got)
	}
	if got := LStrip(s); got != "a b \x1c\U00000085" {
		t.Fatalf("LStrip %q", got)
	}
	if got := RStrip(s); got != "\U00003000\t a b" {
		t.Fatalf("RStrip %q", got)
	}
	if Strip(" \U0000200b ") != "\U0000200b" {
		t.Fatal("U+200B is not Python whitespace")
	}
	if Strip("  ") != "" {
		t.Fatal("all whitespace strips to nothing")
	}
}

func TestSplitLines(t *testing.T) {
	cases := []struct {
		in        string
		bare, eol []string
	}{
		{"", nil, nil},
		{"\n", []string{""}, []string{"\n"}},
		{"a\r\nb\rc", []string{"a", "b", "c"}, []string{"a\r\n", "b\r", "c"}},
		{"a\x1cb\U00002029", []string{"a", "b"}, []string{"a\x1c", "b\U00002029"}},
		{"a\x1fb", []string{"a\x1fb"}, []string{"a\x1fb"}},
	}
	for _, c := range cases {
		if got := SplitLines(c.in, false); !reflect.DeepEqual(got, c.bare) {
			t.Errorf("SplitLines(%q, false) = %q", c.in, got)
		}
		if got := SplitLines(c.in, true); !reflect.DeepEqual(got, c.eol) {
			t.Errorf("SplitLines(%q, true) = %q", c.in, got)
		}
	}
}

func TestLower(t *testing.T) {
	const (
		capSigma   = "\U000003a3"
		alpha      = "\U000003b1"
		capAlpha   = "\U00000391"
		sigma      = "\U000003c3"
		finalSigma = "\U000003c2"
		acute      = "\U00000301"
	)
	cases := map[string]string{
		"\U00000130":                           "i\U00000307",
		capAlpha + capSigma:                    alpha + finalSigma,
		capSigma:                               sigma,
		capAlpha + capSigma + capAlpha:         alpha + sigma + alpha,
		capAlpha + "." + capSigma:              alpha + "." + finalSigma,
		capAlpha + capSigma + acute:            alpha + finalSigma + acute,
		capAlpha + capSigma + acute + capAlpha: alpha + sigma + acute + alpha,
		"OWNER/Repo":                           "owner/repo",
		loneHigh:                               loneHigh,
	}
	for in, want := range cases {
		if got := Lower(in); got != want {
			t.Errorf("Lower(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDigits(t *testing.T) {
	if !IsDigitNd(0x663) || IsDigitNd(0xB2) || IsDigitNd('x') {
		t.Fatal("IsDigitNd is Unicode Nd")
	}
	if v, ok := DigitValue(0x1D7D7); !ok || v != 9 {
		t.Fatalf("DigitValue(U+1D7D7) = %d %v", v, ok)
	}
	if _, ok := DigitValue(0xB2); ok {
		t.Fatal("a superscript has no decimal value")
	}
	if n, ok := ParseDigits("\U00000660\U000006642"); !ok || n.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("ParseDigits = %v %v", n, ok)
	}
	for _, bad := range []string{"", "1\U000000b2", "-1", " 1", "1_0", strings.Repeat("9", 4301)} {
		if _, ok := ParseDigits(bad); ok {
			t.Errorf("ParseDigits(%.20q) accepted", bad)
		}
	}
	if _, ok := ParseDigits(strings.Repeat("9", 4300)); !ok {
		t.Fatal("4300 digits is within python's limit")
	}
}

func TestPythonText(t *testing.T) {
	inputs := append([]string(nil), textCases...)
	inputs = append(inputs, strings.Repeat("\U00000669", 4300), strings.Repeat("\U00000669", 4301),
		"0"+strings.Repeat("1", 30))
	reqs := make([]pyReq, 0, len(inputs)*4)
	for _, s := range inputs {
		reqs = append(reqs,
			pyReq{"strip", []any{cps(s)}}, pyReq{"splitlines", []any{cps(s)}},
			pyReq{"lower", []any{cps(s)}}, pyReq{"digits", []any{cps(s)}})
	}
	out := python(t, reqs)
	for i, s := range inputs {
		strip, lines, lower, digits := out[4*i], out[4*i+1], out[4*i+2], out[4*i+3]
		if !sameJSON(t, []any{cps(Strip(s)), cps(LStrip(s)), cps(RStrip(s)), Len(s)}, strip.OK) {
			t.Errorf("strip family of %q: python %s", s, strip.OK)
		}
		if !sameJSON(t, []any{cpsList(SplitLines(s, false)), cpsList(SplitLines(s, true))}, lines.OK) {
			t.Errorf("splitlines of %q: python %s", s, lines.OK)
		}
		if !sameJSON(t, cps(Lower(s)), lower.OK) {
			t.Errorf("lower of %q = %q: python %s", s, Lower(s), lower.OK)
		}
		n, ok := ParseDigits(s)
		var want any
		if ok {
			want = n.String()
		}
		if digits.failed() {
			if ok {
				t.Errorf("int(%.20q): python %s, Go %v", s, pyErrText(digits), n)
			}
		} else if !sameJSON(t, want, digits.OK) {
			t.Errorf("int(%.20q): python %s, Go %v", s, digits.OK, want)
		}
	}
}

func cpsList(ss []string) [][]rune {
	out := make([][]rune, 0, len(ss))
	for _, s := range ss {
		out = append(out, cps(s))
	}
	return out
}

// unicodeTables is python's answer for every code point.
type unicodeTables struct {
	Space []rune `json:"space"`
	Lower []struct {
		CP rune   `json:"cp"`
		To []rune `json:"to"`
	} `json:"lower"`
	Decimal     [][2]rune `json:"decimal"`
	Cased       []rune    `json:"cased"`
	IgnOrCased  []rune    `json:"ignOrCased"`
	Unprintable [][2]rune `json:"unprintable"`
	Unassigned  [][2]rune `json:"unassigned"`
}

func inRanges(ranges [][2]rune) map[rune]bool {
	set := map[rune]bool{}
	for _, r := range ranges {
		for c := r[0]; c <= r[1]; c++ {
			set[c] = true
		}
	}
	return set
}

// goAssigned is whether Go's own tables (Unicode unicode.Version) assign a code point.
func goAssigned(r rune) bool {
	return unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.P, unicode.S, unicode.Z, unicode.C)
}

// TestPythonUnicodeTables compares every code point: isspace, isprintable (repr's escaping),
// lower, isdecimal and its value, and the two properties of the final-sigma rule. A code
// point unassigned in python's Unicode database or in Go's is the one allowed difference: the
// two are different Unicode editions.
func TestPythonUnicodeTables(t *testing.T) {
	out := python(t, []pyReq{{"tables", []any{}}})
	if out[0].failed() {
		t.Fatal(pyErrText(out[0]))
	}
	var tab unicodeTables
	if err := json.Unmarshal(out[0].OK, &tab); err != nil {
		t.Fatal(err)
	}
	py := newPyTables(tab)
	pyUnassigned := inRanges(tab.Unassigned)
	skipped, mismatches := 0, 0
	for r := rune(0); r <= unicode.MaxRune; r++ {
		why := py.disagreement(r)
		switch {
		case why == "":
		case pyUnassigned[r] || !goAssigned(r):
			skipped++
		default:
			if mismatches++; mismatches <= 20 {
				t.Errorf("U+%04X: %s", r, why)
			}
		}
	}
	t.Logf("PYCOMPARED %d code points; %d differ only where one Unicode edition leaves them unassigned",
		unicode.MaxRune+1, skipped)
}

// pyTables is python's per-code-point answers, indexed.
type pyTables struct {
	space, unprintable, cased, ignOrCased map[rune]bool
	lower                                 map[rune]string
	decimal                               map[rune]int
}

func newPyTables(tab unicodeTables) pyTables {
	py := pyTables{
		space: setOf(tab.Space), unprintable: inRanges(tab.Unprintable),
		cased: setOf(tab.Cased), ignOrCased: setOf(tab.IgnOrCased),
		lower: map[rune]string{}, decimal: map[rune]int{},
	}
	for _, row := range tab.Lower {
		py.lower[row.CP] = fromCPs(row.To)
	}
	for _, d := range tab.Decimal {
		py.decimal[d[0]] = int(d[1])
	}
	return py
}

// disagreement is what Go answers differently from python for r, or "".
func (py pyTables) disagreement(r rune) string {
	s := fromCPs([]rune{r})
	wantLower, has := py.lower[r]
	if !has {
		wantLower = s
	}
	wantDigit, isDec := py.decimal[r]
	gotDigit, gotDec := DigitValue(r)
	agree := IsSpace(r) == py.space[r] && isPrintable(r) == !py.unprintable[r] && Lower(s) == wantLower &&
		IsDigitNd(r) == isDec && gotDec == isDec && gotDigit == wantDigit &&
		(sigmaClass(r) == sigmaCased) == py.cased[r] && (sigmaClass(r) != sigmaNeither) == py.ignOrCased[r]
	if agree {
		return ""
	}
	return fmt.Sprintf("space %v/%v printable %v/%v lower %q/%q digit %v %d/%v %d sigma %d cased %v ign|cased %v",
		IsSpace(r), py.space[r], isPrintable(r), !py.unprintable[r], Lower(s), wantLower,
		gotDec, gotDigit, isDec, wantDigit, sigmaClass(r), py.cased[r], py.ignOrCased[r])
}

func setOf(rs []rune) map[rune]bool {
	set := make(map[rune]bool, len(rs))
	for _, r := range rs {
		set[r] = true
	}
	return set
}
