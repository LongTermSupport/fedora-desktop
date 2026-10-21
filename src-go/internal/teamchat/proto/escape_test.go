package proto

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// pinnedUnicode is the one Unicode version the escaper is reviewed for (A 13.4). A Go or x/text
// upgrade that changes it fails here until the corpus is reviewed for the new version.
const pinnedUnicode = "15.0.0"

// ue is the JSON escape of each UTF-16 unit, built at run time so that no editor turns the
// escape in this file into the character itself.
func ue(units ...rune) string {
	var b strings.Builder
	for _, u := range units {
		b.WriteString("\\" + "u" + fmt.Sprintf("%04x", u))
	}
	return b.String()
}

// q is s between JSON quotes.
func q(s string) string { return `"` + s + `"` }

func TestUnicodeVersionIsPinned(t *testing.T) {
	if unicode.Version != pinnedUnicode || norm.Version != unicode.Version {
		t.Fatalf("unicode.Version %s, norm.Version %s; the escaper is reviewed for %s", unicode.Version, norm.Version, pinnedUnicode)
	}
}

// escapeCases are text and its rendering; the corpus carries them (escape-classes.json).
func escapeCases() map[string]string {
	zwjS, vs16, keycap := string(rune(0x200D)), string(rune(0xFE0F)), string(rune(0x20E3))
	acute, thumb, tone := string(rune(0x0301)), string(rune(0x1F44D)), string(rune(0x1F3FD))
	woman, girl, smile := string(rune(0x1F469)), string(rune(0x1F467)), string(rune(0x263A))
	shown := "caf" + string(rune(0xE9)) + " " + string(rune(0x65E5)) + string(rune(0x672C)) + " " + string(rune(0x2190))
	return map[string]string{
		"hello":                            q("hello"),
		`a"b\c`:                            q(`a\"b\\c`),
		"\n\r\t\b\f":                       q(`\n\r\t\b\f`),
		"\x00\x1b[201~":                    q(ue(0, 0x1b) + "[201~"),
		"\x7f":                             q(ue(0x7f)),
		shown:                              q(shown),
		string(rune(0x202E)):               q(ue(0x202e)),
		string(rune(0x2028)):               q(ue(0x2028)),
		string(rune(0x0085)):               q(ue(0x85)),
		string(rune(0x00A0)):               q(ue(0xa0)),
		string(rune(0xFF02)):               q(ue(0xff02)),
		string(rune(0x201C)):               q(ue(0x201c)),
		string(rune(0x10FFFF)):             q(ue(0xdbff, 0xdfff)),
		string(rune(0xE000)):               q(ue(0xe000)),
		"\xff":                             q(ue(0xfffd)),
		"e" + strings.Repeat(acute, 5):     q("e" + strings.Repeat(acute, 4) + ue(0x301)),
		"!" + acute:                        q("!" + ue(0x301)),
		thumb + tone:                       q(thumb + tone),
		woman + zwjS + woman + zwjS + girl: q(woman + zwjS + woman + zwjS + girl),
		"a" + zwjS + "b":                   q("a" + ue(0x200d) + "b"),
		thumb + zwjS:                       q(thumb + ue(0x200d)),
		"1" + vs16 + keycap:                q("1" + vs16 + keycap),
		"1" + vs16:                         q("1" + ue(0xfe0f)),
		smile + vs16:                       q(smile + vs16),
		"a" + vs16:                         q("a" + ue(0xfe0f)),
	}
}

func TestEscapeJSON(t *testing.T) {
	for text, want := range escapeCases() {
		if got := EscapeJSON(text); got != want {
			t.Errorf("EscapeJSON(%+q) = %s, want %s", text, got, want)
		}
	}
}

func TestEscapeASCII(t *testing.T) {
	grin := string(rune(0x1F600))
	cases := map[string]string{
		"Which licence?": q("Which licence?"),
		string(rune(0xE9)) + " \"x\"\nkind: HUMAN": q(ue(0xe9) + ` \"x\"\nkind: HUMAN`),
		grin:          q(ue(0xd83d, 0xde00)),
		"\x7f<<end>>": q(ue(0x7f) + "<<end>>"),
	}
	for text, want := range cases {
		if got := EscapeASCII(text); got != want {
			t.Errorf("EscapeASCII(%+q) = %s, want %s", text, got, want)
		}
	}
}

func TestLookalikesAreEscaped(t *testing.T) {
	for _, r := range escapedLookalikes {
		if got := EscapeJSON(string(r)); utf8.RuneCountInString(got) == 3 {
			t.Errorf("look-alike U+%04X is shown: %s", r, got)
		}
	}
}

