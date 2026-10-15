package docs

import (
	"strings"
	"unicode"
)

// slugOf is the anchor GitHub gives a heading with this text content: lower-cased, every
// character deleted except letters, marks, numbers, connector punctuation (the underscore), spaces and the
// hyphen, and each white-space character becoming its own hyphen. Runs are not collapsed: the
// deleted dash of "A — B" leaves both spaces, so it is "a--b".
func slugOf(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(heading)) {
		switch {
		case unicode.IsSpace(r):
			b.WriteByte('-')
		case r == '-', unicode.IsLetter(r), unicode.IsNumber(r), unicode.IsMark(r), unicode.Is(unicode.Pc, r):
			b.WriteRune(r)
		}
	}
	return b.String()
}
