package proto

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// spaceChars is `\s` on Unicode scalar values: the White_Space property, as unicode.IsSpace
// (A 7.3). Go's own `\s` is ASCII only.
const (
	spaceChars = `\t\n\v\f\r \x{85}\p{Z}`
	whiteSpace = `[` + spaceChars + `]`
)

// secretPattern is one of v1's twelve secret shapes (protocol.py, P33). A leading `\b` is not
// in the expression: Go's `\b` is ASCII, so wordStart checks the boundary over Unicode word
// characters instead. Every other `\b` of v1's patterns is implied by what follows it or, for
// the AWS key, checked by wordEnd.
type secretPattern struct {
	name      string
	re        *regexp.Regexp
	wordStart bool
	wordEnd   bool
}

var secretPatterns = []secretPattern{
	{"private-key", regexp.MustCompile(`-{5}BEGIN[A-Z0-9 ]*PRIVATE KEY-{5}`), false, false},
	{"github-token", regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{30,}`), true, false},
	{"github-pat", regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`), true, false},
	{"aws-access-key-id", regexp.MustCompile(`(?:AKIA|ASIA)[0-9A-Z]{16}`), true, true},
	{"slack-token", regexp.MustCompile(`xox[abposr]-[A-Za-z0-9-]{10,}`), true, false},
	{"sk-api-key", regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`), true, false},
	{"google-api-key", regexp.MustCompile(`AIza[0-9A-Za-z_-]{35}`), true, false},
	{"jwt", regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`), true, false},
	{"bearer", regexp.MustCompile(`(?i)bearer` + whiteSpace + `+[A-Za-z0-9._~+/=-]{16,}`), true, false},
	{"ansible-vault", regexp.MustCompile(`\$ANSIBLE_VAULT;`), false, false},
	{"url-credentials", regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^` + spaceChars + `/:@]+:[^` + spaceChars + `/@]+@`), false, false},
	{"credential-assignment", regexp.MustCompile(`(?i)(?:password|passwd|pwd|secret|token|access[_-]?token|api[_-]?key|access[_-]?key` +
		`|private[_-]?key|client[_-]?secret)["']?` + whiteSpace + `*[:=]` + whiteSpace + `*["']?[^` + spaceChars + `"']{8,}`), true, false},
}

// isWord is Python's `\w` over str: a letter, a number or `_`.
func isWord(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r) }

// SecretShape is the name of the first secret shape text holds, or "own-token" when it holds
// token (R43); false when it holds neither. A sender refuses such text `secret` (A 7.3);
// receivers do not run it.
func SecretShape(text, token string) (string, bool) {
	if token != "" && strings.Contains(text, token) {
		return "own-token", true
	}
	for _, p := range secretPatterns {
		if p.matches(text) {
			return p.name, true
		}
	}
	return "", false
}

// matches finds a match whose boundaries hold, retrying one character on from a match whose
// boundary fails, so a refused match does not hide a later one.
func (p secretPattern) matches(text string) bool {
	for from := 0; from <= len(text); {
		loc := p.re.FindStringIndex(text[from:])
		if loc == nil {
			return false
		}
		start, end := from+loc[0], from+loc[1]
		if (!p.wordStart || !isWord(lastRune(text[:start]))) && (!p.wordEnd || !isWord(firstRune(text[end:]))) {
			return true
		}
		from = start + utf8.RuneLen(firstRune(text[start:]))
	}
	return false
}

// lastRune is the last character of s; 0, not a word character, for an empty s.
func lastRune(s string) rune {
	r, size := utf8.DecodeLastRuneInString(s)
	if size == 0 {
		return 0
	}
	return r
}

// firstRune is the first character of s; 0 for an empty s.
func firstRune(s string) rune {
	r, size := utf8.DecodeRuneInString(s)
	if size == 0 {
		return 0
	}
	return r
}
