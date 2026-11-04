package proto

import (
	"strings"
	"testing"
)

func TestRenderBodyShowsAndEscapes(t *testing.T) {
	cases := map[string]string{
		"":                                    `""`,
		"a \"b\" \\ <c>":                      `"a \"b\" \\ <c>"`,
		"\x00\x7f\n":                          `"` + esc("0000") + esc("007f") + `\n"`,
		"\U000000e9\U0001f600":                "\"\U000000e9\U0001f600\"",
		"\U0000201c\U0000ff02\U0000ff01":      `"` + esc("201c") + esc("ff02") + "\U0000ff01\"",
		"\xed\xa0\x80":                        `"` + esc("d800") + `"`,
		"\U000e0001":                          `"` + esc("db40") + esc("dc01") + `"`,
		"a" + strings.Repeat("\U00000301", 5): "\"a" + strings.Repeat("\U00000301", 4) + esc("0301") + `"`,
		"1\U0000fe0f\U000020e3":               "\"1\U0000fe0f\U000020e3\"",
		"\U0001f469\U0000200d\U0001f4bb":      "\"\U0001f469\U0000200d\U0001f4bb\"",
		"\U0001f469\U0000200d":                "\"\U0001f469" + esc("200d") + `"`,
		"\U0001e4d0":                          `"` + esc("d839") + esc("dcd0") + `"`,
	}
	for in, want := range cases {
		if got := RenderBody(in); got != want {
			t.Errorf("RenderBody(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBodyEscapedSetHoldsItsRanges(t *testing.T) {
	for _, r := range []rune{0x275b, 0x2760, 0x276c, 0x2771, 0x27a2, 0x27a4, 0xfe00, 0xfe0d, 0xe0100, 0xe01ef, 0x3164} {
		if !bodyEscaped(r) {
			t.Errorf("%U is not in the escaped set", r)
		}
	}
	for _, r := range []rune{0x275a, 0x2761, 0x2772, 0x27a5, 0xfe10, 0xe01f0, 'a'} {
		if bodyEscaped(r) {
			t.Errorf("%U is in the escaped set", r)
		}
	}
}

func TestCapBodyKeepsTheLongestPrefixThatFits(t *testing.T) {
	whole := CapBody(strings.Repeat("a", 2046))
	if whole.Cut || whole.Shown != 2046 || whole.Total != 2046 || len(whole.Rendered) != 2048 {
		t.Fatalf("whole: %+v", whole)
	}
	cut := CapBody(strings.Repeat("\U000000e9", 1024))
	if !cut.Cut || cut.Shown != 2046 || cut.Total != 2048 || len(cut.Rendered) != 2048 {
		t.Fatalf("cut: %+v", cut)
	}
	surrogates := CapBody(strings.Repeat("\xed\xa0\x80", 342))
	if !surrogates.Cut || surrogates.Shown != 1023 || surrogates.Total != 1026 {
		t.Fatalf("lone surrogates: %+v", surrogates)
	}
}

func TestWireBytesIsTheASCIIJSONLength(t *testing.T) {
	for in, want := range map[string]int{"": 2, "\U000000e9": 8, "\U0001f600": 14, "\xed\xa0\x80": 8, "\"": 4} {
		if got := WireBytes(in); got != want {
			t.Errorf("WireBytes(%q) = %d, want %d", in, got, want)
		}
	}
}
