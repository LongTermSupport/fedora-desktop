package readywait

import (
	"strings"
	"unicode/utf8"
)

// quoting says what the lexer is reading.
type quoting byte

const (
	inCode      quoting = 'C'
	inDouble    quoting = 'D' // "..."
	inSingle    quoting = 'S' // '...'
	inAnsiC     quoting = 'A' // $'...'
	wordBreaks          = " \t\n;&|()"
	hereString          = "<<<"
	hereDocMark         = "<<"
	substitute          = "$("
)

// level is one level of quoting; a command substitution is code again, counting its parens.
type level struct {
	kind   quoting
	parens int
}

// pendingHeredoc is a here-document whose body starts at the next newline.
type pendingHeredoc struct {
	stripTabs bool
	word      string
}

// lexer blanks what the rule must not read.
type lexer struct {
	text      string
	out       strings.Builder
	i         int
	stack     []level
	heredocs  []pendingHeredoc
	wordStart bool
}

// codeOnly is text with quoted text, comments and here-document bodies blanked. Every byte keeps
// its offset and every newline its place. A command substitution inside double quotes stays
// code, with its own quoting, as bash reads it.
func codeOnly(text string) string {
	l := &lexer{text: text, stack: []level{{kind: inCode}}, wordStart: true}
	l.out.Grow(len(text))
	for l.i < len(text) {
		l.step()
	}
	return l.out.String()
}

func (l *lexer) top() *level { return &l.stack[len(l.stack)-1] }

func (l *lexer) push(kind quoting, parens int) {
	l.stack = append(l.stack, level{kind: kind, parens: parens})
}

func (l *lexer) pop() { l.stack = l.stack[:len(l.stack)-1] }

// blank writes spaces for the text, keeping its newlines.
func (l *lexer) blank(chunk string) {
	for i := range len(chunk) {
		if chunk[i] == '\n' {
			l.out.WriteByte('\n')
		} else {
			l.out.WriteByte(' ')
		}
	}
}

// copyThrough writes n bytes as they are.
func (l *lexer) copyThrough(n int) {
	l.out.WriteString(l.text[l.i : l.i+n])
	l.i += n
}

// blankEscape blanks a backslash and the character it escapes (an invalid byte counting as one).
func (l *lexer) blankEscape() {
	stop := l.i + 1
	if stop < len(l.text) {
		_, size := utf8.DecodeRuneInString(l.text[stop:])
		stop += size
	}
	l.blank(l.text[l.i:stop])
	l.i = stop
}

// step reads one token.
func (l *lexer) step() {
	if l.text[l.i] == '\n' && len(l.heredocs) > 0 {
		l.heredocBodies()
		return
	}
	switch l.top().kind {
	case inSingle:
		l.single()
	case inDouble, inAnsiC:
		l.quoted()
	case inCode:
		l.code()
	}
}

// heredocBodies blanks the bodies of the here-documents opened on the line that ends here, each
// to its terminator.
func (l *lexer) heredocBodies() {
	n := len(l.text)
	l.out.WriteByte('\n')
	l.i++
	for _, h := range l.heredocs {
		for l.i < n {
			end := n
			if j := strings.IndexByte(l.text[l.i:], '\n'); j >= 0 {
				end = l.i + j
			}
			line := l.text[l.i:end]
			l.blank(l.text[l.i:min(end+1, n)])
			l.i = end + 1
			if h.stripTabs {
				line = strings.TrimLeft(line, "\t")
			}
			if line == h.word {
				break
			}
		}
	}
	l.heredocs = nil
	l.wordStart = true
}

func (l *lexer) single() {
	if l.text[l.i] == '\'' {
		l.pop()
		l.out.WriteByte('\'')
	} else {
		l.blank(l.text[l.i : l.i+1])
	}
	l.i++
}

// quoted reads inside "..." or $'...'.
func (l *lexer) quoted() {
	c := l.text[l.i]
	kind := l.top().kind
	switch {
	case c == '\\':
		l.blankEscape()
	case (kind == inDouble && c == '"') || (kind == inAnsiC && c == '\''):
		l.pop()
		l.out.WriteByte(c)
		l.i++
	case kind == inDouble && strings.HasPrefix(l.text[l.i:], substitute):
		l.push(inCode, 1)
		l.copyThrough(len(substitute))
		l.wordStart = true
	default:
		l.blank(l.text[l.i : l.i+1])
		l.i++
	}
}

// code reads unquoted text.
func (l *lexer) code() {
	c := l.text[l.i]
	rest := l.text[l.i:]
	switch {
	case c == '\\':
		l.blankEscape()
		l.wordStart = false
	case strings.HasPrefix(rest, "$'"):
		l.push(inAnsiC, 0)
		l.copyThrough(2)
	case c == '\'' || c == '"':
		kind := inDouble
		if c == '\'' {
			kind = inSingle
		}
		l.push(kind, 0)
		l.copyThrough(1)
	case c == '#' && l.wordStart:
		end := len(l.text)
		if j := strings.IndexByte(rest, '\n'); j >= 0 {
			end = l.i + j
		}
		l.blank(l.text[l.i:end])
		l.i = end
	case strings.HasPrefix(rest, substitute):
		l.push(inCode, 1)
		l.copyThrough(len(substitute))
		l.wordStart = true
	case strings.HasPrefix(rest, hereString):
		l.copyThrough(len(hereString))
		l.wordStart = false
	case strings.HasPrefix(rest, hereDocMark) && l.openHeredoc():
	default:
		l.plain(c)
	}
}

// openHeredoc notes a here-document introduced here, and says whether there was one.
func (l *lexer) openHeredoc() bool {
	m := heredocRe.FindStringSubmatch(l.text[l.i:])
	if m == nil || (m[2] != m[4] && m[2] != "") {
		return false
	}
	length := len(m[0])
	if m[2] != m[4] {
		length -= len(m[4])
	}
	l.heredocs = append(l.heredocs, pendingHeredoc{stripTabs: m[1] == "-", word: m[3]})
	l.copyThrough(length)
	l.wordStart = false
	return true
}

// plain copies a character of code, tracking the parentheses that end a substitution.
func (l *lexer) plain(c byte) {
	top := l.top()
	switch {
	case c == '(':
		top.parens++
	case c == ')' && top.parens > 0:
		top.parens--
		if top.parens == 0 && len(l.stack) > 1 {
			l.pop()
		}
	}
	l.out.WriteByte(c)
	l.wordStart = strings.IndexByte(wordBreaks, c) >= 0
	l.i++
}
