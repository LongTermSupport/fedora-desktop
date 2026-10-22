package proto

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// MaxBodyBytes caps a rendered `body:` string, quotes included (A 13.4, E8).
const MaxBodyBytes = 2048

// maxMarks is the combining marks shown on one base; more are escaped, so a stack cannot spill
// over the line above or below. Every real script's pointing fits.
const maxMarks = 4

const (
	zwj         = 0x200D
	emojiStyle  = 0xFE0F
	textStyle   = 0xFE0E
	keycapMark  = 0x20E3
	firstTone   = 0x1F3FB
	lastTone    = 0x1F3FF
	keycapBases = "0123456789#*"
)

// psAllowed are the blocks whose punctuation and symbols are shown (inclusive ranges): an
// allow-list, so a look-alike in a block nobody listed is escaped without being named.
var psAllowed = [][2]rune{
	{0x00A1, 0x00BF},
	{0x00D7, 0x00D7},
	{0x00F7, 0x00F7},
	{0x0370, 0x1FFF},
	{0x2010, 0x2027},
	{0x2030, 0x205E},
	{0x20A0, 0x20CF},
	{0x2100, 0x214F},
	{0x2190, 0x21FF},
	{0x2212, 0x2212},
	{0x221E, 0x221E},
	{0x2248, 0x2248},
	{0x2260, 0x2260},
	{0x2264, 0x2265},
	{0x231A, 0x231B},
	{0x2328, 0x2328},
	{0x23CF, 0x23CF},
	{0x23E9, 0x23F3},
	{0x23F8, 0x23FA},
	{0x2600, 0x27BF},
	{0x2B05, 0x2B07},
	{0x2B1B, 0x2B1C},
	{0x2B50, 0x2B50},
	{0x2B55, 0x2B55},
	{0x3001, 0x3007},
	{0x3012, 0x3013},
	{0x3020, 0x303F},
	{0xFF01, 0xFF60},
	{0xFFE0, 0xFFEE},
	{0x1F004, 0x1F004},
	{0x1F0CF, 0x1F0CF},
	{0x1F18E, 0x1F18E},
	{0x1F191, 0x1F19A},
	{0x1F1E6, 0x1F1FF},
	{0x1F300, 0x1F64F},
	{0x1F680, 0x1F6FF},
	{0x1F7E0, 0x1F7EB},
	{0x1F900, 0x1F9FF},
	{0x1FA70, 0x1FAFF},
}

// pictographBlocks are the blocks an emoji sequence joins.
var pictographBlocks = [][2]rune{
	{0x2600, 0x27BF},
	{0x2B05, 0x2B55},
	{0x1F004, 0x1F0CF},
	{0x1F300, 0x1F6FF},
	{0x1F7E0, 0x1F7EB},
	{0x1F900, 0x1F9FF},
	{0x1FA70, 0x1FAFF},
}

// escapedLookalikes are escaped although their category and block are shown: invisible fillers,
// and the look-alikes of the quote, the backslash and the markers' angle brackets that NFKC
// leaves alone (E2's list; the corpus pins it).
var escapedLookalikes = []rune{
	0x034F, 0x115F, 0x1160, 0x17B4, 0x17B5, 0x180B, 0x180C, 0x180D, 0x180F, 0x2800, 0x3164, 0xFFA0,
	0x00AB, 0x00BB, 0x02BA, 0x02C2, 0x02C3, 0x02DD, 0x02EE, 0x05F4, 0x1433, 0x1438, 0x201C, 0x201D,
	0x201E, 0x201F, 0x2033, 0x2036, 0x2039, 0x203A, 0x3003,
	0x169B, 0x169C, 0x0F3A, 0x0F3B, 0x0F3C, 0x0F3D, 0x201A, 0x201B, 0x140A, 0x1405,
	0x275B, 0x275C, 0x275D, 0x275E, 0x275F, 0x2760, 0x276C, 0x276D, 0x276E, 0x276F, 0x2770, 0x2771,
	0x27A2, 0x27A3, 0x27A4,
}

var (
	wordClasses  = []*unicode.RangeTable{unicode.L, unicode.N}
	psClasses    = []*unicode.RangeTable{unicode.P, unicode.S}
	escapedRunes = func() map[rune]bool {
		m := map[rune]bool{}
		for _, r := range escapedLookalikes {
			m[r] = true
		}
		for r := rune(0xFE00); r <= 0xFE0F; r++ {
			m[r] = true
		}
		for r := rune(0xE0100); r <= 0xE01EF; r++ {
			m[r] = true
		}
		return m
	}()
)

// EscapeJSON is A 13.4's one escaper: text as one JSON string showing printable ASCII (but the
// quote and the backslash) and the shown characters as themselves, and everything else as a
// JSON escape. Invalid UTF-8 is shown as an escaped U+FFFD.
func EscapeJSON(text string) string { return render([]rune(text), false) }

// EscapeASCII is the escaper in ASCII-only mode (the `question:` headline, A 13.2): every
// code point outside printable ASCII escaped.
func EscapeASCII(text string) string { return render([]rune(text), true) }

