// Package richtext draws the markdown an agent writes for a person as the HTML Matrix carries
// beside it (`format` org.matrix.custom.html, `formatted_body`; Plan 00241 Phase 3). The plain
// `body` stays the markdown; this is only what Element draws.
//
// The markdown is parsed as CommonMark with GitHub's tables (goldmark), and the HTML is written
// here, node by node, never by goldmark's own renderer: every node this package does not draw
// is shown as its text, so the output holds only the tags below.
//
//   - R1 paragraphs as `<p>`; a line break inside one is kept as `<br>`.
//   - R2 headings as `<h1>` to `<h6>`; a thematic break as `<hr>`.
//   - R3 `-`, `*` and `1.` lists as `<ul>` and `<ol>` (`start` when it is not 1), nested.
//   - R4 emphasis as `<em>`, strong emphasis as `<strong>`.
//   - R5 code spans as `<code>`; fenced and indented code as `<pre><code>`, with
//     `class="language-<name>"` when the fence names a language of letters, digits and `+-_.#`.
//   - R6 block quotes as `<blockquote>`.
//   - R7 links, autolinks and images as `<a href>`: an image is a link to it named by its
//     alt text, because Matrix's `<img>` takes only `mxc://` sources.
//   - R8 a link goes only to an `http` or `https` address with a host, or to `mailto`; any
//     other destination (`javascript:`, `data:`, a relative path) draws its text alone.
//   - R9 GitHub tables as `<table>` with `<thead>` and `<tbody>`. A column's alignment is not
//     drawn: Matrix's recommended subset gives `<th>` and `<td>` no attribute for it.
//   - R10 HTML in the text, inline or a block, is shown as its text, escaped; entities and
//     backslash escapes resolve as CommonMark says, and the result is escaped again.
//   - R11 the output holds only tags of Matrix's recommended subset (the client-server
//     specification's `m.room.message` formatting): p, br, h1-h6, hr, ul, ol, li, em, strong,
//     code, pre, blockquote, a, table, thead, tbody, tr, th, td; and only the attributes
//     `href`, `start` and `class`.
//   - R12 empty text draws as "".
package richtext

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	goldhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Format is the `format` a formatted message carries.
const Format = "org.matrix.custom.html"

var markdown = goldmark.New(goldmark.WithExtensions(extension.Table)).Parser()

// safeSchemes are the link schemes R8 allows, and whether each needs a host.
var safeSchemes = map[string]bool{"http": true, "https": true, "mailto": false}

// HTML is markdown drawn as Matrix's HTML (R1-R12).
func HTML(markdownText string) string {
	source := []byte(markdownText)
	r := renderer{source: source}
	r.children(markdown.Parse(text.NewReader(source), parser.WithContext(parser.NewContext())))
	return r.out.String()
}

type renderer struct {
	source []byte
	out    builder
}

// put writes markup this package made; strings.Builder never fails a write.
func (r *renderer) put(s string) { r.out.sb.WriteString(s) }

func (r *renderer) children(n ast.Node) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		r.node(c)
	}
}

// wrap draws n's children between an opening and a closing tag.
func (r *renderer) wrap(open, closing string, n ast.Node) {
	r.put(open)
	r.children(n)
	r.put(closing)
}

func (r *renderer) node(n ast.Node) {
	if r.block(n) || r.table(n) {
		return
	}
	r.inline(n)
}

func (r *renderer) block(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.Paragraph:
		r.wrap("<p>", "</p>", n)
	case *ast.TextBlock:
		r.children(n)
	case *ast.Heading:
		level := strconv.Itoa(n.Level)
		r.wrap("<h"+level+">", "</h"+level+">", n)
	case *ast.ThematicBreak:
		r.put("<hr>")
	case *ast.Blockquote:
		r.wrap("<blockquote>", "</blockquote>", n)
	case *ast.List:
		r.list(n)
	case *ast.ListItem:
		r.wrap("<li>", "</li>", n)
	case *ast.FencedCodeBlock:
		r.code(n, language(n.Language(r.source)))
	case *ast.CodeBlock:
		r.code(n, "")
	case *ast.HTMLBlock:
		r.htmlBlock(n)
	default:
		return false
	}
	return true
}

func (r *renderer) list(n *ast.List) {
	switch {
	case !n.IsOrdered():
		r.wrap("<ul>", "</ul>", n)
	case n.Start != 1:
		r.wrap(`<ol start="`+strconv.Itoa(n.Start)+`">`, "</ol>", n)
	default:
		r.wrap("<ol>", "</ol>", n)
	}
}

func (r *renderer) code(n ast.Node, lang string) {
	r.put("<pre><code")
	if lang != "" {
		r.put(` class="language-` + lang + `"`)
	}
	r.put(">")
	lines := n.Lines()
	for i := range lines.Len() {
		at := lines.At(i)
		goldhtml.DefaultWriter.RawWrite(&r.out, at.Value(r.source))
	}
	r.put("</code></pre>")
}

