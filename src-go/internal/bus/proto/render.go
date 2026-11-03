package proto

import (
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// BodyMaxBytes is SPEC P75: the rendered body, quotes included, is at most this many UTF-8 bytes.
const BodyMaxBytes = 2048

// bodyMarksMax is how many combining marks in a row one shown base carries (P74).
const bodyMarksMax = 4

const (
	bodyZWJ    = 0x200d
	bodyVS15   = 0xfe0e
	bodyVS16   = 0xfe0f
	bodyKeycap = 0x20e3
)

// bodyPSAllowed are the blocks whose punctuation and symbols are shown (inclusive ranges).
var bodyPSAllowed = [][2]rune{
	{0x00a1, 0x00bf},
	{0x00d7, 0x00d7},
	{0x00f7, 0x00f7},
	{0x0370, 0x1fff},
	{0x2010, 0x2027},
	{0x2030, 0x205e},
	{0x20a0, 0x20cf},
	{0x2100, 0x214f},
	{0x2190, 0x21ff},
	{0x2212, 0x2212},
	{0x221e, 0x221e},
	{0x2248, 0x2248},
	{0x2260, 0x2260},
	{0x2264, 0x2265},
	{0x231a, 0x231b},
	{0x2328, 0x2328},
	{0x23cf, 0x23cf},
	{0x23e9, 0x23f3},
	{0x23f8, 0x23fa},
	{0x2600, 0x27bf},
	{0x2b05, 0x2b07},
	{0x2b1b, 0x2b1c},
	{0x2b50, 0x2b50},
	{0x2b55, 0x2b55},
	{0x3001, 0x3007},
	{0x3012, 0x3013},
	{0x3020, 0x303f},
	{0xff01, 0xff60},
	{0xffe0, 0xffee},
	{0x1f004, 0x1f004},
	{0x1f0cf, 0x1f0cf},
	{0x1f18e, 0x1f18e},
	{0x1f191, 0x1f19a},
	{0x1f1e6, 0x1f1ff},
	{0x1f300, 0x1f64f},
	{0x1f680, 0x1f6ff},
	{0x1f7e0, 0x1f7eb},
	{0x1f900, 0x1f9ff},
	{0x1fa70, 0x1faff},
}

// bodyEscapedPoints and bodyEscapedRanges are escaped although their category or block is
// shown: fillers, joiners, variation selectors, and quote, backslash and angle look-alikes.
var (
	bodyEscapedPoints = []rune{
		0x034f, 0x115f, 0x1160, 0x17b4, 0x17b5, 0x180b, 0x180c, 0x180d, 0x180f, 0x2800, 0x3164, 0xffa0,
		0x00ab, 0x00bb, 0x02ba, 0x02c2, 0x02c3, 0x02dd, 0x02ee, 0x05f4, 0x1433, 0x1438, 0x201c, 0x201d,
		0x201e, 0x201f, 0x2033, 0x2036, 0x2039, 0x203a, 0x3003, 0x169b, 0x169c, 0x0f3a, 0x0f3b, 0x0f3c,
		0x0f3d, 0x201a, 0x201b, 0x140a, 0x1405,
	}
	bodyEscapedRanges = [][2]rune{
		{0x275b, 0x2760}, {0x276c, 0x2771}, {0x27a2, 0x27a4}, {0xfe00, 0xfe0f}, {0xe0100, 0xe01ef},
	}
	// bodyNFKCStructural is every code point the category and block rules would show whose
	// NFKC form holds a quote, backslash, angle bracket or non-printable character: for
	// Unicode 14 these four (P74, P84; the Python test side recomputes them).
	bodyNFKCStructural = []rune{0xff02, 0xff1c, 0xff1e, 0xff3c}
	// bodyPictographBlocks are the blocks whose So symbols an emoji sequence joins.
	bodyPictographBlocks = [][2]rune{
		{0x2600, 0x27bf},
		{0x2b05, 0x2b55},
		{0x1f004, 0x1f0cf},
		{0x1f300, 0x1f6ff},
		{0x1f7e0, 0x1f7eb},
		{0x1f900, 0x1f9ff},
		{0x1fa70, 0x1faff},
	}
)

func inRanges(r rune, ranges [][2]rune) bool {
	for _, rg := range ranges {
		if rg[0] <= r && r <= rg[1] {
			return true
		}
	}
	return false
}

func bodyEscaped(r rune) bool {
	return slices.Contains(bodyEscapedPoints, r) || inRanges(r, bodyEscapedRanges)
}

// bodyIn is the category test with Unicode 15.0's additions read as unassigned (P84).
func bodyIn(r rune, tables ...*unicode.RangeTable) bool {
	return !NewInUnicode15(r) && unicode.In(r, tables...)
}

func asciiAlnum(r rune) bool {
	return r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z'
}

// bodyState is what the render of a code point depends on: the code point before it, when
// that was shown as itself, the marks shown on it in a row, and an emoji sequence in progress.
type bodyState struct {
	prev    rune
	hasPrev bool
	marks   int
	inEmoji bool
}

// shown is envelope._shown: whether a non-ASCII code point is shown as itself.
func (s *bodyState) shown(r rune) bool {
	if r == bodyVS15 || r == bodyVS16 {
		return s.hasPrev && bodyIn(s.prev, unicode.So, unicode.Sk)
	}
	if bodyEscaped(r) {
		return false
	}
	if bodyIn(r, unicode.M) {
		return s.hasPrev && (asciiAlnum(s.prev) || s.prev > '~') && s.marks < bodyMarksMax
	}
	switch {
	case bodyIn(r, unicode.P, unicode.S):
		if !inRanges(r, bodyPSAllowed) {
			return false
		}
	case !bodyIn(r, unicode.L, unicode.N):
		return false
	}
	return !slices.Contains(bodyNFKCStructural, r)
}

// bodyPictograph is a shown So symbol of the pictograph blocks.
func bodyPictograph(r rune) bool {
	return bodyIn(r, unicode.So) && inRanges(r, bodyPictographBlocks) && (&bodyState{}).shown(r)
}

// inSequence is a joiner between a shown pictograph run and a pictograph, or U+FE0F on a
// keycap base right before the keycap mark.
func (s *bodyState) inSequence(r, next rune, hasNext bool) bool {
	if r == bodyZWJ {
		return s.inEmoji && hasNext && bodyPictograph(next)
	}
	return r == bodyVS16 && s.hasPrev && strings.ContainsRune("0123456789#*", s.prev) && hasNext && next == bodyKeycap
}

func (s *bodyState) advance(r rune) {
	if bodyIn(r, unicode.M) {
		s.marks++
	} else {
		s.marks = 0
	}
	skinTone := r >= 0x1f3fb && r <= 0x1f3ff
	s.inEmoji = bodyPictograph(r) || s.inEmoji && (r == bodyVS16 || skinTone)
	s.prev, s.hasPrev = r, true
}

func renderPoints(rs []rune) string {
	var b strings.Builder
	b.WriteByte('"')
	var s bodyState
	for i, r := range rs {
		hasNext := i+1 < len(rs)
		var next rune
		if hasNext {
			next = rs[i+1]
		}
		asciiShown := r >= ' ' && r <= '~' && r != '"' && r != '\\'
		if asciiShown || r > '~' && (s.shown(r) || s.inSequence(r, next, hasNext)) {
			b.WriteRune(r)
			s.advance(r)
			continue
		}
		escaped := dumps(pyjson.FromCodePoints([]rune{r}), true)
		b.WriteString(escaped[1 : len(escaped)-1])
		s = bodyState{}
	}
	b.WriteByte('"')
	return b.String()
}

// RenderBody is SPEC P74: the text as one JSON string, printable ASCII (but the quote and the
// backslash) and the shown code points as themselves, every other code point as the escape
// json.dumps(ch, ensure_ascii=True) writes.
func RenderBody(text string) string { return renderPoints(points(text)) }

// CappedBody is a body's render cut to BodyMaxBytes (SPEC P75): Shown and Total are the UTF-8
// bytes (WTF-8 for a lone surrogate) of the text shown and of the whole text.
type CappedBody struct {
	Rendered     string
	Shown, Total int
	Cut          bool
}

// CapBody is SPEC P75: the render of the whole text when it fits, else of its longest
// code-point prefix whose render fits. A longer prefix never renders shorter.
func CapBody(text string) CappedBody {
	rs := points(text)
	total := len(pyjson.FromCodePoints(rs))
	rendered := renderPoints(rs)
	if len(rendered) <= BodyMaxBytes {
		return CappedBody{Rendered: rendered, Shown: total, Total: total}
	}
	n := sort.Search(len(rs)+1, func(n int) bool { return len(renderPoints(rs[:n])) > BodyMaxBytes }) - 1
	return CappedBody{
		Rendered: renderPoints(rs[:n]), Shown: len(pyjson.FromCodePoints(rs[:n])), Total: total, Cut: true,
	}
}

// WireBytes is SPEC P76: the length of json.dumps(text, ensure_ascii=True).
func WireBytes(text string) int { return len(dumps(text, true)) }
