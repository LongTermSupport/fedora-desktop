"""The sanctioned judge of a Bash line as WRITTEN, with the one exception that answers issue #294.

This module is the only place in the project's handlers that judges the raw text of a Bash
command line. It is a module of its own, named with a leading underscore (the daemon loads no
handler from it), so that the Semgrep rule `hook-regex-over-raw-command` can exclude it by path
(`.semgrep/hook-handlers.yml`): a renamed copy of the judge anywhere else is a finding, and the
rule `hook-sanctioned-name-impostor` bans a look-alike of the names it sanctions.

The default is the original handler's reading: the patterns are searched over the whole raw text,
so a line that runs a script it wrote, hands text to a shell, or wraps a change in anything
(`sudo`, `script -c`, `watch`, `find -exec`) names the change in its text and is denied.

The exception: a line is judged by what it RUNS (the daemon's command-position segments) only when
it is ALL INERT. A line is all inert when every one of these holds:

1. **It writes no file.** Any redirect to anything but `/dev/null`, `/dev/stdout`, `/dev/stderr`
   or a file descriptor (`>`, `>>`, `&>`, `>|`, `n>`, `<>`, `>&file`) means the raw text is
   judged. Text written on the line can then never be run by a later segment of the same line
   (a git hook, a `.git/config` key, an rc file, a script).
2. **It substitutes nothing.** `$(...)`, a backtick, `<(...)` or `>(...)` anywhere outside single
   quotes and quoted heredoc bodies (a double-quoted one included) executes, whatever the daemon's
   view makes of it.
3. **Every command is on the inert list** (:data:`TOOLS`) with an EXACT option allow-list. An
   option not on the list, a long option spelled in any abbreviation (git and GNU tools accept any
   unique prefix), a glued short option whose letter is not listed, anything but a listed git
   subcommand, and any git option before the subcommand (`-c`, `-C`, `--exec-path`, `--git-dir`,
   `--work-tree`, `--config-env`) means the raw text is judged. A list is never a deny-list.
4. **No word of those commands can expand to an option.** A `$` outside single quotes that starts
   a word or sits unquoted, or an unquoted brace, can become `--output=file` after the line is
   written (`for x in --output=f; do git log $x; done`).

An all-inert line cannot run a system change, so the exception cannot let one through.
"""

import re
from collections.abc import Callable
from dataclasses import dataclass, field

from claude_code_hooks_daemon.utils.command_position import (
    command_position_code_segments,
    segment_spans,
)
from claude_code_hooks_daemon.utils.shell_segmentation import (
    resolve_shell_word,
    shell_word_spans,
)


@dataclass(frozen=True)
class Tool:
    """One inert head (or head and subcommand) and the exact options it accepts.

    ``why`` says why every listed option cannot run a program or write a file. ``short`` are the
    flag letters, ``short_value`` the letters that take one value (the rest of the word, or the next
    word), ``long`` the exact long spellings that take no value (``--color=never`` is one), and
    ``long_value`` the exact long names with how many words they take (``--name=v`` or ``--name v``).
    ``needs_one_of`` names options of which at least one must be present.
    """

    why: str
    short: str = ""
    short_value: str = ""
    long: frozenset[str] = frozenset()
    long_value: dict[str, int] = field(default_factory=dict)
    numeric: bool = False
    any_args: bool = False
    needs_one_of: frozenset[str] = frozenset()

    def seen_options(self, args: list[str]) -> frozenset[str] | None:
        """The options ``args`` use, or None when one is not on this tool's exact lists."""
        if self.any_args:
            return frozenset()
        seen: set[str] = set()
        index = 0
        operands_only = False
        while index < len(args):
            word = args[index]
            index += 1
            if operands_only or word == "-" or not word.startswith("-"):
                continue
            if word == "--":
                operands_only = True
            elif word.startswith("--"):
                if word in self.long:
                    seen.add(word)
                    continue
                name, equals, _ = word.partition("=")
                count = self.long_value.get(name)
                if count is None or (equals and count != 1):
                    return None
                seen.add(name)
                index += 0 if equals else count
                if index > len(args):
                    return None
            elif not (self.numeric and word[1:].isdigit()):
                index = self._short_cluster(word, seen, index, len(args))
                if index < 0:
                    return None
        return frozenset(seen)

    def _short_cluster(self, word: str, seen: set[str], index: int, total: int) -> int:
        """The next argument index after the cluster ``word`` (`-qm`, `-nie`), or -1 if a letter is not listed."""
        for position, letter in enumerate(word[1:], start=1):
            if letter in self.short_value:
                seen.add("-" + letter)
                if position + 1 == len(word):
                    index += 1
                    return -1 if index > total else index
                return index
            if letter not in self.short:
                return -1
            seen.add("-" + letter)
        return index

    def accepts(self, args: list[str]) -> bool:
        seen = self.seen_options(args)
        return seen is not None and (not self.needs_one_of or bool(seen & self.needs_one_of))


