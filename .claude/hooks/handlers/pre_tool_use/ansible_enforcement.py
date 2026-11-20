"""AnsibleEnforcementHandler - enforces Ansible-only system deployment.

Blocks direct package management, system configuration, and service management
commands. All system changes must go through Ansible playbooks.

The line is judged as WRITTEN, exactly as the original handler did: any forbidden pattern
anywhere in the Bash text denies it. That default cannot regress, because it is the original
reading, and a line that runs a script it wrote, hands text to a shell, or wraps a change in
anything (`sudo`, `script -c`, `watch`, `find -exec`, `perl -e system(...)`) names the change
in its text.

One exception, `judge_raw_command_line` below, answers issue #294 (a commit message, an echo or
a grep pattern that merely NAMES `dnf install` was denied): when EVERY command the line runs is
inert, the line is judged by those commands instead of its raw text. An inert command executes
none of its arguments as code: `echo`, `printf`, `grep`/`rg`, `cat`, `jq`, `git commit`/`log`/
`show`/`diff`/`status`/`add`/`tag`/`grep`/`push`/`fetch`, `gh issue|pr comment|create` with a body
given, `true`, `test`, `wc`, `sort`, `head`, `tail`, `ls`, `pwd`, `cd`, `uniq`. A wrapper (`sudo`,
`env`, `timeout`, `nice`, `nohup`...), a shell, an interpreter, `find`, `xargs`, `make`, `watch`
and anything unknown is NOT inert, so a line holding one is judged on its raw text, whatever else
it holds. An all-inert line cannot run a system change, so the exception cannot let one through.
Over-blocking is accepted: an inert write plus an unrelated shell run on one line is judged as the
original judged it.
"""

import re
import sys
from collections.abc import Callable
from pathlib import Path
from typing import Any, ClassVar

# Add daemon to path for imports
sys.path.insert(0, str(Path(__file__).parent.parent.parent / "hooks-daemon/src"))

from claude_code_hooks_daemon.core import Decision, GatingResult
from claude_code_hooks_daemon.core.acceptance_test import AcceptanceTest, TestType
from claude_code_hooks_daemon.core.handler_bases import PreToolUseHandlerBase
from claude_code_hooks_daemon.core.rule import Rule
from claude_code_hooks_daemon.core.utils import get_bash_command
from claude_code_hooks_daemon.utils.command_position import (
    DATA_HEADS,
    command_position_code_segments,
    segment_spans,
)
from claude_code_hooks_daemon.utils.shell_segmentation import (
    resolve_shell_word,
    shell_word_spans,
)

RULE_ID = "R-ANSIBLE-DIRECT-SYSTEM-CHANGE"

_RULE = Rule(
    rule_id=RULE_ID,
    blocked="a command that changes the system directly: dnf/yum/rpm/pip/npm -g/flatpak/snap, systemctl state changes, gsettings/dconf/firewall-cmd writes, user and group management",
    why="The change lives outside version control: configuration drift that a fresh install does not reproduce",
    fix="Put the change in an Ansible playbook under playbooks/imports/ and run the playbook",
    verbose=(
        "WHY: every system change must go through an Ansible playbook, so it is version\n"
        "controlled, documented in playbook files, reproducible on a fresh install and\n"
        "consistent with the rest of the configuration.\n\n"
        "The line is judged as written: `sudo dnf install x`, `a && systemctl restart y`,\n"
        "`bash -c 'dnf install x'`, a script the line writes and then runs, and text piped into\n"
        "a shell are blocked, whatever options sit before the verb (`dnf -y install`) and\n"
        "however the tool is spelled (`dnf5`, `/usr/bin/dnf`, `d''nf`, behind `sudo`, `env`).\n\n"
        "Only a line whose every command is inert (echo, printf, grep, cat, jq, a git commit...)\n"
        "is judged by what it runs, so a commit message or an echo that merely names `dnf install`\n"
        "is not blocked. A shell, wrapper, interpreter, `find`, `xargs` or `make` on the line\n"
        "puts the whole line back to its raw text.\n\n"
        "ALLOWED (read-only queries): dnf info/list/search, rpm -q, systemctl status,\n"
        "systemctl --user, gsettings get, dconf read, flatpak list."
    ),
)