// htmlBlock is R10 for a block: its lines as a paragraph of text.
func (r *renderer) htmlBlock(n *ast.HTMLBlock) {
	lines := make([]string, 0, n.Lines().Len()+1)
	for i := range n.Lines().Len() {
		at := n.Lines().At(i)
		lines = append(lines, string(at.Value(r.source)))
	}
	if n.HasClosure() {
		lines = append(lines, string(n.ClosureLine.Value(r.source)))
	}
	r.put("<p>")
	for i, line := range lines {
		if i > 0 {
			r.put("<br>")
		}
		goldhtml.DefaultWriter.RawWrite(&r.out, []byte(strings.TrimRight(line, "\r\n")))
	}
	r.put("</p>")
}

func (r *renderer) table(n ast.Node) bool {
	switch n := n.(type) {
	case *extast.Table:
		r.put("<table>")
		rows := false
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			if _, row := c.(*extast.TableRow); row && !rows {
				rows = true
				r.put("<tbody>")
			}
			r.node(c)
		}
		if rows {
			r.put("</tbody>")
		}
		r.put("</table>")
	case *extast.TableHeader:
		r.wrap("<thead><tr>", "</tr></thead>", n)
	case *extast.TableRow:
		r.wrap("<tr>", "</tr>", n)
	case *extast.TableCell:
		if _, header := n.Parent().(*extast.TableHeader); header {
			r.wrap("<th>", "</th>", n)
		} else {
			r.wrap("<td>", "</td>", n)
		}
	default:
		return false
	}
	return true
}

func (r *renderer) inline(n ast.Node) {
	switch n := n.(type) {
	case *ast.Text:
		r.text(n)
	case *ast.String:
		r.write(n.Value, n.IsRaw())
	case *ast.CodeSpan:
		r.codeSpan(n)
	case *ast.Emphasis:
		tag := "em"
		if n.Level == 2 {
			tag = "strong"
		}
		r.wrap("<"+tag+">", "</"+tag+">", n)
	case *ast.Link:
		r.link(n.Destination, n)
	case *ast.Image:
		r.link(n.Destination, n)
	case *ast.AutoLink:
		r.autoLink(n)
	case *ast.RawHTML:
		for i := range n.Segments.Len() {
			at := n.Segments.At(i)
			goldhtml.DefaultWriter.RawWrite(&r.out, at.Value(r.source))
		}
	default:
		r.children(n)
	}
}

func (r *renderer) text(n *ast.Text) {
	r.write(n.Value(r.source), n.IsRaw())
	if n.SoftLineBreak() || n.HardLineBreak() {
		r.put("<br>")
	}
}

// write is text escaped; unless raw, its entities and backslash escapes resolved first.
func (r *renderer) write(value []byte, raw bool) {
	if raw {
		goldhtml.DefaultWriter.RawWrite(&r.out, value)
		return
	}
	goldhtml.DefaultWriter.Write(&r.out, value)
}

func (r *renderer) codeSpan(n *ast.CodeSpan) {
	r.put("<code>")
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			value := strings.TrimSuffix(string(t.Value(r.source)), "\n")
			goldhtml.DefaultWriter.RawWrite(&r.out, []byte(value))
			if len(value) < len(t.Value(r.source)) {
				r.put(" ")
			}
		}
	}
	r.put("</code>")
}

// link draws n's children as a link to destination when R8 allows it, else as text alone.
func (r *renderer) link(destination []byte, n ast.Node) {
	href, ok := safeHref(destination)
	if !ok {
		r.children(n)
		return
	}
	r.wrap(`<a href="`+href+`">`, "</a>", n)
}

func (r *renderer) autoLink(n *ast.AutoLink) {
	href, ok := safeHref(n.URL(r.source))
	if ok {
		r.put(`<a href="` + href + `">`)
	}
	goldhtml.DefaultWriter.RawWrite(&r.out, n.Label(r.source))
	if ok {
		r.put("</a>")
	}
}

// safeHref is R8: the destination with its escapes resolved and percent-encoded, as an
// attribute value, when its scheme is one of safeSchemes.
func safeHref(destination []byte) (string, bool) {
	dest := util.URLEscape(util.UnescapePunctuations(destination), true)
	u, err := url.Parse(string(dest))
	if err != nil {
		return "", false
	}
	needsHost, safe := safeSchemes[strings.ToLower(u.Scheme)]
	if !safe || needsHost && u.Host == "" || !needsHost && u.Opaque == "" {
		return "", false
	}
	return string(util.EscapeHTML([]byte(u.String()))), true
}

// language is a fence's language when it is a plain name (R5), else "".
func language(info []byte) string {
	if len(info) == 0 {
		return ""
	}
	for _, c := range info {
		if !nameByte(c) {
			return ""
		}
	}
	return string(info)
}

func nameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("+-_.#", c) >= 0
}

// builder is a strings.Builder as goldmark's writer, which never fails.
type builder struct{ sb strings.Builder }

func (b *builder) String() string { return b.sb.String() }

func (b *builder) Write(p []byte) (int, error) { return b.sb.Write(p) }

func (b *builder) WriteByte(c byte) error { return b.sb.WriteByte(c) }

func (b *builder) WriteRune(c rune) (int, error) { return b.sb.WriteRune(c) }

func (b *builder) WriteString(s string) (int, error) { return b.sb.WriteString(s) }

func (b *builder) Available() int { return 0 }

func (b *builder) Buffered() int { return 0 }

func (b *builder) Flush() error { return nil }