_GIT_WHY = "git runs a program only through a global option, an option that names one, a configured key or a hook; the line writes no config or hook (rule 1) and no global option is accepted"
_GH_BODY = frozenset({"-b", "--body", "-F", "--body-file"})
_GH_TEXT = {
    "short_value": "bFtR",
    "long_value": {"--body": 1, "--body-file": 1, "--title": 1, "--repo": 1},
    "needs_one_of": _GH_BODY,
}
_GH_WHY = "-b/--body/-F/--body-file give the text, -t/--title the title, -R/--repo the repository: all data; no editor (-e, --editor), browser (--web) or API call is listed"

#: Every inert head, keyed by the head, or by head and subcommand for git and gh. The only words that
#: decide what runs are the options: each tool lists its own exactly, with the reason it is safe.
TOOLS: dict[str, Tool] = {
    "echo": Tool(why="the builtin prints its words; -n (no newline), -e and -E (escape handling) only change how it prints", short="neE"),
    "printf": Tool(why="the builtin prints its words by a format string; no option is accepted, so `-v var` (an assignment) is not"),
    "cat": Tool(why="reads files and prints them; the listed flags (-n -b number lines, -s squeeze blank lines, -A -E -T -v show control characters) only change the printing", short="nbsAETv"),
    "grep": Tool(
        why="searches its operands for a pattern; -n -i -l -r -E -F -w -c -v pick what is searched and printed, -e takes the pattern, --color=never is the exact spelling that turns colour off; no -f, -P, -H, --include or --exclude-from is listed",
        short="nilrEFwcv",
        short_value="e",
        long=frozenset({"--color=never"}),
    ),
    "rg": Tool(
        why="the same small set as grep; --pre, --pre-glob and --hostname-bin run a program and -z (search compressed files) runs a decompressor, so none is listed",
        short="nilrEFwcv",
        short_value="e",
        long=frozenset({"--color=never"}),
    ),
    "jq": Tool(
        why="a jq program cannot run a command or write a file; -r -c -n -e -s -S -R -j change input and output only, --arg and --argjson bind a name to a value; -f, --from-file, -L and --rawfile read files or modules and are not listed",
        short="rcnesSRj",
        long_value={"--arg": 2, "--argjson": 2},
    ),
    "wc": Tool(why="counts lines (-l), words (-w), bytes (-c) or characters (-m) of its input; --files0-from is not listed", short="lwcm"),
    "cd": Tool(why="changes the directory of the line and runs nothing; it takes no option"),
    "true": Tool(why="ignores every argument and does nothing", any_args=True),
    "false": Tool(why="ignores every argument and fails", any_args=True),
    "git commit": Tool(
        why=_GIT_WHY + "; -m and -F give the message, -a stages tracked files, -q is quiet, -S signs with the configured key; --amend, -C, -c, -e, --template, --no-verify and every long option are not listed",
        short="aqS",
        short_value="mF",
    ),
    "git log": Tool(
        why=_GIT_WHY + "; the listed options choose what is printed and how; --grep and --format take text; --output, --ext-diff and -O (an order file) are not listed",
        short="p",
        short_value="n",
        long=frozenset({"--oneline", "--stat", "--name-only", "--name-status", "--no-color", "--no-patch"}),
        long_value={"--grep": 1, "--format": 1, "--pretty": 1},
        numeric=True,
    ),
    "git show": Tool(
        why=_GIT_WHY + "; the listed options choose what is printed and how; --output and --ext-diff are not listed",
        short="ps",
        long=frozenset({"--oneline", "--stat", "--name-only", "--name-status", "--no-color"}),
        long_value={"--format": 1, "--pretty": 1},
    ),
    "git diff": Tool(
        why=_GIT_WHY + "; the listed options choose what is compared and printed; --no-ext-diff turns the external diff off, --ext-diff and --output are not listed",
        long=frozenset({"--stat", "--name-only", "--name-status", "--cached", "--staged", "--no-color", "--quiet", "--no-ext-diff"}),
    ),
    "git status": Tool(
        why=_GIT_WHY + "; -s -b and their long spellings and --porcelain only change the report",
        short="sb",
        long=frozenset({"--short", "--branch", "--porcelain"}),
    ),
    "git add": Tool(why=_GIT_WHY + "; -A -u stage by scope, -n and -v only report; -p and -i (interactive) are not listed", short="Aunv"),
    "gh issue comment": Tool(why=_GH_WHY, **_GH_TEXT),
    "gh issue create": Tool(why=_GH_WHY, **_GH_TEXT),
    "gh pr comment": Tool(why=_GH_WHY, **_GH_TEXT),
    "gh pr create": Tool(why=_GH_WHY, **_GH_TEXT),
}

