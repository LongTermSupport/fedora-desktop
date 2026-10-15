package wsi

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Whisper's non-speech marks, each within one line: "(music)", "[BLANK_AUDIO]".
var (
	parenMark   = regexp.MustCompile(`\([^)\n]*\)`)
	bracketMark = regexp.MustCompile(`\[[^]\n]*\]`)
)

// previewLength is how many characters of the text a notification shows (W28).
const previewLength = 60

// cleanTranscript is W22: the marks removed line by line, leading newlines and white space
// removed, the first letter upper-cased.
func cleanTranscript(raw string) string {
	lines := strings.Split(strings.TrimRight(raw, "\n"), "\n")
	for i, line := range lines {
		lines[i] = bracketMark.ReplaceAllString(parenMark.ReplaceAllString(line, ""), "")
	}
	text := strings.TrimRight(strings.Join(lines, "\n"), "\n")
	text = strings.TrimPrefix(strings.TrimPrefix(text, "\n"), "\n")
	text = strings.TrimLeftFunc(text, unicode.IsSpace)
	first, size := utf8.DecodeRuneInString(text)
	if size == 0 {
		return ""
	}
	return string(unicode.ToUpper(first)) + text[size:]
}

// preview is the first 60 characters, "..." added when there were more.
func preview(text string) string {
	runes := []rune(text)
	if len(runes) <= previewLength {
		return text
	}
	return string(runes[:previewLength]) + "..."
}