// WireBytes is the bytes text takes as the ASCII-escaped JSON string the session's socket
// carries, quotes included (E9).
func WireBytes(text string) int { return len(EscapeASCII(text)) }

// Body is a rendered `body:` string: the whole text, or its longest prefix of whole code
// points whose rendering fits MaxBodyBytes, never cut inside an escape. Shown and Total are
// UTF-8 bytes of the text shown and of the whole.
type Body struct {
	Rendered     string
	Shown, Total int
	Cut          bool
}

// CapBody renders text under the cap (E8). A longer prefix never renders shorter: a code
// point's rendering depends on those before it and, for a joiner or a keycap's selector only,
// on the next one, which can turn a 6-byte escape into a 3-byte character but adds at least 3.
func CapBody(text string) Body {
	runes := []rune(text)
	whole := render(runes, false)
	if len(whole) <= MaxBodyBytes {
		return Body{Rendered: whole, Shown: len(text), Total: len(text)}
	}
	low, high := 0, len(runes)
	for low < high {
		mid := (low + high + 1) / 2
		if len(render(runes[:mid], false)) <= MaxBodyBytes {
			low = mid
		} else {
			high = mid - 1
		}
	}
	return Body{Rendered: render(runes[:low], false), Shown: len(string(runes[:low])), Total: len(text), Cut: true}
}

// render is the escaper over code points. A shown code point carries the state the next one
// is judged by: whether it may take a mark (and how many marks are in a row), and whether it
// continues an emoji sequence a joiner may extend.
func render(runes []rune, asciiOnly bool) string {
	var b strings.Builder
	b.WriteByte('"')
	var previous rune = -1
	marks, inEmoji := 0, false
	for i, r := range runes {
		following := rune(-1)
		if i+1 < len(runes) {
			following = runes[i+1]
		}
		if !printableASCII(r) && (asciiOnly || r < utf8.RuneSelf || !shown(r, previous, marks) && !inSequence(r, previous, inEmoji, following)) {
			writeEscape(&b, r)
			previous, marks, inEmoji = -1, 0, false
			continue
		}
		b.WriteRune(r)
		if unicode.Is(unicode.M, r) {
			marks++
		} else {
			marks = 0
		}
		inEmoji = pictograph(r) || inEmoji && (r == emojiStyle || r >= firstTone && r <= lastTone)
		previous = r
	}
	b.WriteByte('"')
	return b.String()
}

func printableASCII(r rune) bool { return r >= ' ' && r <= '~' && r != '"' && r != '\\' }

// shown is whether a non-ASCII r is shown as itself after previous (the code point before it
// when that was shown, else -1) and marks combining marks in a row.
func shown(r, previous rune, marks int) bool {
	if r == textStyle || r == emojiStyle {
		return previous >= 0 && unicode.In(previous, unicode.So, unicode.Sk)
	}
	switch {
	case r == utf8.RuneError || escapedRunes[r]:
		return false
	case unicode.Is(unicode.M, r):
		return previous >= 0 && (previous > '~' || unicode.In(previous, wordClasses...)) && marks < maxMarks
	case unicode.In(r, psClasses...):
		if !inRanges(r, psAllowed) {
			return false
		}
	case !unicode.In(r, wordClasses...):
		return false
	}
	return foldsSafely(r)
}

// foldsSafely: r's NFKC form holds no quote, backslash or angle bracket and nothing that is
// not printable.
func foldsSafely(r rune) bool {
	for _, f := range norm.NFKC.String(string(r)) {
		if strings.ContainsRune(`"\<>`, f) || !unicode.IsPrint(f) {
			return false
		}
	}
	return true
}

// pictograph is a shown symbol of the pictograph blocks, which an emoji sequence may join.
func pictograph(r rune) bool {
	return r >= 0 && unicode.Is(unicode.So, r) && inRanges(r, pictographBlocks) && shown(r, -1, 0)
}

// inSequence: a joiner between a shown pictograph (with its selector or tone) and the next
// pictograph; an emoji selector on a keycap base right before the keycap mark.
func inSequence(r, previous rune, inEmoji bool, following rune) bool {
	if r == zwj {
		return inEmoji && pictograph(following)
	}
	return r == emojiStyle && previous >= 0 && strings.ContainsRune(keycapBases, previous) && following == keycapMark
}

func inRanges(r rune, ranges [][2]rune) bool {
	for _, span := range ranges {
		if r >= span[0] && r <= span[1] {
			return true
		}
	}
	return false
}

// writeEscape is JSON's escape of r: the short forms, else `\u` and four lower-case hex digits,
// an astral code point as its surrogate pair.
func writeEscape(b *strings.Builder, r rune) {
	short := map[rune]string{'"': `\"`, '\\': `\\`, '\n': `\n`, '\r': `\r`, '\t': `\t`, '\b': `\b`, '\f': `\f`}
	if s, ok := short[r]; ok {
		b.WriteString(s)
		return
	}
	units := []rune{r}
	if r > 0xFFFF {
		hi, lo := utf16.EncodeRune(r)
		units = []rune{hi, lo}
	}
	for _, u := range units {
		hex := strconv.FormatInt(int64(u), 16)
		b.WriteString(`\u` + strings.Repeat("0", 4-len(hex)) + hex)
	}
}
