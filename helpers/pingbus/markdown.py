"""The markdown an agent writes for a person, as the HTML Matrix carries (Plan 00241 STYLE.md).

A message keeps its markdown as the plain `body`, which every client shows, and adds the
HTML Element draws as `formatted_body` with `format: org.matrix.custom.html`. Only the
subset STYLE.md uses is converted:
- blocks: `#` to `######` headings, paragraphs (a line break is kept), `-`/`*` and `1.`
  lists (a line indented two spaces continues an item), `>` quotes, and fenced code;
- inline: `**bold**`, `*italics*` and `_italics_`, inline code, and links to `http`/`https`
  addresses.

Every character of the text is HTML-escaped, so nothing in it becomes markup but what the
markers make.
"""

from __future__ import annotations

import html
import re
from collections.abc import Mapping

FORMAT = "org.matrix.custom.html"
FENCE = "```"

_HEADING = re.compile(r"(#{1,6}) (.*)")
_BULLET = re.compile(r"[-*] (.*)")
_NUMBER = re.compile(r"[0-9]+\. (.*)")
_QUOTE = re.compile(r"> ?(.*)")
_INLINE = re.compile(
    r"`([^`\n]+)`"
    r"|\[([^\]\n]+)\]\((https?://[^)\s]+)\)"
    r"|\*\*(.+?)\*\*"
    r"|(?<![\w*])\*(?![\s*])(.+?)(?<![\s*])\*(?![\w*])"
    r"|(?<!\w)_(?![\s_])(.+?)(?<![\s_])_(?!\w)"
)


def formatted(content: Mapping[str, object]) -> dict:
    """`content` with its `body` added as HTML: a new dict, the argument unchanged."""
    body = content["body"]
    if not isinstance(body, str):
        raise TypeError("a message's body is a string")
    return {**content, "format": FORMAT, "formatted_body": to_html(body)}


def to_html(text: str) -> str:
    """The HTML of `text`, block by block."""
    lines = text.split("\n")
    out: list[str] = []
    i = 0
    while i < len(lines):
        line = lines[i]
        heading = _HEADING.fullmatch(line)
        if line.startswith(FENCE):
            i = _fence(lines, i + 1, out)
        elif not line.strip():
            i += 1
        elif heading:
            level = len(heading[1])
            out.append(f"<h{level}>{_inline(heading[2])}</h{level}>")
            i += 1
        elif _QUOTE.match(line):
            i = _quote(lines, i, out)
        elif _BULLET.match(line) or _NUMBER.match(line):
            i = _list(lines, i, out)
        else:
            i = _paragraph(lines, i, out)
    return "".join(out)


def _starts_block(line: str) -> bool:
    return (not line.strip() or line.startswith(FENCE) or bool(_HEADING.fullmatch(line))
            or bool(_QUOTE.match(line)) or bool(_BULLET.match(line)) or bool(_NUMBER.match(line)))


def _fence(lines: list[str], i: int, out: list[str]) -> int:
    """Code from line i to the closing fence (or the end), verbatim."""
    code: list[str] = []
    while i < len(lines) and not lines[i].startswith(FENCE):
        code.append(lines[i])
        i += 1
    out.append(f"<pre><code>{html.escape(chr(10).join(code))}</code></pre>")
    return i + 1


def _quote(lines: list[str], i: int, out: list[str]) -> int:
    quoted: list[str] = []
    while i < len(lines) and (m := _QUOTE.match(lines[i])):
        quoted.append(_inline(m[1]))
        i += 1
    out.append(f"<blockquote>{'<br>'.join(quoted)}</blockquote>")
    return i


def _list(lines: list[str], i: int, out: list[str]) -> int:
    """One list, of the kind its first item is: it ends at a line neither an item of that
    kind nor indented under one."""
    item, tag = (_BULLET, "ul") if _BULLET.match(lines[i]) else (_NUMBER, "ol")
    items: list[str] = []
    while i < len(lines):
        m = item.match(lines[i])
        if m:
            items.append(m[1])
        elif items and lines[i].startswith("  ") and lines[i].strip():
            items[-1] += " " + lines[i].strip()
        else:
            break
        i += 1
    out.append(f"<{tag}>" + "".join(f"<li>{_inline(x)}</li>" for x in items) + f"</{tag}>")
    return i


def _paragraph(lines: list[str], i: int, out: list[str]) -> int:
    kept = [lines[i]]
    i += 1
    while i < len(lines) and not _starts_block(lines[i]):
        kept.append(lines[i])
        i += 1
    out.append("<p>" + "<br>".join(_inline(x) for x in kept) + "</p>")
    return i


def _inline(text: str) -> str:
    """Inline markers as tags; everything between them escaped."""
    out: list[str] = []
    at = 0
    for m in _INLINE.finditer(text):
        out.append(html.escape(text[at:m.start()]))
        code, label, url, bold, star, under = m.groups()
        if code is not None:
            out.append(f"<code>{html.escape(code)}</code>")
        elif url is not None:
            out.append(f'<a href="{html.escape(url)}">{html.escape(label)}</a>')
        elif bold is not None:
            out.append(f"<strong>{_inline(bold)}</strong>")
        else:
            out.append(f"<em>{_inline(star if star is not None else under)}</em>")
        at = m.end()
    out.append(html.escape(text[at:]))
    return "".join(out)
