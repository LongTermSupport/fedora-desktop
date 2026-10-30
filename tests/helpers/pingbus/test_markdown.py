"""Unit tests for helpers/pingbus/markdown.py (Plan 00241 STYLE.md): the markdown an agent
writes for a person, as the HTML Matrix carries in `formatted_body`.

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_markdown
"""

from __future__ import annotations

import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import markdown as md


class Blocks(unittest.TestCase):
    def test_headings(self) -> None:
        self.assertEqual(md.to_html("#### 🔄 Update · it moved"), "<h4>🔄 Update · it moved</h4>")
        self.assertEqual(md.to_html("# One"), "<h1>One</h1>")
        self.assertEqual(md.to_html("####### seven"), "<p>####### seven</p>")
        self.assertEqual(md.to_html("#nospace"), "<p>#nospace</p>")

    def test_a_paragraph_keeps_its_line_breaks(self) -> None:
        self.assertEqual(md.to_html("one\ntwo"), "<p>one<br>two</p>")
        self.assertEqual(md.to_html("one\n\ntwo"), "<p>one</p><p>two</p>")

    def test_bullets_with_either_marker_and_a_continuation(self) -> None:
        self.assertEqual(md.to_html("- a\n* b\n  more of b"), "<ul><li>a</li><li>b more of b</li></ul>")

    def test_numbered_list(self) -> None:
        self.assertEqual(md.to_html("1. a\n2. b"), "<ol><li>a</li><li>b</li></ol>")

    def test_a_list_ends_at_a_line_that_is_not_an_item(self) -> None:
        self.assertEqual(md.to_html("- a\nafter"), "<ul><li>a</li></ul><p>after</p>")
        self.assertEqual(md.to_html("- a\n1. b"), "<ul><li>a</li></ul><ol><li>b</li></ol>")

    def test_blockquote(self) -> None:
        self.assertEqual(md.to_html("> said\n> twice"), "<blockquote>said<br>twice</blockquote>")
        self.assertEqual(md.to_html(">bare"), "<blockquote>bare</blockquote>")

    def test_fenced_code_is_kept_verbatim_and_escaped(self) -> None:
        self.assertEqual(md.to_html("```\n**not bold** <b>\n  indented\n```"),
                         "<pre><code>**not bold** &lt;b&gt;\n  indented</code></pre>")
        self.assertEqual(md.to_html("```bash\nls\n```"), "<pre><code>ls</code></pre>")

    def test_an_unclosed_fence_runs_to_the_end(self) -> None:
        self.assertEqual(md.to_html("```\nls"), "<pre><code>ls</code></pre>")

    def test_the_style_example(self) -> None:
        text = ("#### 🔄 Update · 00228 running again\n\n- ✅ **00191:** done\n- ⏳ **00228:** running\n\n"
                "**🙋 Needs you:**\n- **A7:** say \"A7 a\"")
        self.assertEqual(md.to_html(text),
                         "<h4>🔄 Update · 00228 running again</h4>"
                         "<ul><li>✅ <strong>00191:</strong> done</li><li>⏳ <strong>00228:</strong> running</li></ul>"
                         "<p><strong>🙋 Needs you:</strong></p>"
                         "<ul><li><strong>A7:</strong> say &quot;A7 a&quot;</li></ul>")


class Inline(unittest.TestCase):
    def test_bold_and_italics(self) -> None:
        self.assertEqual(md.to_html("**b** and *i* and _j_"), "<p><strong>b</strong> and <em>i</em> and <em>j</em></p>")

    def test_code_is_not_formatted_inside(self) -> None:
        self.assertEqual(md.to_html("run `a **b** c`"), "<p>run <code>a **b** c</code></p>")

    def test_an_underscore_inside_a_word_is_text(self) -> None:
        self.assertEqual(md.to_html("snake_case_name"), "<p>snake_case_name</p>")

    def test_a_lone_marker_is_text(self) -> None:
        self.assertEqual(md.to_html("2 * 3 and a ` tick"), "<p>2 * 3 and a ` tick</p>")

    def test_links_only_to_web_addresses(self) -> None:
        self.assertEqual(md.to_html("[docs](https://example.com/a?b=1&c=2)"),
                         '<p><a href="https://example.com/a?b=1&amp;c=2">docs</a></p>')
        self.assertEqual(md.to_html("[x](javascript:alert(1))"), "<p>[x](javascript:alert(1))</p>")


class Escaping(unittest.TestCase):
    def test_html_in_the_text_is_shown_not_run(self) -> None:
        self.assertEqual(md.to_html("<script>x</script> & \"q\""),
                         "<p>&lt;script&gt;x&lt;/script&gt; &amp; &quot;q&quot;</p>")

    def test_a_link_text_cannot_close_the_tag(self) -> None:
        self.assertEqual(md.to_html('[a"><b](https://example.com/")'),
                         '<p><a href="https://example.com/&quot;">a&quot;&gt;&lt;b</a></p>')


class Formatted(unittest.TestCase):
    def test_formatted_adds_the_pair_to_content(self) -> None:
        content = {"msgtype": "m.notice", "body": "**hi**"}
        self.assertEqual(md.formatted(content),
                         {"msgtype": "m.notice", "body": "**hi**",
                          "format": "org.matrix.custom.html", "formatted_body": "<p><strong>hi</strong></p>"})

    def test_formatted_leaves_the_argument_alone(self) -> None:
        content = {"body": "x"}
        md.formatted(content)
        self.assertEqual(content, {"body": "x"})


if __name__ == "__main__":
    unittest.main()