def _options(value_options: tuple[str, ...] = (), not_before_option: str = "") -> str:
    """A pattern for any number of options: the words that start with `-`.

    ``value_options`` name the ones that take the next word as their value
    (`dnf --releasever 40 install`). ``not_before_option`` is a lookahead an option must not
    start with: the `--user` of `systemctl --user enable`.
    """
    guard = f"(?!{not_before_option})" if not_before_option else ""
    valued = "|".join(re.escape(option) for option in value_options)
    return (rf"(?:{guard}(?:{valued})\s+\S+\s+|" if valued else "(?:") + rf"{guard}-\S*\s+)*"


def _tool(names: str, verbs: str, value_options: tuple[str, ...] = (), not_before_option: str = "") -> str:
    """A pattern for ``names`` followed by any options and then one of ``verbs``."""
    return rf"\b(?:{names})\s+{_options(value_options, not_before_option)}(?:{verbs})\b"


_RPM_VALUE_OPTIONS = r"--root|--dbpath|--prefix|--relocate|--define|-D|--macros"
_NPM_GLOBAL = r"(?:-g|--global)\b"
_NPM_VERB = r"(?:install|i|add)\b"
_DNF_VALUE_OPTIONS = (
    "-c", "--config", "--releasever", "--enablerepo", "--disablerepo", "--repo", "--repoid", "--installroot", "-x", "--exclude",
    "--forcearch", "--setopt", "-d", "--debuglevel", "-e", "--errorlevel", "--rpmverbosity", "--color",
)  # fmt: skip
_PACKAGE_VERBS = (
    "install|remove|erase|update|upgrade|downgrade|reinstall|autoremove|distro-sync|distrosync|swap|mark"
    r"|groupinstall|groupremove|groupupdate|group\s+(?:install|remove|erase|upgrade|update)"
    r"|module\s+(?:install|remove|enable|disable|reset|switch-to|update)|history\s+(?:undo|redo|rollback)"
)
_SYSTEMCTL_VALUE_OPTIONS = ("-H", "--host", "-M", "--machine", "--root", "-t", "--type", "-p", "--property")
_SYSTEMCTL_VERBS = (
    "enable|disable|start|stop|restart|reload|mask|unmask|try-restart|reload-or-restart|try-reload-or-restart"
    "|daemon-reload|daemon-reexec|set-default|isolate|preset|preset-all|link|edit|revert|set-property"
    "|reset-failed|kill|add-wants|add-requires|import-environment|set-environment|unset-environment"
    "|reboot|poweroff|halt|suspend|hibernate"
)

# Forbidden command patterns, matched case-insensitively against the text of the line.
_FORBIDDEN_PATTERNS: tuple[str, ...] = (
    # Package management - install/remove operations
    _tool("dnf5?", _PACKAGE_VERBS + r"|system-upgrade|builddep|copr\s+(?:enable|disable|remove)", _DNF_VALUE_OPTIONS),
    _tool("yum", _PACKAGE_VERBS, _DNF_VALUE_OPTIONS),
    rf"\brpm\s+(?:(?:{_RPM_VALUE_OPTIONS})\s+\S+\s+|--(?!install\b|upgrade\b|erase\b|freshen\b|query\b|verify\b)\S+\s+|-v+\s+)*"
    r"(?:-[iUeFhv]*[iUeF][iUeFhv]*|--install|--upgrade|--erase|--freshen)",
    _tool("pip[0-9.]*", "install|uninstall", ("--proxy", "--cache-dir", "--log", "--timeout", "--retries", "--cert", "--client-cert", "--trusted-host", "--python", "--exists-action")),
    # npm install -g, with the global flag after the verb or before it
    rf"\bnpm\s+(?:-\S*\s+)*{_NPM_VERB}[^;&|\n]*?\s+{_NPM_GLOBAL}",
    rf"\bnpm\s+(?:-\S*\s+)*?{_NPM_GLOBAL}\s+(?:-\S*\s+)*{_NPM_VERB}",
    _tool("flatpak", "install|remove|uninstall|update"),
    _tool("snap", "install|remove|refresh"),
    # System services - state changes (not queries). A --user unit is the user's own, but the last
    # scope flag wins, so any --system on the line makes it a system change.
    _tool("systemctl", _SYSTEMCTL_VERBS, _SYSTEMCTL_VALUE_OPTIONS, not_before_option=r"--user\b"),
    rf"\bsystemctl\s+{_options(_SYSTEMCTL_VALUE_OPTIONS)}--system\s+{_options(_SYSTEMCTL_VALUE_OPTIONS)}(?:{_SYSTEMCTL_VERBS})\b",
    # System configuration
    _tool("gsettings", "set|reset|reset-recursively", ("--schemadir",)),
    _tool("dconf", "write|reset|load"),
    r"\bgconftool(-2)?\s+--set",
    r"\bfirewall-cmd\s+.*--(add|remove|zone)",
    # User/group management
    r"\buseradd\b",
    r"\busermod\b",
    r"\buserdel\b",
    r"\bgroupadd\b",
    r"\bgroupmod\b",
    r"\bgroupdel\b",
)

