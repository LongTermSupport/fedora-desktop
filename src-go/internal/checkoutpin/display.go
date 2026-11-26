package checkoutpin

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Shown is checkout text as an approval screen prints it (Plan 00266 K6): every rune that is
// not printable (a control character, a format character such as a bidi override or a
// zero-width space, an unassigned or private-use one) is written as \u{XXXX}, and a byte that is
// not UTF-8 as \x{XX}, so the screen shows the text approved rather than what it would draw.
// Newline and tab are kept, so a file shows as its lines. Every line Command prints goes
// through it (say).
func Shown(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size <= 1:
			fmt.Fprintf(&b, `\x{%02X}`, s[i])
		case r == '\n' || r == '\t' || unicode.IsPrint(r):
			b.WriteString(s[i : i+size])
		default:
			fmt.Fprintf(&b, `\u{%04X}`, r)
		}
		i += size
	}
	return b.String()
}

// unshowable is whether a value holds anything Shown would escape, or a tab or newline: a
// value ccy or a person writes holds none.
func unshowable(s string) bool {
	return !utf8.ValidString(s) || strings.ContainsFunc(s, func(r rune) bool { return !unicode.IsPrint(r) })
}
