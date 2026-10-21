package statusline

import (
	"regexp"
	"unicode"
)

// ansiEscape is a colour (SGR or CSI) escape, which takes no columns (S27).
var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")

// wideRanges are the code point ranges of East Asian Width W and F that a status line can
// show: the CJK blocks and the emoji blocks. GAP: this is a table of the common ranges, not
// unicodedata's full table, so a code point outside these ranges that Python measures as 2 is
// measured as 1 here (S27). The segment's own text (ASCII, team names and the bus label) is
// exact.
var wideRanges = [][2]rune{
	{0x1100, 0x115F}, {0x2E80, 0x303E}, {0x3041, 0x33FF}, {0x3400, 0x4DBF},
	{0x4E00, 0x9FFF}, {0xA000, 0xA4CF}, {0xAC00, 0xD7A3}, {0xF900, 0xFAFF},
	{0xFE30, 0xFE4F}, {0xFF00, 0xFF60}, {0xFFE0, 0xFFE6}, {0x1F300, 0x1F64F},
	{0x1F900, 0x1F9FF}, {0x20000, 0x3FFFD},
}

// displayWidth is the terminal columns text takes, by the hooks daemon's measure: colour
// escapes take none, wide code points two, combining and format code points none, and
// everything else one (S27).
func displayWidth(text string) int {
	width := 0
	for _, r := range ansiEscape.ReplaceAllString(text, "") {
		switch {
		case unicode.Is(unicode.Mn, r), unicode.Is(unicode.Me, r), unicode.Is(unicode.Cf, r):
			continue
		case isWide(r):
			width += 2
		default:
			width++
		}
	}
	return width
}

func isWide(r rune) bool {
	for _, span := range wideRanges {
		if r >= span[0] && r <= span[1] {
			return true
		}
	}
	return false
}