#: A quoted word that is one plain name or path: `'dnf'`, `"/usr/bin/systemctl"`.
_QUOTED_NAME = re.compile(r"""(?<![\w'"])(['"])([\w./+-]+)\1(?![\w'"])""")

#: The commands that execute none of their arguments as code. The list is closed on purpose:
#: a wrapper, a shell, an interpreter, `find`, `xargs`, `make`, `watch` and anything unknown is not
#: in it, so a line holding one is judged on its raw text. `git` and `gh` are inert only with the
#: subcommands below (`_git_is_inert`, `_gh_is_inert`).
INERT_COMMANDS: frozenset[str] = frozenset(
    {
        "echo", "printf", "grep", "egrep", "fgrep", "rg", "cat", "jq", "git", "gh", "true", "false", "test", "[",
        "wc", "sort", "head", "tail", "ls", "pwd", "cd", "uniq",
    }
)  # fmt: skip
_GIT_INERT_SUBCOMMANDS = frozenset({"commit", "log", "show", "diff", "status", "add", "tag", "grep", "push", "fetch"})
#: An argument that makes an otherwise inert command run another program.
_RUNS_A_PROGRAM = ("--exec", "--receive-pack", "--upload-pack", "--open-files-in-pager", "--pre", "--hostname-bin", "--compress-program")
_BODY_OPTIONS = frozenset({"-b", "--body", "-F", "--body-file"})
_CLOSERS = frozenset({"done", "fi"})
_HEREDOC_BODY = "HEREDOC_BODY"

#: Words that open a shell construct before its command: `then echo x`.
_RESERVED_PREFIX: frozenset[str] = frozenset({"then", "do", "else", "elif", "if", "while", "until", "!", "{", "(", "}", ")", ""})

_PRINT_FREE = ("$(", "`", "<(", ">(")
_SIMPLE_VARIABLE = re.compile(r"(?<!\\)\$(?:\{[A-Za-z_][A-Za-z0-9_]*\}|[A-Za-z_][A-Za-z0-9_]*)")
_HEREDOC_OPERATOR = re.compile(r"<<(-?)[ \t]*([A-Za-z_][A-Za-z0-9_]*)(?=[\s;|&)<>]|$)")
_ANY_HEREDOC_DELIMITER = re.compile(r"""<<-?[ \t]*['"]?([A-Za-z_][A-Za-z0-9_]*)['"]?""")


def _basename(word: str) -> str:
    return word.rsplit("/", 1)[-1]


def _without_group_parens(word: str) -> str:
    """``word`` without the `(` that opens a subshell and the unmatched `)` that closes one."""
    word = word.lstrip("(") if not word.startswith("$(") else word
    while word.endswith(")") and word.count(")") > word.count("("):
        word = word[:-1]
    return word


def _words(segment: str) -> list[str] | None:
    """The segment's resolved words without a construct opener, or None when one cannot be read."""
    resolved = [resolve_shell_word(segment[start:end]) for start, end in shell_word_spans(segment)]
    if any(word is None for word in resolved):
        return None
    words = [_without_group_parens(word) for word in resolved if word is not None]
    while words and words[0] in _RESERVED_PREFIX:
        words = words[1:]
    return words


def _head_word(segment: str) -> str | None:
    """The first word of the segment after a construct opener, None when it cannot be read.

    Later words may be unreadable (`echo $X`): only the head decides what a print-only rewrite does.
    """
    for start, end in shell_word_spans(segment):
        word = resolve_shell_word(segment[start:end])
        if word is None:
            return None
        word = _without_group_parens(word)
        if word not in _RESERVED_PREFIX:
            return word
    return None