#: The heads whose segment can be inert. `for`, `done` and `fi` are shell constructs, not commands.
INERT_COMMANDS: frozenset[str] = frozenset({name for name in TOOLS if " " not in name} | {"git", "gh"})
_CLOSERS = frozenset({"done", "fi"})
#: Heads whose words may expand: an expansion cannot turn an argument of these into a program or a write.
_EXPANSION_OK = frozenset({"echo", "printf", "cd", "true", "false", "for", "done", "fi"})

#: Words that open a shell construct before its command: `then echo x`.
_RESERVED_PREFIX: frozenset[str] = frozenset({"then", "do", "else", "elif", "if", "while", "until", "!", "{", "(", "}", ")", ""})

_PRINT_FREE = ("$(", "`", "<(", ">(")
_HEREDOC_OPERATOR = re.compile(r"""<<(-?)[ \t]*(\\?)(['"]?)([A-Za-z_][A-Za-z0-9_]*)\3""")
_FD_DUPLICATE = re.compile(r"\d+-?|-")
_SUBSTITUTION_IN_BODY = re.compile(r"(?<!\\)(?:\$\(|`)")
_STANDARD_TARGETS = frozenset({"/dev/null", "/dev/stdout", "/dev/stderr"})
_WORD_END = " \t\n;&|()<>"
_COMMENT_START_AFTER = " \t\n;&|()<>"


def _without_group_parens(word: str) -> str:
    """``word`` without the `(` that opens a subshell and the unmatched `)` that closes one."""
    word = word.lstrip("(") if not word.startswith("$(") else word
    while word.endswith(")") and word.count(")") > word.count("("):
        word = word[:-1]
    return word


def _word_pairs(segment: str) -> list[tuple[str, str]]:
    """(as written, resolved) of the segment's words after a construct opener.

    A word the shell resolves only at run time (`"$x"`) stays as written, so an option check sees
    its leading `-`.
    """
    pairs: list[tuple[str, str]] = []
    for start, end in shell_word_spans(segment):
        raw = segment[start:end]
        word = resolve_shell_word(raw)
        pairs.append((raw, raw if word is None else _without_group_parens(word)))
    while pairs and pairs[0][1] in _RESERVED_PREFIX:
        pairs = pairs[1:]
    return pairs


