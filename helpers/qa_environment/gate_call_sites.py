"""Find the `qa_gate_detail` call sites in a bash script, and say what it could not read.

`scripts/test-qa-helper-summary.bash` reads every pattern `qa-all.bash` applies to a gate's
output and runs it against that gate's real output, so a pattern and the wording it matches
cannot drift apart unnoticed. That coupling only covers the call sites it can FIND, which
makes finding them the load-bearing step.

The bash version was three greps that had to agree: a strict extraction, a looser
denominator, and a set of parameter expansions that re-split each match. Every defect found
in it was one of the three drifting from the other two — a spelling the extraction accepted
and the splitter mis-read, a spelling both greps missed in lockstep and therefore agreed on,
a prose mention only one of them counted. Two counts that must agree can agree while both
are wrong; that is the shape this whole QA tree keeps finding.

So there is no second count here. Anything that looks like a call and does not parse is
returned in `unparsed`, carrying its line number and text, and the caller fails on it. A
silently dropped call site is not a state this function can reach: it either parses an
occurrence or reports it.

Comments are removed first, quote-aware, because the name appears in prose in the very file
being read.

WHAT IT READS AND WHAT IT DOES NOT, stated no wider than it is true. It understands comments
and quoting well enough to tell a comment from a `#` inside a string. It does NOT track
string or heredoc CONTEXT, so the name written inside a string literal or a heredoc body —
`echo "use qa_gate_detail here"` — is scanned like any other occurrence, fails to parse, and
is reported as an unparsed call site. That is a false report with a confident message, which
is the same misdiagnosis class the comment stripping removes, one construct over. It is
latent: `qa-all.bash` has no such occurrence today. Anyone meeting that message for a line
that is plainly a string now knows where to look.
"""

import json
import re
import sys

_NAME = "qa_gate_detail"

# The name as a whole word. `_qa_gate_detail` and `qa_gate_detail_v2` are different
# functions, and a call to either is not a call to this one.
_OCCURRENCE = re.compile(rf"(?<![A-Za-z0-9_]){_NAME}(?![A-Za-z0-9_])")

# `"$var"` or `"${var}"`, then a pattern in either quote style. The pattern's quote character
# is captured and the closing quote must match it, so a `'` inside a double-quoted pattern
# does not end it early.
#
# `(?<!\\)` on the closing quote finds the real end of the pattern. Without it the non-greedy
# group stops at the first `"` of a `\"` and the site PARSES with a truncated pattern — not
# dropped, not reported, just wrong, which downstream becomes "the pattern no longer matches
# that gate's output": a true failure with a false diagnosis.
_ARGS = re.compile(
    r"""\s+"\$\{?(?P<var>[A-Za-z_][A-Za-z0-9_]*)\}?"\s+"""
    r"""(?P<quote>['"])(?P<pattern>.*?)(?<!\\)(?P=quote)"""
)

# THIS PARSER DOES NOT INTERPRET BASH ESCAPES, and an escaped quote is where that stops being
# harmless. Bash reads `"say \"hi\" now"` as `say "hi" now`; the text between the quotes is
# `say \"hi\" now`. Handing the second on would apply a pattern that is not the one
# `qa-all.bash` applies, and the whole point of this gate is that those two are the same
# string. Finding the right closing quote is therefore necessary but not sufficient — a
# pattern carrying an escaped quote is returned as `unparsed`, which is the honest answer for
# something this parser cannot read. Other backslashes are left alone: `\b` and `\d` are
# ordinary regex and bash passes them through unchanged. No live pattern contains a quote.
_ESCAPED_QUOTE = re.compile(r"\\['\"]")


def strip_comment(line):
    """Remove a trailing bash comment, leaving a `#` that is inside quotes alone.

    Both directions have bitten this gate. A comment mentioning the function inflated the old
    denominator and failed the suite with a message blaming the extraction regex — a true
    failure with a false diagnosis, which sends the next reader to widen a correct pattern.
    The header comment escaped only by writing the name in backticks.

    A `#` starts a comment when it is at the start of a word: at the line start, or preceded
    by whitespace. `x=1#two` is a literal in bash and stays one.
    """
    quote = None
    for index, char in enumerate(line):
        if quote is not None:
            if char == quote:
                quote = None
            continue
        if char in "'\"":
            quote = char
            continue
        if char == "#" and (index == 0 or line[index - 1].isspace()):
            return line[:index].rstrip()
    return line


def call_sites(text):
    """-> (sites, unparsed).

    A site is `{"line": n, "var": name, "pattern": text}`; an unparsed occurrence is
    `{"line": n, "text": the line}`. Both lists are in file order.
    """
    sites = []
    unparsed = []

    for lineno, raw in enumerate(text.splitlines(), start=1):
        line = strip_comment(raw)
        for occurrence in _OCCURRENCE.finditer(line):
            match = _ARGS.match(line, occurrence.end())
            if match is None or _ESCAPED_QUOTE.search(match.group("pattern")):
                unparsed.append({"line": lineno, "text": raw.strip()})
                continue
            sites.append({
                "line": lineno,
                "var": match.group("var"),
                "pattern": match.group("pattern"),
            })

    return sites, unparsed


def main(argv):
    if len(argv) != 2:
        print(f"usage: {argv[0]} <bash-script>", file=sys.stderr)
        return 2

    with open(argv[1], encoding="utf-8") as handle:
        sites, unparsed = call_sites(handle.read())

    # JSON rather than delimited lines: a pattern is an extended regex and may contain any
    # delimiter a marker line could use. The caller reads it with jq.
    print(json.dumps({"sites": sites, "unparsed": unparsed}))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