def _normalised(text: str) -> str:
    """``text`` with each word resolved the way the shell does (`d''nf` is `dnf`), words joined by a space."""
    pieces = []
    for start, end in shell_word_spans(text):
        word = resolve_shell_word(text[start:end])
        pieces.append(text[start:end] if word is None else word)
    return " ".join(pieces)


def _git_is_inert(rest: list[str]) -> bool:
    """`git [-C dir] <subcommand>` with an inert subcommand and no argument that runs a program."""
    index = 0
    while rest[index : index + 1] == ["-C"]:
        index += 2
    return rest[index : index + 1] and rest[index] in _GIT_INERT_SUBCOMMANDS and not _names_a_program_to_run(rest)


def _gh_is_inert(rest: list[str]) -> bool:
    """`gh issue|pr comment|create` with the text given by `-b`, `--body` or a body file."""
    if len(rest) < 3 or rest[0] not in ("issue", "pr") or rest[1] not in ("comment", "create"):
        return False
    return any(word in _BODY_OPTIONS or word.startswith(("--body=", "--body-file=")) for word in rest[2:])


def _names_a_program_to_run(rest: list[str]) -> bool:
    return any(word.startswith(_RUNS_A_PROGRAM) or word == "-O" for word in rest)


def _head_is_inert(words: list[str]) -> bool:
    head, rest = words[0], words[1:]
    if head == "for":
        return True
    if head in _CLOSERS:
        return not rest
    if head not in INERT_COMMANDS:
        return False
    if head == "git":
        return bool(_git_is_inert(rest))
    if head == "gh":
        return _gh_is_inert(rest)
    return not _names_a_program_to_run(rest)


def _segment_is_inert(segment: str, heredoc_delimiters: set[str]) -> bool:
    """Whether the command one segment runs executes none of its arguments as code."""
    if any(marker in segment for marker in _PRINT_FREE):
        return False
    text = segment.strip()
    if heredoc_delimiters and (text == _HEREDOC_BODY or text in heredoc_delimiters):
        return True
    words = _words(segment)
    if words is None:
        return False
    return not words or _head_is_inert(words)


def _quote_inert_heredocs(text: str) -> str:
    """``text`` with each unquoted heredoc whose body substitutes nothing made a quoted one.

    An unquoted `<<EOF` body is expanded by the shell, so the daemon keeps it as text that may
    execute. A heredoc with no `$(` and no grave accent is plain data, so giving its delimiter
    quotes hands the daemon the same body to judge by its own rules. The text is only judged,
    never executed, so the rewrite changes no behaviour.
    """
    pieces: list[str] = []
    position = 0
    quote = ""
    pending: list[re.Match[str]] = []
    index = 0
    while index < len(text):
        char = text[index]
        if quote:
            if char == "\\" and quote == '"':
                index += 2
                continue
            if char == quote:
                quote = ""
        elif char == "\\":
            index += 2
            continue
        elif char in "'\"":
            quote = char
        elif char == "#" and (index == 0 or text[index - 1] in " \t\n"):
            newline = text.find("\n", index)
            index = len(text) if newline < 0 else newline
            continue
        elif text.startswith("<<", index) and not text.startswith("<<<", index) and (index == 0 or text[index - 1] != "<"):
            match = _HEREDOC_OPERATOR.match(text, index)
            if match is not None:
                pending.append(match)
                index = match.end()
                continue
        elif char == "\n":
            index += 1
            for operator in pending:
                end = _heredoc_end(text, index, operator)
                if end is None:
                    continue
                body_end, after = end
                if "$(" not in text[index:body_end] and "`" not in text[index:body_end]:
                    pieces.append(text[position : operator.start()])
                    pieces.append(f"<<{operator.group(1)}'{operator.group(2)}'")
                    position = operator.end()
                index = after
            pending = []
            continue
        index += 1
    pieces.append(text[position:])
    return "".join(pieces)


def _heredoc_end(text: str, body_start: int, operator: re.Match[str]) -> tuple[int, int] | None:
    """(end of the body, the index after the terminator line) of a heredoc, or None if unterminated."""
    delimiter = operator.group(2)
    offset = body_start
    while offset <= len(text):
        newline = text.find("\n", offset)
        line_end = len(text) if newline < 0 else newline
        line = text[offset:line_end]
        if (line.lstrip("\t") if operator.group(1) else line) == delimiter:
            return offset, min(line_end + 1, len(text))
        if newline < 0:
            return None
        offset = newline + 1
    return None


