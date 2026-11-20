"""AnsibleEnforcementHandler - enforces Ansible-only system deployment.

Blocks direct package management, system configuration, and service management
commands. All system changes must go through Ansible playbooks.

The handler judges the commands a Bash line RUNS, one command-position segment at a
time (the daemon's `command_position_code_segments`). Text that only names a command
runs nothing and is not judged: an echo or printf argument, a grep or rg pattern, a
git or gh message, a heredoc body fed to a data sink, a comment, a shell that only
checks syntax. A `bash -c '...'` body, a `$(...)` substitution, a pipeline stage and a
script the line writes and then runs are judged as commands, and anything the daemon's
view cannot read stays as written, so an unreadable shape is a false positive, never a
bypass.

Two shapes the daemon's view cannot follow are judged on the line as written: a script
the line writes and then runs behind a wrapper (`sudo ./install.sh`, `timeout 9 bash s.sh`,
`bash $PWD/s.sh`), and text piped into a shell (`git log -1 --format=%s | bash`). A
computed command word (`$(echo dnf) install`) cannot be judged by any static reading and
is not.
"""

import re
import sys
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
    SHELL_NAMES,
    SOURCE_HEADS,
    command_position_code_segments,
    is_shell_command_option,
    segment_spans,
)
from claude_code_hooks_daemon.utils.shell_segmentation import (
    resolve_shell_word,
    shell_word_spans,
    strip_inert_spans,
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
        "Only a command that RUNS is judged. A commit message, an echo, a grep pattern or a\n"
        "heredoc body that names `dnf install` is not blocked; `sudo dnf install x`,\n"
        "`a && systemctl restart y` and `bash -c 'dnf install x'` are, whatever options sit\n"
        "before the verb (`dnf -y install`) and however the tool is spelled (`dnf5`,\n"
        "`/usr/bin/dnf`, behind `sudo`, `env`, `xargs`, `eval`).\n\n"
        "A script the line writes and then runs (`cat > s.sh <<EOF ... EOF` then\n"
        "`sudo ./s.sh`), and text piped into a shell, are judged as written.\n\n"
        "ALLOWED (read-only queries): dnf info/list/search, rpm -q, systemctl status,\n"
        "systemctl --user, gsettings get, dconf read, flatpak list."
    ),
)


def _tool(names: str, verbs: str, value_options: tuple[str, ...] = (), not_before_verb: str = "") -> str:
    """A pattern for ``names`` followed by any options and then one of ``verbs``.

    Options are the words that start with `-`; ``value_options`` name the ones that take
    the next word as their value (`dnf --releasever 40 install`). ``not_before_verb`` is a
    lookahead an option must not start with: the `--user` of `systemctl --user enable`.
    """
    guard = f"(?!{not_before_verb})" if not_before_verb else ""
    valued = "|".join(re.escape(option) for option in value_options)
    skip = (rf"(?:{guard}(?:{valued})\s+\S+\s+|" if valued else "(?:") + rf"{guard}-\S*\s+)*"
    return rf"\b(?:{names})\s+{skip}(?:{verbs})\b"


_RPM_VALUE_OPTIONS = r"--root|--dbpath|--prefix|--relocate|--define|-D|--macros"
_NPM_GLOBAL = r"(?:-g|--global)\b"
_NPM_VERB = r"(?:install|i|add)\b"

