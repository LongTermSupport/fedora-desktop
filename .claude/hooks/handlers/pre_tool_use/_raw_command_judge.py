"""The one sanctioned judge of a raw Bash command line (issue #294, frozen form).

The line is judged AS WRITTEN, exactly as the original handler judged it: the patterns are
searched over the whole raw text, so a command that only MENTIONS a blocked tool in text (a commit
message, an echo, a grep pattern) is denied too. That is a known limit, not a feature: four rounds
of an exception for text-only mentions each opened a real run the original denied, so the exception
is gone and the issue stays open for a parser-grade approach.

On top of the original reading it only ADDS denials. After the raw text it searches each
command-position segment of the line (the body of `bash -c '...'`, a command after `sudo`) and the
same segment with its words resolved the way the shell does (`d''nf` is `dnf`). A search that found
nothing in the raw text can only find more here, never less.

It lives in its own module so the Semgrep rule hook-regex-over-raw-command has one named place to
trust: a handler hands its line to `judge_raw_command_line` and nowhere else matches text against
the raw line. The rule hook-sanctioned-name-impostor keeps that name from being defined or
imported from anywhere but here, which is what protects the judge (the path exclusion of the
rules adds nothing to it).
"""

from collections.abc import Callable

from claude_code_hooks_daemon.utils.command_position import command_position_code_segments
from claude_code_hooks_daemon.utils.shell_segmentation import resolve_shell_word, shell_word_spans


def _normalised(text: str) -> str:
    """``text`` with each word resolved the way the shell does (`d''nf` is `dnf`), words joined by a space."""
    pieces = []
    for start, end in shell_word_spans(text):
        word = resolve_shell_word(text[start:end])
        pieces.append(text[start:end] if word is None else word)
    return " ".join(pieces)


def judge_raw_command_line(line: str, search: Callable[[str], str | None]) -> str | None:
    """The text ``search`` finds in ``line`` as written, or in one of its segments, or None.

    The raw text comes first, so every line the original handler denied is denied here.
    """
    found = search(line)
    if found is not None:
        return found
    for segment in command_position_code_segments(line):
        for text in (segment, _normalised(segment)):
            found = search(text)
            if found is not None:
                return found
    return None