def _swap_head_for_echo(segment: str, name: str) -> str:
    """``segment`` with its first word that resolves to ``name`` spelled `echo`."""
    for start, end in shell_word_spans(segment):
        word = resolve_shell_word(segment[start:end])
        if word is not None and _basename(_without_group_parens(word)) == name:
            return segment[:start] + segment[start:end].replace(name, "echo", 1) + segment[end:]
    return segment


def _inert_form(segment: str) -> str:
    """``segment`` rewritten to a form the daemon's view blanks, when it only prints.

    `jq` prints its program, so it becomes an echo of the same text; an echo, printf, grep, rg or
    jq whose only expansions are plain `$NAME` or `${NAME}` prints them, so the references are
    named `_V` and the daemon blanks the arguments. The result is only ever judged for inertness
    and searched; a line that is not all inert is judged on its raw text, never on this form.
    """
    head = _head_word(segment)
    if head == "jq":
        segment = _swap_head_for_echo(segment, "jq")
        head = "echo"
    if head is not None and (head in DATA_HEADS or head == "echo"):
        if any(marker in segment for marker in _PRINT_FREE):
            return segment
        renamed = _SIMPLE_VARIABLE.sub("_V", segment)
        return segment if "$" in renamed else renamed
    return segment


def _view_input(line: str) -> str:
    """``line`` with the print-only forms rewritten, ready for the daemon's command view."""
    line = _quote_inert_heredocs(line)
    pieces: list[str] = []
    previous_end = 0
    for start, end in segment_spans(line):
        pieces.append(line[previous_end:start])
        pieces.append(_inert_form(line[start:end]))
        previous_end = end
    pieces.append(line[previous_end:])
    return "".join(pieces)


def _inert_segments(line: str) -> list[str] | None:
    """The command-position segments of ``line`` when every one runs an inert command, else None."""
    segments = command_position_code_segments(_view_input(line))
    delimiters = set(_ANY_HEREDOC_DELIMITER.findall(line))
    if all(_segment_is_inert(segment, delimiters) for segment in segments):
        return segments
    return None


def every_command_is_inert(line: str) -> bool:
    """Whether every command ``line`` runs is inert (:data:`INERT_COMMANDS`)."""
    return _inert_segments(line) is not None


def judge_raw_command_line(line: str, search: Callable[[str], str | None]) -> str | None:
    """The text of the system change ``line`` runs, or None. THE one place a line is judged as written.

    This is the sanctioned raw-text judge (the Semgrep rule hook-regex-over-raw-command exempts a
    call of it by name, so the choice stays one visible call). The default is the original
    handler's reading: ``search`` over the whole raw text, then over each command-position segment
    with its words resolved (`d''nf install`). The exception is a line whose every command is
    inert: it is judged by those segments alone, so a message, an echo or a grep pattern that only
    names a change passes.
    """
    inert = _inert_segments(line)
    if inert is not None:
        candidates = inert
    else:
        found = search(line)
        if found is not None:
            return found
        candidates = command_position_code_segments(line)
    for segment in candidates:
        for text in (segment, _normalised(segment)):
            found = search(text)
            if found is not None:
                return found if inert is None else segment
    return None


