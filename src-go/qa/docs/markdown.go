package docs

import (
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// link is one link of a document: the line its text starts on and its destination as the parser
// gives it.
type link struct {
	line   int
	target string
}

// tableRow is one body row of a table whose first cell is exactly one code span: the line and
// the span's text.
type tableRow struct {
	line int
	name string
}

// document is what the gate needs from one Markdown file, read once with a CommonMark parser
// (and the GitHub table extension).
type document struct {
	links    []link
	headings map[string]bool
	rows     []tableRow
}

// parser is the one Markdown parser; it holds no state between documents.
var parser = goldmark.New(goldmark.WithExtensions(extension.Table)).Parser()

// parseDocument reads a document: its links, the anchors its headings define, and its table rows.
func parseDocument(src []byte) document {
	doc := parser.Parse(text.NewReader(src))
	lines := lineStarts(src)
	out := document{headings: map[string]bool{}}
	// A walk's visitor never fails, so the walk's error is always nil.
	walkAll(doc, func(n ast.Node) {
		switch v := n.(type) {
		case *ast.Link:
			if len(v.Destination) > 0 {
				out.links = append(out.links, link{line: lineOf(lines, firstOffset(v)), target: string(v.Destination)})
			}
		case *ast.Heading:
			if slug := slugOf(plainText(v, src)); slug != "" {
				out.headings[uniqueSlug(out.headings, slug)] = true
			}
		case *extast.TableRow:
			if row, ok := rowOf(v, src, lines); ok {
				out.rows = append(out.rows, row)
			}
		}
	})
	return out
}

// walkAll calls visit for every node of the tree in document order.
func walkAll(root ast.Node, visit func(ast.Node)) {
	for n := root; n != nil; {
		visit(n)
		switch {
		case n.FirstChild() != nil:
			n = n.FirstChild()
		default:
			for n != nil && n != root && n.NextSibling() == nil {
				n = n.Parent()
			}
			if n == nil || n == root {
				return
			}
			n = n.NextSibling()
		}
	}
}

// uniqueSlug is the slug a heading gets: the second and later headings with the same slug are
// numbered -1, -2, ... the way GitHub numbers them.
func uniqueSlug(taken map[string]bool, slug string) string {
	if !taken[slug] {
		return slug
	}
	for n := 1; ; n++ {
		if candidate := slug + "-" + strconv.Itoa(n); !taken[candidate] {
			return candidate
		}
	}
}

// rowOf reads a body row: it is a gate row when the first cell holds only one code span.
func rowOf(row *extast.TableRow, src []byte, lines []int) (tableRow, bool) {
	cell := row.FirstChild()
	if cell == nil || cell.ChildCount() != 1 {
		return tableRow{}, false
	}
	span, ok := cell.FirstChild().(*ast.CodeSpan)
	if !ok {
		return tableRow{}, false
	}
	name := strings.TrimSpace(rawText(span, src))
	if !gateName.MatchString(name) {
		return tableRow{}, false
	}
	return tableRow{line: lineOf(lines, firstOffset(span)), name: name}, true
}

// gateName is what a documented gate's name looks like: a file name or a dotted module path.
var gateName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// rawText is the text of a node's descendants exactly as written.
func rawText(n ast.Node, src []byte) string {
	var b strings.Builder
	walkAll(n, func(c ast.Node) {
		if t, ok := c.(*ast.Text); ok {
			b.Write(t.Segment.Value(src))
		}
	})
	return b.String()
}

// plainText is the text content of an inline tree as a reader sees it: code spans and link text
// kept, emphasis marks, images and raw HTML dropped, escapes and entities resolved, a line break a space.
func plainText(n ast.Node, src []byte) string {
	var b strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch v := c.(type) {
		case *ast.Text:
			b.WriteString(unescape(v.Segment.Value(src)))
			if v.SoftLineBreak() || v.HardLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.String:
			b.Write(v.Value)
		case *ast.CodeSpan:
			b.WriteString(rawText(v, src))
		case *ast.Image, *ast.RawHTML:
		default:
			b.WriteString(plainText(c, src))
		}
	}
	return b.String()
}

// unescape resolves backslash escapes of ASCII punctuation and character references in one
// pass, so an escaped ampersand stays an ampersand and is not read as the start of a reference.
func unescape(b []byte) string {
	s := string(b)
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s) && isPunct(s[i+1]):
			i++
			out.WriteByte(s[i])
		case s[i] == '&':
			end := strings.IndexByte(s[i:], ';')
			if end > 0 && end <= maxReference {
				if decoded := html.UnescapeString(s[i : i+end+1]); decoded != s[i:i+end+1] {
					out.WriteString(decoded)
					i += end
					continue
				}
			}
			out.WriteByte(s[i])
		default:
			out.WriteByte(s[i])
		}
	}
	return out.String()
}

// maxReference bounds how far past an ampersand a character reference's semicolon can be.
const maxReference = 32

func isPunct(c byte) bool { return strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", c) >= 0 }

// firstOffset is the byte offset the node's text starts at: its first text descendant, else the
// first line of the block holding it.
func firstOffset(n ast.Node) int {
	offset := -1
	walkAll(n, func(c ast.Node) {
		if t, ok := c.(*ast.Text); ok && offset < 0 {
			offset = t.Segment.Start
		}
	})
	if offset >= 0 {
		return offset
	}
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Type() == ast.TypeBlock && p.Lines() != nil && p.Lines().Len() > 0 {
			return p.Lines().At(0).Start
		}
	}
	return 0
}

// lineStarts are the byte offsets at which each line begins.
func lineStarts(src []byte) []int {
	starts := []int{0}
	for i, c := range src {
		if c == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// lineOf is the 1-based line holding the offset.
func lineOf(starts []int, offset int) int {
	return sort.Search(len(starts), func(i int) bool { return starts[i] > offset })
}