def _normalised(text: str) -> str:
    """``text`` with each word resolved the way the shell does (`d''nf` is `dnf`), words joined by a space."""
    pieces = []
    for start, end in shell_word_spans(text):
        word = resolve_shell_word(text[start:end])
        pieces.append(text[start:end] if word is None else word)
    return " ".join(pieces)


def word_can_expand_to_an_option(raw: str) -> bool:
    """Whether the word as written can become an option (or several words) when the shell expands it.

    A `$` outside single quotes does when it is unquoted or comes before any literal text of the
    word (`$x`, `"$x"`, `''"$x"`): the value then supplies the leading `-`. A `$` after literal
    text in double quotes (`"dnf install $PKG"`) cannot. An unquoted brace expands to several words.
    """
    quote = ""
    literal_seen = False
    index = 0
    while index < len(raw):
        char = raw[index]
        if quote == "'":
            if char == "'":
                quote = ""
            else:
                literal_seen = True
        elif char == "\\":
            index += 1
            literal_seen = True
        elif quote == '"':
            if char == '"':
                quote = ""
            elif char == "$" and not literal_seen:
                return True
            else:
                literal_seen = True
        elif char in "'\"":
            quote = char
        elif char in "${":
            return True
        else:
            literal_seen = True
        index += 1
    return False


def head_is_inert(words: list[str]) -> bool:
    """Whether the command ``words`` run is on the inert list with every option on that tool's exact list."""
    head, rest = words[0], words[1:]
    if head == "for":
        return True
    if head in _CLOSERS:
        return not rest
    if head == "git":
        git_tool = TOOLS.get(f"git {rest[0]}") if rest else None
        return git_tool is not None and git_tool.accepts(rest[1:])
    if head == "gh":
        gh_tool = TOOLS.get(f"gh {rest[0]} {rest[1]}") if len(rest) >= 2 else None
        return gh_tool is not None and gh_tool.accepts(rest[2:])
    tool = TOOLS.get(head)
    return tool is not None and " " not in head and tool.accepts(rest)


def _segment_is_inert(segment: str) -> bool:
    """Whether the command one segment of the line AS WRITTEN runs executes none of its arguments as code.

    The daemon's view blanks the arguments of grep, echo and a commit message, options included, so
    the options are read from the line as written.
    """
    if any(marker in segment for marker in _PRINT_FREE):
        return False
    pairs = _word_pairs(segment)
    if not pairs:
        return True
    words = [word for _, word in pairs]
    if not head_is_inert(words):
        return False
    return words[0] in _EXPANSION_OK or not any(word_can_expand_to_an_option(raw) for raw, _ in pairs[1:])


@dataclass
class LineScan:
    """What the raw text of a line does apart from the commands it names."""

    writes: bool = False
    substitutes: bool = False
    bodies: list[tuple[int, int]] = field(default_factory=list)


def _redirect_target(line: str, start: int) -> tuple[str, int]:
    """The redirect target word starting at or after ``start``, its quotes removed, and where it ends."""
    index = start
    while index < len(line) and line[index] in " \t":
        index += 1
    target = []
    quote = ""
    while index < len(line):
        char = line[index]
        if quote:
            if char == quote:
                quote = ""
            else:
                target.append(char)
        elif char in "'\"":
            quote = char
        elif char in _WORD_END:
            break
        else:
            target.append(char)
        index += 1
    return "".join(target), index


def _heredoc_body_end(text: str, body_start: int, delimiter: str, strip_tabs: bool) -> tuple[int, int]:
    """(end of the body, the index after the terminator line); an unterminated body runs to the end."""
    offset = body_start
    while offset <= len(text):
        newline = text.find("\n", offset)
        line_end = len(text) if newline < 0 else newline
        candidate = text[offset:line_end]
        if (candidate.lstrip("\t") if strip_tabs else candidate) == delimiter:
            return offset, min(line_end + 1, len(text))
        if newline < 0:
            return len(text), len(text)
        offset = newline + 1
    return len(text), len(text)


