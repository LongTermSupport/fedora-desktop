package proto

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// pySpaceClass is the body of a class matching Python's \s on a str (str.isspace).
const pySpaceClass = `\t-\r\x1c- \x{85}\x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}`

// secretPattern is one §7 shape, spelled for RE2 (SPEC P33). Python's \b is checked by hand:
// lead before the first segment, between segments, and after the last when tail is set.
// fold marks a (?i) pattern, matched against the text with dotless i and dotted capital I
// read as "i", as Python's IGNORECASE folds them and RE2's does not.
type secretPattern struct {
	name     string
	lead     bool
	tail     bool
	fold     bool
	segments []*regexp.Regexp
}

func anchored(pattern string) *regexp.Regexp { return regexp.MustCompile(`^(?:` + pattern + `)`) }

func one(re *regexp.Regexp) []*regexp.Regexp { return []*regexp.Regexp{re} }

var secretPatterns = []secretPattern{
	{name: "private-key", segments: one(regexp.MustCompile(`-{5}BEGIN[A-Z0-9 ]*PRIVATE KEY-{5}`))},
	{name: "github-token", lead: true, segments: one(anchored(`gh[pousr]_[A-Za-z0-9]{30,}`))},
	{name: "github-pat", lead: true, segments: one(anchored(`github_pat_[A-Za-z0-9_]{20,}`))},
	{name: "aws-access-key-id", lead: true, tail: true, segments: one(anchored(`(?:AKIA|ASIA)[0-9A-Z]{16}`))},
	{name: "slack-token", lead: true, segments: one(anchored(`xox[abposr]-[A-Za-z0-9-]{10,}`))},
	{name: "sk-api-key", lead: true, segments: one(anchored(`sk-[A-Za-z0-9_-]{20,}`))},
	{name: "google-api-key", lead: true, segments: one(anchored(`AIza[0-9A-Za-z_-]{35}`))},
	{
		name: "jwt", lead: true,
		segments: one(anchored(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`)),
	},
	{
		name: "bearer", lead: true, fold: true,
		segments: one(anchored(`(?i)bearer[` + pySpaceClass + `]+[A-Za-z0-9._~+/=-]{16,}`)),
	},
	{name: "ansible-vault", segments: one(regexp.MustCompile(`\$ANSIBLE_VAULT;`))},
	{
		name: "url-credentials",
		segments: one(regexp.MustCompile(
			`[A-Za-z][A-Za-z0-9+.-]*://[^` + pySpaceClass + `/:@]+:[^` + pySpaceClass + `/@]+@`)),
	},
	{
		name: "credential-assignment", lead: true, fold: true,
		segments: []*regexp.Regexp{
			anchored(`(?i)(?:password|passwd|pwd|secret|token|access[_-]?token|api[_-]?key|access[_-]?key` +
				`|private[_-]?key|client[_-]?secret)`),
			anchored(`(?i)["']?[` + pySpaceClass + `]*[:=][` + pySpaceClass + `]*["']?[^` + pySpaceClass + `"']{8,}`),
		},
	},
}

var foldDottedI = strings.NewReplacer("\U00000131", "i", "\U00000130", "i")

// SecretShape is SPEC P33: the name of the first §7 secret shape found anywhere in the text,
// with Python re.search semantics, or "".
func SecretShape(text string) string {
	folded := foldDottedI.Replace(text)
	for _, p := range secretPatterns {
		s := text
		if p.fold {
			s = folded
		}
		if p.found(s) {
			return p.name
		}
	}
	return ""
}

func (p secretPattern) found(s string) bool {
	if !p.lead {
		return p.segments[0].MatchString(s)
	}
	for start := 0; start < len(s); start += runeSize(s[start:]) {
		if wordBoundary(s, start) && p.matchesAt(s, start) {
			return true
		}
	}
	return false
}

// matchesAt runs the segments one after another from start, a boundary between each.
func (p secretPattern) matchesAt(s string, start int) bool {
	at := start
	for i, seg := range p.segments {
		if i > 0 && !wordBoundary(s, at) {
			return false
		}
		loc := seg.FindStringIndex(s[at:])
		if loc == nil {
			return false
		}
		at += loc[1]
	}
	return !p.tail || wordBoundary(s, at)
}

func runeSize(s string) int {
	if r, size := utf8.DecodeRuneInString(s); r != utf8.RuneError || size > 0 {
		return size
	}
	return 1
}

// wordBoundary is Python's \b at a byte offset: a word character on exactly one side. A byte
// that is not UTF-8 (a lone surrogate's) decodes as U+FFFD, no word character, as in Python.
func wordBoundary(s string, at int) bool {
	before, after := false, false
	if at > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:at])
		before = size > 0 && pyWord(r)
	}
	if at < len(s) {
		r, size := utf8.DecodeRuneInString(s[at:])
		after = size > 0 && pyWord(r)
	}
	return before != after
}

// pyWord is Python's \w on a str: a letter, a number or "_". A code point Unicode 15.0
// added is none, as in the container's Python, whose tables are 14.0 (SPEC P84, D5).
func pyWord(r rune) bool {
	if r == '_' {
		return true
	}
	return (unicode.IsLetter(r) || unicode.IsNumber(r)) && !NewInUnicode15(r)
}