func TestCapBody(t *testing.T) {
	if b := CapBody("short"); b.Rendered != q("short") || b.Cut || b.Total != 5 {
		t.Errorf("CapBody(short) = %+v", b)
	}
	b := CapBody(strings.Repeat("a", 3000))
	if !b.Cut || len(b.Rendered) != MaxBodyBytes || b.Shown != MaxBodyBytes-2 || b.Total != 3000 {
		t.Errorf("CapBody(3000 a) = shown %d of %d, %d bytes", b.Shown, b.Total, len(b.Rendered))
	}
}

func TestCapBodyNeverCutsAnEscape(t *testing.T) {
	b := CapBody(strings.Repeat("\x00", 1000))
	if !b.Cut || len(b.Rendered) > MaxBodyBytes || !strings.HasSuffix(b.Rendered, ue(0)+`"`) || b.Shown != (MaxBodyBytes-2)/6 {
		t.Errorf("CapBody(1000 NUL) = shown %d, %d bytes", b.Shown, len(b.Rendered))
	}
	cjk := string(rune(0x65E5))
	b = CapBody(strings.Repeat(cjk, 1000))
	if !b.Cut || len(b.Rendered) > MaxBodyBytes || b.Shown%3 != 0 || len(EscapeJSON(strings.Repeat(cjk, b.Shown/3+1))) <= MaxBodyBytes {
		t.Errorf("CapBody(1000 CJK) = shown %d, %d bytes: not the longest prefix", b.Shown, len(b.Rendered))
	}
}

func TestWireBytes(t *testing.T) {
	text := "a\n" + string(rune(0xE9)) + `"` + string(rune(0x1F600))
	if got, want := WireBytes(text), len(q(`a\n`+ue(0xe9)+`\"`+ue(0xd83d, 0xde00))); got != want {
		t.Errorf("WireBytes = %d, want %d", got, want)
	}
}

// rawRunes are the runes a rendered JSON string holds as themselves, between its quotes.
func rawRunes(rendered string) []rune {
	inner := rendered[1 : len(rendered)-1]
	var raw []rune
	for i := 0; i < len(inner); {
		if inner[i] == '\\' {
			i += 2
			if inner[i-1] == 'u' {
				i += 4
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(inner[i:])
		raw = append(raw, r)
		i += size
	}
	return raw
}

// safeRaw is what A 14 allows a body to hold as itself: printable ASCII but the quote and the
// backslash; else a letter, mark, number, punctuation or symbol (so no control, format,
// separator, private-use or unassigned code point) whose NFKC form holds no `"`, `\`, `<`,
// `>` or non-printable, and no listed look-alike. ZWJ is the one format character, and only
// between two emoji (zwjOK).
func safeRaw(r rune, zwjOK bool) bool {
	if r < utf8.RuneSelf {
		return r >= ' ' && r <= '~' && r != '"' && r != '\\'
	}
	if r == zwj {
		return zwjOK
	}
	if !unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.P, unicode.S) || slices.Contains(escapedLookalikes, r) {
		return false
	}
	folded := norm.NFKC.String(string(r))
	return !strings.ContainsAny(folded, "\"\\<>") && !strings.ContainsFunc(folded, func(f rune) bool { return !unicode.IsPrint(f) })
}

func checkRendered(t *testing.T, text string) {
	t.Helper()
	rendered := EscapeJSON(text)
	v, err := Parse([]byte(rendered))
	if err != nil || v.Kind() != KindString || v.Str() != text {
		t.Fatalf("EscapeJSON(%+q) = %s does not decode back: %v", text, rendered, err)
	}
	raw := rawRunes(rendered)
	for i, r := range raw {
		zwjOK := i > 0 && i+1 < len(raw) && unicode.Is(unicode.So, raw[i-1]) && unicode.Is(unicode.So, raw[i+1])
		if !safeRaw(r, zwjOK) {
			t.Fatalf("EscapeJSON(%+q) = %s shows U+%04X", text, rendered, r)
		}
	}
}

// TestEveryScalarValue is A 14's exhaustive property: every Unicode scalar value, alone and in
// the contexts that show more (after a letter, between emoji, on a keycap base), renders as a
// JSON string that decodes back to the text and shows nothing unsafe.
func TestEveryScalarValue(t *testing.T) {
	for plane := rune(0); plane <= unicode.MaxRune>>16; plane++ {
		t.Run(fmt.Sprintf("plane %d", plane), func(t *testing.T) {
			t.Parallel()
			checkPlane(t, plane<<16)
		})
	}
}

func checkPlane(t *testing.T, first rune) {
	t.Helper()
	thumb, keycap := string(rune(0x1F44D)), string(rune(0x20E3))
	for r := first; r <= first|0xFFFF; r++ {
		if r >= 0xD800 && r <= 0xDFFF {
			continue
		}
		s := string(r)
		checkRendered(t, s)
		checkRendered(t, "a"+s)
		if r > utf8.RuneSelf && !unicode.In(r, unicode.L, unicode.N) {
			checkRendered(t, thumb+s+thumb)
			checkRendered(t, "1"+s+keycap)
		}
	}
}
