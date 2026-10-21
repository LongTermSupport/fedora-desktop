package statusline

import (
	"regexp"
	"sort"
)

// ansiEscape is a colour (SGR or CSI) escape, which takes no columns (S27).
var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")

// span is an inclusive range of code points.
type span struct{ lo, hi rune }

// displayWidth is the terminal columns text takes, by the hooks daemon's measure as Python's
// unicodedata gives it: colour escapes take none; a code point with a canonical combining class
// other than 0, or of category Cf, none; one of East Asian Width W or F, two; any other, one
// (S27). The tables in widthtable.go are generated from the oracle's Python, unassigned code
// points included (it measures them as F).
func displayWidth(text string) int {
	width := 0
	for _, r := range ansiEscape.ReplaceAllString(text, "") {
		switch {
		case inSpans(zeroSpans, r):
			continue
		case inSpans(wideSpans, r):
			width += 2
		default:
			width++
		}
	}
	return width
}

// inSpans is whether r falls in one of spans, which are sorted and disjoint.
func inSpans(spans []span, r rune) bool {
	i := sort.Search(len(spans), func(i int) bool { return spans[i].hi >= r })
	return i < len(spans) && spans[i].lo <= r
}
