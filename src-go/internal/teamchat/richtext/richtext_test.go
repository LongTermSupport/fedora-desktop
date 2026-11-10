package richtext

import (
	"strings"
	"testing"
)

// TestHTML pins the HTML of each construct the team's message style uses (Plan 00241
// STYLE.md, Phase 3): R1-R12 of the package doc.
func TestHTML(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"R1 paragraph":        {"Hello world", "<p>Hello world</p>"},
		"R1 line break kept":  {"one\ntwo", "<p>one<br>two</p>"},
		"R1 two paragraphs":   {"one\n\ntwo", "<p>one</p><p>two</p>"},
		"R2 heading":          {"# Title", "<h1>Title</h1>"},
		"R2 heading six":      {"###### Small", "<h6>Small</h6>"},
		"R2 rule":             {"a\n\n---\n\nb", "<p>a</p><hr><p>b</p>"},
		"R3 bullets":          {"- one\n- two", "<ul><li>one</li><li>two</li></ul>"},
		"R3 star bullets":     {"* one\n* two", "<ul><li>one</li><li>two</li></ul>"},
		"R3 numbered":         {"1. one\n2. two", "<ol><li>one</li><li>two</li></ol>"},
		"R3 numbered start":   {"3. three\n4. four", `<ol start="3"><li>three</li><li>four</li></ol>`},
		"R3 nested":           {"- a\n  - b", "<ul><li>a<ul><li>b</li></ul></li></ul>"},
		"R3 loose item":       {"- a\n\n- b", "<ul><li><p>a</p></li><li><p>b</p></li></ul>"},
		"R4 bold":             {"**bold**", "<p><strong>bold</strong></p>"},
		"R4 italics star":     {"*it*", "<p><em>it</em></p>"},
		"R4 italics under":    {"_it_", "<p><em>it</em></p>"},
		"R4 snake_case kept":  {"a_b_c", "<p>a_b_c</p>"},
		"R5 code span":        {"run `go test`", "<p>run <code>go test</code></p>"},
		"R5 code span markup": {"`<b>&`", "<p><code>&lt;b&gt;&amp;</code></p>"},
		"R5 fence":            {"```\nx := 1\n<y>\n```", "<pre><code>x := 1\n&lt;y&gt;\n</code></pre>"},
		"R5 fence language":   {"```go\nx\n```", `<pre><code class="language-go">x` + "\n</code></pre>"},
		"R5 fence bad lang":   {"```go\"><script>\nx\n```", "<pre><code>x\n</code></pre>"},
		"R5 indented code":    {"    x < y", "<pre><code>x &lt; y\n</code></pre>"},
		"R6 quote":            {"> quoted\n> more", "<blockquote><p>quoted<br>more</p></blockquote>"},
		"R7 link":             {"[site](https://example.com/a?b=1&c=2)", `<p><a href="https://example.com/a?b=1&amp;c=2">site</a></p>`},
		"R7 http link":        {"[s](http://example.com)", `<p><a href="http://example.com">s</a></p>`},
		"R7 mailto":           {"[m](mailto:someone@example.com)", `<p><a href="mailto:someone@example.com">m</a></p>`},
		"R7 autolink":         {"<https://example.com>", `<p><a href="https://example.com">https://example.com</a></p>`},
		"R7 image as link":    {"![logo](https://example.com/l.png)", `<p><a href="https://example.com/l.png">logo</a></p>`},
		"R9 table": {
			"| A | B |\n|---|---|\n| 1 | 2 |",
			"<table><thead><tr><th>A</th><th>B</th></tr></thead><tbody><tr><td>1</td><td>2</td></tr></tbody></table>",
		},
		"R9 table aligned": {
			"| L | C | R |\n|:--|:-:|--:|\n| a | **b** | `c` |",
			"<table><thead><tr><th>L</th><th>C</th><th>R</th></tr></thead><tbody><tr><td>a</td><td><strong>b</strong></td><td><code>c</code></td></tr></tbody></table>",
		},
		"R9 table header only": {
			"| A |\n|---|",
			"<table><thead><tr><th>A</th></tr></thead></table>",
		},
		"R9 table pipe in cell": {
			"| A |\n|---|\n| x \\| y |",
			"<table><thead><tr><th>A</th></tr></thead><tbody><tr><td>x | y</td></tr></tbody></table>",
		},
		"R12 empty": {"", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := HTML(tc.in); got != tc.want {
				t.Errorf("HTML(%q)\n got: %s\nwant: %s", tc.in, got, tc.want)
			}
		})
	}
}