# Forbidden command patterns, matched case-insensitively against the text of a command that runs.
_FORBIDDEN_PATTERNS: tuple[str, ...] = (
    # Package management - install/remove operations
    _tool(
        "dnf5?",
        "install|remove|erase|update|upgrade|downgrade|reinstall",
        ("-c", "--config", "--releasever", "--enablerepo", "--disablerepo", "--repo", "--installroot", "-x", "--exclude", "--forcearch", "--setopt"),
    ),
    _tool("yum", "install|remove|erase|update|upgrade", ("-c", "--config", "--installroot", "--enablerepo", "--disablerepo", "-x", "--exclude")),
    rf"\brpm\s+(?:(?:{_RPM_VALUE_OPTIONS})\s+\S+\s+|--(?!install\b|upgrade\b|erase\b|freshen\b|query\b|verify\b)\S+\s+|-v+\s+)*"
    r"(?:-[iUeFhv]*[iUeF][iUeFhv]*|--install|--upgrade|--erase|--freshen)",
    _tool("pip3?", "install", ("--proxy", "--cache-dir", "--log", "--timeout", "--retries", "--cert", "--client-cert", "--trusted-host", "--python", "--exists-action")),
    # npm install -g, with the global flag after the verb or before it
    rf"\bnpm\s+(?:-\S*\s+)*{_NPM_VERB}[^;&|\n]*?\s+{_NPM_GLOBAL}",
    rf"\bnpm\s+(?:-\S*\s+)*?{_NPM_GLOBAL}\s+(?:-\S*\s+)*{_NPM_VERB}",
    _tool("flatpak", "install|remove|uninstall|update"),
    _tool("snap", "install|remove"),
    # System services - state changes (not queries); a --user unit is the user's own
    _tool(
        "systemctl",
        "enable|disable|start|stop|restart|reload|mask|unmask",
        ("-H", "--host", "-M", "--machine", "--root", "-t", "--type", "-p", "--property"),
        not_before_verb=r"--user\b",
    ),
    # System configuration
    _tool("gsettings", "set", ("--schemadir",)),
    _tool("dconf", "write"),
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

#: Commands that run what follows them (a wrapper): the line is judged for what they run.
_WRAPPERS: frozenset[str] = frozenset(
    {
        "sudo", "doas", "pkexec", "env", "command", "exec", "builtin", "nohup", "nice", "ionice",
        "timeout", "time", "setsid", "stdbuf", "flock", "strace", "ltrace", "xargs", "eval",
        "chroot", "unshare", "taskset", "chrt",
    }
)  # fmt: skip

#: Wrappers whose options and values never name a command, so a shell behind them is the command.
_PLAIN_WRAPPERS: frozenset[str] = frozenset({"sudo", "doas", "pkexec", "env", "command", "exec", "builtin", "nohup", "time", "setsid"})

#: Words that open a shell construct before its command: `then dnf install x`.
_RESERVED_PREFIX: frozenset[str] = frozenset({"then", "do", "else", "elif", "if", "while", "until", "!", "{", "(", "}", ")"})

_ASSIGNMENT = re.compile(r"[A-Za-z_][A-Za-z0-9_]*=.*")
_REDIRECT_TO_FILE = re.compile(r"[0-9]*(?:>>?|&>>?)(?!&)(?P<path>.*)")
_WRITERS = frozenset({"tee", "cp", "mv", "install"})
_NO_FILE = frozenset({"/dev/null", "/dev/stdout", "/dev/stderr", "/dev/tty"})
_SOURCE_NAMES = SOURCE_HEADS
_PRINT_LITERAL = re.compile(r"""print\(\s*(?:'(?P<single>[^'"\\]*)'|"(?P<double>[^'"\\]*)")\s*\)""")
_PYTHON = re.compile(r"python[0-9.]*")
_SIMPLE_VARIABLE = re.compile(r"(?<!\\)\$(?:\{[A-Za-z_][A-Za-z0-9_]*\}|[A-Za-z_][A-Za-z0-9_]*)")
_HEREDOC_OPERATOR = re.compile(r"<<(-?)[ \t]*([A-Za-z_][A-Za-z0-9_]*)(?=[\s;|&)<>]|$)")
_SHELL_VALUE_OPTIONS = frozenset({"-o", "+o", "-O", "+O"})


def _basename(word: str) -> str:
    return word.rsplit("/", 1)[-1]


def _without_group_parens(word: str) -> str:
    """``word`` without the `(` that opens a subshell and the unmatched `)` that closes one."""
    word = word.lstrip("(") if not word.startswith("$(") else word
    while word.endswith(")") and word.count(")") > word.count("("):
        word = word[:-1]
    return word


def _command_words(segment: str) -> list[str]:
    """The segment's resolved words, without a construct opener, an unreadable word as ``""``."""
    words = [resolve_shell_word(segment[start:end]) for start, end in shell_word_spans(segment)]
    plain = ["" if word is None else _without_group_parens(word) for word in words]
    while plain and (plain[0] in _RESERVED_PREFIX or not plain[0]):
        plain = plain[1:]
    return plain


def _lead_index(words: list[str]) -> int | None:
    """Index of the first word that is not a variable assignment or a construct opener."""
    for index, word in enumerate(words):
        if not (_ASSIGNMENT.fullmatch(word) or word in _RESERVED_PREFIX):
            return index
    return None


def _is_syntax_check_only(words: list[str]) -> bool:
    """Whether the segment is a shell run with `-n` (parse, never run), as plain wrappers lead it.

    A word before the shell that is not a plain wrapper, an option or an assignment means the
    shell is an argument of something else (`sudo dnf install bash -n`), so it is not read.
    """
    shell_at = None
    for index, word in enumerate(words):
        if _basename(word) in SHELL_NAMES:
            shell_at = index
            break
        if not (
            _ASSIGNMENT.fullmatch(word)
            or word.startswith("-")
            or word in _RESERVED_PREFIX
            or _basename(word) in _PLAIN_WRAPPERS
        ):
            return False
    if shell_at is None:
        return False
    index = shell_at + 1
    while index < len(words):
        word = words[index]
        if not word.startswith(("-", "+")) or word in ("-", "--", "+"):
            return False
        if word in _SHELL_VALUE_OPTIONS:
            if word == "-o" and words[index + 1 : index + 2] == ["noexec"]:
                return True
            index += 2
            continue
        if not word.startswith("--"):
            if "n" in word[1:]:
                return True
            if is_shell_command_option(word):
                return False
        index += 1
    return False


def _reads_stdin_as_shell(words: list[str]) -> bool:
    """Whether the segment is a shell that runs its stdin: no `-c` string, no script operand."""
    lead = _lead_index(words)
    if lead is None:
        return False
    shell_at = None
    for index in range(lead, len(words)):
        if _basename(words[index]) in SHELL_NAMES:
            shell_at = index
            break
        if not (words[index].startswith("-") or _basename(words[index]) in _WRAPPERS or re.fullmatch(r"[0-9]+[smhd]?", words[index])):
            return False
    if shell_at is None or _is_syntax_check_only(words):
        return False
    operands = []
    for word in words[shell_at + 1 :]:
        if word.startswith("-") and word != "-":
            if not word.startswith("--") and is_shell_command_option(word):
                return False
            continue
        operands.append(word)
    return all(operand in ("-", "/dev/stdin") for operand in operands) or any(
        word.startswith("-") and not word.startswith("--") and "s" in word[1:] for word in words[shell_at + 1 :]
    )


def _runs_shell_or_script(words: list[str], written: set[str]) -> bool:
    """Whether the segment runs a shell, `source`, or a path a command of this line wrote."""
    lead = _lead_index(words)
    if lead is None or _is_syntax_check_only(words):
        return False
    head = _basename(words[lead])
    if head in DATA_HEADS:
        return False
    if head in SHELL_NAMES or head in _SOURCE_NAMES or words[lead] in _SOURCE_NAMES:
        return True
    if _basename(words[lead]) in written:
        return True
    if head in _WRAPPERS:
        return any(
            _basename(word) in SHELL_NAMES or word in _SOURCE_NAMES or (word and _basename(word) in written)
            for word in words[lead + 1 :]
        )
    return False


def _written_names(words: list[str]) -> set[str]:
    """Basenames of the files one segment writes: a redirect target, `tee`, `cp`, `mv`, `install`."""
    names: set[str] = set()
    for index, word in enumerate(words):
        match = _REDIRECT_TO_FILE.fullmatch(word)
        if match is None:
            continue
        target = match.group("path") or (words[index + 1] if index + 1 < len(words) else "")
        if target not in _NO_FILE:
            names.add(_basename(target))
    lead = _lead_index(words)
    if lead is None:
        return names
    command = words[lead:]
    while command and _basename(command[0]) in _PLAIN_WRAPPERS:
        command = command[1:]
        while command and command[0].startswith("-"):
            command = command[1:]
    if command and _basename(command[0]) in _WRITERS:
        operands = [word for word in command[1:] if word and not word.startswith("-")]
        if _basename(command[0]) == "tee":
            names.update(_basename(word) for word in operands)
        elif operands:
            names.add(_basename(operands[-1]))
    return names


def _joined_by_pipe(command: str, previous_end: int, start: int) -> bool:
    return command[previous_end:start].strip() in ("|", "|&")


def _runs_what_the_daemons_view_cannot_follow(command: str) -> bool:
    """Whether the line writes a file and then runs a shell or script, or pipes into a shell.

    The daemon's view recognises a written script only when it is run bare with the path
    spelled as written; behind a wrapper the heredoc body it blanked as data is lost. Text piped
    into a shell is whatever the earlier stages print. Either way the line is judged as written.
    """
    stripped = strip_inert_spans(command)
    written: set[str] = set()
    wrote_any = False
    previous_end: int | None = None
    for start, end in segment_spans(stripped):
        words = _command_words(stripped[start:end])
        if previous_end is not None and _joined_by_pipe(stripped, previous_end, start) and _reads_stdin_as_shell(words):
            return True
        if wrote_any and _runs_shell_or_script(words, written):
            return True
        names = _written_names(words)
        if names or any(_REDIRECT_TO_FILE.fullmatch(word) and word not in _NO_FILE for word in words):
            wrote_any = True
        written |= names
        previous_end = end
    return False


def _quote_inert_heredocs(text: str) -> str:
    """``text`` with each unquoted heredoc whose body substitutes nothing made a quoted one.

    An unquoted `<<EOF` body is expanded by the shell, so the daemon keeps it as text that may
    execute. A heredoc with no `$(` and no grave accent is plain data, so giving its delimiter
    hands the daemon the same body to judge by its own rules (a data sink's body is blanked,
    a shell's body is read as commands, a pipe into a shell keeps it). The text is only
    judged, never executed, so the rewrite changes no behaviour.
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


def _inert_form(segment: str) -> str:
    """``segment`` rewritten to a form the daemon's view blanks, when what it holds cannot run.

    - a shell run with `-n` only parses its body: it becomes an echo;
    - `jq` and `python -c "print('literal')"` print their program or literal: they become an echo
      of the same text, so a pipe into a shell keeps the text exactly as before;
    - an echo, printf, grep, rg or jq whose only expansions are plain `$NAME` or `${NAME}`
      prints them, so the references are named `_V` and the daemon blanks the arguments.
    """
    words = _command_words(segment)
    lead = _lead_index(words)
    if lead is None:
        return segment
    if _is_syntax_check_only(words):
        return " echo _ "
    head = _basename(words[lead])
    spans = shell_word_spans(segment)
    if head == "jq" and len(spans) > lead:
        segment = segment[: spans[lead][0]] + "echo" + segment[spans[lead][1] :]
        head = "echo"
    elif _PYTHON.fullmatch(head) and words[lead + 1 :] == ["-c", words[-1]] and (match := _PRINT_LITERAL.fullmatch(words[-1])):
        return f" echo '{match.group('single') or match.group('double') or ''}' "
    if head in DATA_HEADS or head == "echo":
        if any(marker in segment for marker in ("$(", "`", "<(", ">(")):
            return segment
        renamed = _SIMPLE_VARIABLE.sub("_V", segment)
        return segment if "$" in renamed else renamed
    return segment


def _view_input(command: str) -> str:
    """``command`` with the inert forms rewritten, ready for the daemon's command view."""
    command = _quote_inert_heredocs(command)
    pieces: list[str] = []
    previous_end = 0
    for start, end in segment_spans(command):
        pieces.append(command[previous_end:start])
        pieces.append(_inert_form(command[start:end]))
        previous_end = end
    pieces.append(command[previous_end:])
    return "".join(pieces)


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
    - Text that only names a blocked command (a message, an echo, a search pattern)
    """

    # Forbidden command patterns (case-insensitive)
    FORBIDDEN_PATTERNS: ClassVar[tuple[str, ...]] = _FORBIDDEN_PATTERNS

    def __init__(self) -> None:
        super().__init__(name="enforce-ansible-deployment", priority=10, terminal=True)

        # Compile patterns for performance
        self._compiled_patterns = [re.compile(pattern, re.IGNORECASE) for pattern in self.FORBIDDEN_PATTERNS]

    def _search(self, text: str) -> re.Match[str] | None:
        """The first forbidden pattern found in ``text`` (a quoted plain name read unquoted)."""
        text = _QUOTED_NAME.sub(r"\2", text)
        for pattern in self._compiled_patterns:
            match = pattern.search(text)
            if match:
                return match
        return None

    def _offending_text(self, command: str) -> str | None:
        """The text of the system change ``command`` runs, or None when it runs none.

        A command-position segment that changes the system, else the matched text of the line
        as written when it runs a script it wrote or a shell fed by a pipe.
        """
        for segment in command_position_code_segments(_view_input(command)):
            if self._search(segment):
                return segment
        if _runs_what_the_daemons_view_cannot_follow(command):
            match = self._search(command)
            if match:
                return match.group(0)
        return None

    def matches(self, hook_input: dict[str, Any]) -> bool:
        """Check if the command line RUNS a direct system management command.

        Args:
            hook_input: Hook input dict with tool_name and tool_input

        Returns:
            True if a command that runs is a forbidden system command, False otherwise
        """
        command = get_bash_command(hook_input)
        if not command:
            return False
        return self._offending_text(command) is not None

    def handle(self, hook_input: dict[str, Any]) -> GatingResult:
        """Block the operation with guidance on Ansible workflow.

        Args:
            hook_input: Hook input dict

        Returns:
            GatingResult denying under R-ANSIBLE-DIRECT-SYSTEM-CHANGE, or allow when
            no command that runs is a system change
        """
        command = get_bash_command(hook_input)
        offending = self._offending_text(command) if command else None
        if command is None or offending is None:
            return GatingResult(decision=Decision.ALLOW)

        # Guidance follows the command that runs, not the text around it.
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
            "must go through Ansible playbooks. Only a command that runs is judged: text "
            "that names one (a commit message, an echo, a grep pattern) is not blocked.\n\n"
            "**Blocked**: `dnf install`, `systemctl enable/start/stop`, `gsettings set`, "
            "`useradd`, `pip install` (system), `npm install -g`, `flatpak install`\n\n"
            "**Allowed** (read-only queries): `dnf info/list/search`, `systemctl status`, "
            "`gsettings get`, `flatpak list`\n\n"
            "**Use instead**: Create/update Ansible playbook in `playbooks/imports/` "
            "and deploy with `ansible-playbook`."
        )

    def get_acceptance_tests(self) -> list[AcceptanceTest]:
        """Return acceptance tests for AnsibleEnforcementHandler.

        Each denied probe is a command that RUNS a system change, so the verdict is a real-run
        judgement. It names a package or unit that does not exist and answers no to every prompt
        (`--assumeno`), so it changes nothing even if the guard missed it. The allowed probes
        only print, query, or parse.
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
                title="Allow text that only names dnf install",
                command='echo "dnf install vim"',
                dispatch_as_bash=True,
                description="An echo argument is printed, not run",
                expected_decision=Decision.ALLOW,
                expected_message_patterns=[],
                safety_notes="echo prints its argument",
                test_type=TestType.BLOCKING,
            ),
            AcceptanceTest(
                title="Allow a shell that only checks syntax",
                command="bash -n -c 'dnf install nonexistent-pkg-zz-safe-test'",
                dispatch_as_bash=True,
                description="bash -n parses its body and runs nothing, so a body that names dnf install is not a system change",
                expected_decision=Decision.ALLOW,
                expected_message_patterns=[],
                safety_notes="bash -n only parses its body; the package does not exist either",
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