def _scan_redirect(line: str, index: int, scan: LineScan) -> int:
    """The index of the last character of the redirect at ``index`` (a `>`), noting a write in ``scan``."""
    after = index + 1
    if line[after : after + 1] in (">", "|"):
        after += 1
    elif line[after : after + 1] == "&":
        duplicate = _FD_DUPLICATE.match(line, after + 1)
        if duplicate is not None:
            return duplicate.end() - 1
        after += 1
    target, end = _redirect_target(line, after)
    if target not in _STANDARD_TARGETS:
        scan.writes = True
    return max(end, after) - 1


def scan_line(line: str) -> LineScan:
    """Whether the raw text writes a file or substitutes, outside single quotes, comments and quoted heredoc bodies."""
    scan = LineScan()
    pending: list[tuple[str, bool, bool]] = []
    quote = ""
    index = 0
    while index < len(line):
        char = line[index]
        if quote == "'":
            quote = "" if char == "'" else quote
        elif quote == '"':
            if char == "\\":
                index += 1
            elif char == '"':
                quote = ""
            elif char == "`" or line.startswith("$(", index):
                scan.substitutes = True
        elif char == "\\":
            index += 1
        elif char in "'\"":
            quote = char
        elif char == "#" and (index == 0 or line[index - 1] in _COMMENT_START_AFTER):
            newline = line.find("\n", index)
            index = len(line) if newline < 0 else newline - 1
        elif char == "`" or line.startswith(("$(", "<(", ">("), index):
            scan.substitutes = True
        elif line.startswith("<<<", index):
            index += 2
        elif line.startswith("<<", index):
            match = _HEREDOC_OPERATOR.match(line, index)
            if match is not None:
                pending.append((match.group(4), bool(match.group(2) or match.group(3)), bool(match.group(1))))
                index = match.end() - 1
        elif line.startswith("<>", index):
            scan.writes = True
        elif char == "\n" and pending:
            for delimiter, quoted, strip_tabs in pending:
                end, after = _heredoc_body_end(line, index + 1, delimiter, strip_tabs)
                if not quoted and _SUBSTITUTION_IN_BODY.search(line[index + 1 : end]):
                    scan.substitutes = True
                scan.bodies.append((index + 1, after))
                index = after - 1
            pending = []
        elif char == ">":
            index = _scan_redirect(line, index, scan)
        index += 1
    return scan


def _without_heredoc_bodies(line: str, bodies: list[tuple[int, int]]) -> str:
    """``line`` without the heredoc bodies and their terminator lines: data, not commands."""
    pieces = []
    position = 0
    for start, end in bodies:
        pieces.append(line[position:start])
        position = end
    pieces.append(line[position:])
    return "".join(pieces)


def every_command_is_inert(line: str) -> bool:
    """Whether ``line`` is all inert: no write, no substitution, only exactly listed commands and options.

    The commands are read from the line AS WRITTEN: the daemon's command view blanks the arguments
    of grep, echo and a commit message (options included) and surfaces only the forms it knows.
    """
    scan = scan_line(line)
    if scan.writes or scan.substitutes:
        return False
    visible = _without_heredoc_bodies(line, scan.bodies)
    return all(_segment_is_inert(visible[start:end]) for start, end in segment_spans(visible))


def judge_raw_command_line(line: str, search: Callable[[str], str | None]) -> str | None:
    """The text of the system change ``line`` runs, or None. THE one place a line is judged as written.

    The default is the original handler's reading: ``search`` over the whole raw text, then over
    each command-position segment with its words resolved (`d''nf install`). The exception is a
    line that is all inert (:func:`every_command_is_inert`): every command on it is on the exact
    allow-lists and none runs its arguments, so nothing on it can run a system change and a
    message, an echo or a grep pattern that only names one passes.
    """
    if every_command_is_inert(line):
        return None
    found = search(line)
    if found is not None:
        return found
    for segment in command_position_code_segments(line):
        for text in (segment, _normalised(segment)):
            found = search(text)
            if found is not None:
                return found
    return None