// TestHostile pins R8 and R10: no markup from the text survives, and a link goes only to a
// safe scheme; what is refused stays as its text, escaped.
func TestHostile(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"R10 inline html":     {"a <b>bold</b> c", "<p>a &lt;b&gt;bold&lt;/b&gt; c</p>"},
		"R10 script block":    {"<script>alert(1)</script>", "<p>&lt;script&gt;alert(1)&lt;/script&gt;</p>"},
		"R10 html block":      {"<div onclick=\"x\">\nhi\n</div>", "<p>&lt;div onclick=&quot;x&quot;&gt;<br>hi<br>&lt;/div&gt;</p>"},
		"R10 comment":         {"<!-- c -->", "<p>&lt;!-- c --&gt;</p>"},
		"R10 entities":        {"&lt;b&gt; &amp; \"q\" 'a'", "<p>&lt;b&gt; &amp; &quot;q&quot; 'a'</p>"},
		"R10 escaped entity":  {`\&amp; \<b>`, "<p>&amp;amp; &lt;b&gt;</p>"},
		"R10 nul":             {"a\x00b", "<p>a�b</p>"},
		"R8 javascript":       {"[x](javascript:alert(1))", "<p>x</p>"},
		"R8 javascript upper": {"[x](JavaScript:alert(1))", "<p>x</p>"},
		"R8 entity scheme":    {"[x](javascript&#58;alert(1))", "<p>x</p>"},
		"R8 data":             {"[x](data:text/html;base64,PHNjcmlwdD4=)", "<p>x</p>"},
		"R8 vbscript":         {"[x](vbscript:msgbox)", "<p>x</p>"},
		"R8 relative":         {"[x](/etc/passwd)", "<p>x</p>"},
		"R8 no host":          {"[x](https:/nohost)", "<p>x</p>"},
		"R8 autolink js":      {"<javascript:alert(1)>", "<p>javascript:alert(1)</p>"},
		"R8 image js":         {"![x](javascript:alert(1))", "<p>x</p>"},
		"R8 quote in url":     {`[x](https://example.com/"onmouseover="a)`, `<p><a href="https://example.com/%22onmouseover=%22a">x</a></p>`},
		"R8 link text html":   {"[<i>x</i>](https://example.com)", `<p><a href="https://example.com">&lt;i&gt;x&lt;/i&gt;</a></p>`},
		"R10 table html":      {"| <img src=x> |\n|---|\n| <b>1</b> |", "<table><thead><tr><th>&lt;img src=x&gt;</th></tr></thead><tbody><tr><td>&lt;b&gt;1&lt;/b&gt;</td></tr></tbody></table>"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := HTML(tc.in); got != tc.want {
				t.Errorf("HTML(%q)\n got: %s\nwant: %s", tc.in, got, tc.want)
			}
		})
	}
}

// TestOnlyAllowedTags is R11 over every case and a mixed document: each tag the output holds
// is one of Matrix's recommended subset, and its attributes are the ones this package writes.
func TestOnlyAllowedTags(t *testing.T) {
	doc := strings.Join([]string{
		"# H", "para *em* **strong** `code` [l](https://example.com)", "", "> q", "",
		"- a", "- b", "", "1. c", "", "```sh", "x", "```", "", "| A | B |", "|:-|-:|", "| 1 | 2 |",
		"", "<script>x</script>", "", "---",
	}, "\n")
	out := HTML(doc)
	allowed := map[string]bool{
		"<h1>": true, "</h1>": true, "<p>": true, "</p>": true, "<em>": true, "</em>": true,
		"<strong>": true, "</strong>": true, "<code>": true, "</code>": true, "</a>": true,
		"<blockquote>": true, "</blockquote>": true, "<ul>": true, "</ul>": true, "<ol>": true,
		"</ol>": true, "<li>": true, "</li>": true, "<pre>": true, "</pre>": true,
		"<table>": true, "</table>": true, "<thead>": true, "</thead>": true, "<tbody>": true,
		"</tbody>": true, "<tr>": true, "</tr>": true, "<th>": true, "</th>": true, "<td>": true,
		"</td>": true, "<hr>": true, "<br>": true,
		`<a href="https://example.com">`: true, `<code class="language-sh">`: true,
	}
	for rest := out; ; {
		open := strings.IndexByte(rest, '<')
		if open < 0 {
			break
		}
		end := strings.IndexByte(rest[open:], '>')
		if end < 0 {
			t.Fatalf("unclosed tag in %s", out)
		}
		if tag := rest[open : open+end+1]; !allowed[tag] {
			t.Errorf("tag %s is not in the allowed subset; output %s", tag, out)
		}
		rest = rest[open+end+1:]
	}
}