class AnsibleEnforcementHandler(PreToolUseHandlerBase):
    """Block direct system management commands - enforce Ansible deployment.

    Blocks:
    - Package management: dnf install/remove/upgrade, rpm -i/-e, pip install, npm -g, flatpak install
    - System services: systemctl enable/start/stop/restart/disable
    - System config: gsettings set, dconf write, firewall-cmd modifications
    - User management: useradd, usermod, groupadd

    Allows:
    - Query commands: dnf info/list/search, systemctl status, gsettings get, etc.
    - Ansible playbook execution
    - Container operations (podman, docker)
    - Text that only names a blocked command, when every command on the line is inert
    """

    # Forbidden command patterns (case-insensitive)
    FORBIDDEN_PATTERNS: ClassVar[tuple[str, ...]] = _FORBIDDEN_PATTERNS

    def __init__(self) -> None:
        super().__init__(name="enforce-ansible-deployment", priority=10, terminal=True)

        # Compile patterns for performance
        self._compiled_patterns = [re.compile(pattern, re.IGNORECASE) for pattern in self.FORBIDDEN_PATTERNS]

    def _search(self, text: str) -> str | None:
        """The matched text of the first forbidden pattern in ``text`` (a quoted plain name read unquoted)."""
        text = _QUOTED_NAME.sub(r"\2", text)
        for pattern in self._compiled_patterns:
            match = pattern.search(text)
            if match:
                return match.group(0)
        return None

    def matches(self, hook_input: dict[str, Any]) -> bool:
        """Check if the Bash line names a direct system management command that runs.

        Args:
            hook_input: Hook input dict with tool_name and tool_input

        Returns:
            True if the line is a forbidden system command, False otherwise
        """
        command = get_bash_command(hook_input)
        if not command:
            return False
        return judge_raw_command_line(command, self._search) is not None

    def handle(self, hook_input: dict[str, Any]) -> GatingResult:
        """Block the operation with guidance on Ansible workflow.

        Args:
            hook_input: Hook input dict

        Returns:
            GatingResult denying under R-ANSIBLE-DIRECT-SYSTEM-CHANGE, or allow when
            the line is not a system change
        """
        command = get_bash_command(hook_input)
        offending = judge_raw_command_line(command, self._search) if command else None
        if command is None or offending is None:
            return GatingResult(decision=Decision.ALLOW)

        # Guidance follows the change the line names, not the text around it.
        category = self._categorize_command(offending)
        guidance = self._get_category_guidance(category)

        return GatingResult.deny(
            f"Command: {command}\n\n"
            f"This command modifies system state outside of version control.\n"
            f"Manual system changes create configuration drift and are not reproducible.\n\n"
            f"CORRECT APPROACH:\n"
            f"1. Create or update an Ansible playbook in playbooks/imports/\n"
            f"2. {guidance}\n"
            f"3. Run the playbook:\n"
            f"   ansible-playbook playbooks/imports/[playbook-name].yml\n\n"
            f"This ensures:\n"
            f"  - Changes are version controlled in git\n"
            f"  - Changes are documented in playbook files\n"
            f"  - Changes are reproducible on fresh installs\n"
            f"  - Configuration remains consistent\n\n"
            f"ALLOWED COMMANDS (read-only queries):\n"
            f"  - dnf info/list/search, rpm -q\n"
            f"  - systemctl status, systemctl --user\n"
            f"  - gsettings get, dconf read\n"
            f"  - flatpak list\n\n"
            f"See CLAUDE.md 'INFRASTRUCTURE AS CODE - ANSIBLE-ONLY DEPLOYMENT' for details."
        ).under_rule(_RULE)

    def get_rules(self) -> list[Rule]:
        """Return the single Rule backing this handler's deny."""
        return [_RULE]

    def get_claude_md(self) -> str | None:
        """Return CLAUDE.md guidance for this handler."""
        return (
            "## ansible_enforcement — no direct system management commands\n\n"
            "Direct package management, service management, and system configuration "
            "commands are blocked ([R-ANSIBLE-DIRECT-SYSTEM-CHANGE]). All system changes "
            "must go through Ansible playbooks. The line is judged as written; only a line "
            "whose every command is inert (echo, grep, cat, a git commit) is judged by what it "
            "runs, so a message that merely names one passes.\n\n"
            "**Blocked**: `dnf install`, `systemctl enable/start/stop`, `gsettings set`, "
            "`useradd`, `pip install` (system), `npm install -g`, `flatpak install`\n\n"
            "**Allowed** (read-only queries): `dnf info/list/search`, `systemctl status`, "
            "`gsettings get`, `flatpak list`\n\n"
            "**Use instead**: Create/update Ansible playbook in `playbooks/imports/` "
            "and deploy with `ansible-playbook`."
        )

    def get_acceptance_tests(self) -> list[AcceptanceTest]:
        """Return acceptance tests for AnsibleEnforcementHandler.

        Each denied probe is judged on its raw text. It names a package or unit that does not
        exist and answers no to every prompt (`--assumeno`), or is a shell that only parses its
        body (`bash -n`), so it changes nothing even if the guard missed it. The allowed probes
        only print or query.
        """
        return [
            AcceptanceTest(
                title="Block dnf install command",
                command="dnf install --assumeno nonexistent-pkg-zz-safe-test",
                dispatch_as_bash=True,
                description="Blocks direct package installation - must use Ansible",
                expected_decision=Decision.DENY,
                expected_message_patterns=[rf"BLOCKED \[{RULE_ID}\]", r"CORRECT APPROACH"],
                safety_notes="--assumeno answers no to the prompt and the package does not exist, so nothing installs even if the guard missed it",
                test_type=TestType.BLOCKING,
            ),
            AcceptanceTest(
                title="Block systemctl restart command",
                command="systemctl restart nonexistent-unit-zz-safe-test.service",
                dispatch_as_bash=True,
                description="Blocks direct service management - must use Ansible",
                expected_decision=Decision.DENY,
                expected_message_patterns=[rf"BLOCKED \[{RULE_ID}\]", r"systemd"],
                safety_notes="The unit does not exist, so a restart that got through fails with unit not found",
                test_type=TestType.BLOCKING,
            ),
            AcceptanceTest(
                title="Block a dnf install behind a shell, judged as written",
                command="bash -n -c 'dnf install --assumeno nonexistent-pkg-zz-safe-test'",
                dispatch_as_bash=True,
                description="A shell is not inert, so the line is judged on its raw text",
                expected_decision=Decision.DENY,
                expected_message_patterns=[rf"BLOCKED \[{RULE_ID}\]", r"CORRECT APPROACH"],
                safety_notes="bash -n only parses its body and runs nothing; the package does not exist either",
                test_type=TestType.BLOCKING,
            ),
            AcceptanceTest(
                title="Allow text that only names dnf install",
                command='echo "dnf install vim"',
                dispatch_as_bash=True,
                description="Every command on the line is inert: an echo argument is printed, not run",
                expected_decision=Decision.ALLOW,
                expected_message_patterns=[],
                safety_notes="echo prints its argument",
                test_type=TestType.BLOCKING,
            ),
            AcceptanceTest(
                title="Allow dnf info query command",
                command="dnf info nonexistent-pkg-zz-safe-test",
                dispatch_as_bash=True,
                description="Permits read-only package queries",
                expected_decision=Decision.ALLOW,
                expected_message_patterns=[],
                safety_notes="Read-only query for a package that does not exist",
                test_type=TestType.BLOCKING,
            ),
        ]

    def _categorize_command(self, command: str) -> str:
        """Determine the category of system command.

        Args:
            command: The bash command string

        Returns:
            Category string: 'package', 'service', 'config', 'user', or 'other'
        """
        command_lower = command.lower()

        if any(pkg in command_lower for pkg in ['dnf', 'yum', 'rpm', 'flatpak', 'snap', 'pip', 'npm']):
            return 'package'
        if 'systemctl' in command_lower:
            return 'service'
        if any(cfg in command_lower for cfg in ['gsettings', 'dconf', 'gconftool', 'firewall-cmd']):
            return 'config'
        if any(usr in command_lower for usr in ['useradd', 'usermod', 'userdel', 'groupadd', 'groupmod']):
            return 'user'

        return 'other'

    def _get_category_guidance(self, category: str) -> str:
        """Get Ansible-specific guidance for command category.

        Args:
            category: Command category from _categorize_command()

        Returns:
            Ansible module/approach guidance string
        """
        guidance_map = {
            'package': (
                "Use Ansible 'package' or 'dnf' module:\n"
                "     - name: Install package\n"
                "       package:\n"
                "         name: package-name\n"
                "         state: present"
            ),
            'service': (
                "Use Ansible 'systemd' module:\n"
                "     - name: Enable and start service\n"
                "       systemd:\n"
                "         name: service-name\n"
                "         state: started\n"
                "         enabled: yes"
            ),
            'config': (
                "Use Ansible 'dconf', 'command', or 'blockinfile' module:\n"
                "     - name: Configure GNOME setting\n"
                "       dconf:\n"
                "         key: /org/gnome/desktop/path\n"
                "         value: \"'value'\""
            ),
            'user': (
                "Use Ansible 'user' or 'group' module:\n"
                "     - name: Create user\n"
                "       user:\n"
                "         name: username\n"
                "         state: present"
            ),
            'other': (
                "Use appropriate Ansible module or 'command' module with proper handlers"
            ),
        }

        return guidance_map.get(category, guidance_map['other'])
