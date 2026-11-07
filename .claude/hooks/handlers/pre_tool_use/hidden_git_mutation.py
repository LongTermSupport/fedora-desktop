"""HiddenGitMutationHandler - an untracked script holding git mutations is not run.

The command guards (destructive_git, the branch force-delete, the worktree-removal guards, the
commit gates, the WIP cap) judge the Bash command line. A script file run from that line carries
its git commands past them unseen. A committed script is reviewed and QA-gated; an uncommitted
one (scratch, a temporary file, anything gitignored, one only staged) is neither, so running one
that holds a git mutation is denied (Plan 00250, TRIAGE cause 1).

What it does not see, by design: git or a verb computed at run time (`"$tool" "$verb"`), a file
the command copies into place before running it, `bash <(cat f)`, `BASH_ENV`, and runners it does
not name. The guard catches what a careless agent writes, not what is built to evade it.
"""

from __future__ import annotations

import os
import re
import shlex
import subprocess
import sys
from pathlib import Path
from typing import Any

sys.path.insert(0, str(Path(__file__).parent.parent.parent / "hooks-daemon/src"))

from claude_code_hooks_daemon.core import Decision, GatingResult
from claude_code_hooks_daemon.core.acceptance_test import AcceptanceTest, TestType
from claude_code_hooks_daemon.core.handler_bases import PreToolUseHandlerBase
from claude_code_hooks_daemon.core.rule import Rule
from claude_code_hooks_daemon.core.utils import get_bash_command
from claude_code_hooks_daemon.utils.command_evasion import GIT_INVOCATION
from claude_code_hooks_daemon.utils.command_position import command_position_segments

RULE_ID = "R-UNTRACKED-SCRIPT-GIT-MUTATION"

_SHELLS = frozenset({"bash", "sh", "dash", "zsh", "ksh"})
_INTERPRETER = re.compile(r"^(?:python(?:\d+(?:\.\d+)?)?|perl|ruby)$")
# Interpreter options that take a value, and those that mean the program is not a file.
_INTERPRETER_OPTIONS_WITH_VALUE = frozenset({"-W", "-X", "-I"})
_INTERPRETER_INLINE = frozenset({"-c", "-m", "-e", "-E"})
# `bash -c "$(cat f)"`, `eval "$(< f)"`: the script's text is substituted in, unseen.
_SUBSTITUTED_FILE = re.compile(r"\b(?:bash|sh|eval)\b[^;&|\n]*\$\(\s*(?:cat\s+|<\s*)([^\s)$]+)\s*\)")
_SOURCE_WORDS = frozenset({"source", "."})
# Words that run the command after them: (their options that take a value, their positionals).
_WRAPPERS: dict[str, tuple[frozenset[str], int]] = {
    "env": (frozenset({"-u", "-C", "--unset", "--chdir"}), 0),
    "sudo": (frozenset({"-u", "-g", "-C", "-D", "-h", "-p", "-r", "-t", "-U"}), 0),
    "timeout": (frozenset({"-s", "-k", "--signal", "--kill-after"}), 1),
    "nice": (frozenset({"-n", "--adjustment"}), 0),
    "ionice": (frozenset({"-c", "-n", "-p"}), 0),
    "stdbuf": (frozenset({"-i", "-o", "-e"}), 0),
    "flock": (frozenset({"-w", "-E", "--timeout", "--conflict-exit-code"}), 1),
    "xargs": (frozenset({"-n", "-I", "-P", "-L", "-d", "-a", "-s", "-E"}), 0),
    "exec": (frozenset({"-a"}), 0),
    "nohup": (frozenset(), 0),
    "command": (frozenset(), 0),
    "time": (frozenset(), 0),
    "setsid": (frozenset(), 0),
}
_SHELL_OPTIONS_WITH_VALUE = frozenset({"-o", "+o", "-O", "+O"})
_ASSIGNMENT = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*=")
_QUOTED_GIT = re.compile(r"""(?<![\w/.-])(["'])git\1(?=\s)""")
_MAX_SCRIPT_BYTES = 2_000_000
_GIT_TIMEOUT_SECONDS = 5
_MAX_DEPTH = 3

# A git subcommand that changes the index, the work tree, a ref, history or a remote. Left out
# as hazard-free: the read-only verbs, fetch (it moves only remote-tracking refs), and a listing
# form of stash, worktree, branch and tag.
_SHELL_VERBS = (
    r"(?:(?:add|am|apply|checkout|cherry-pick|clean|commit|filter-branch|filter-repo|merge|mv|pull"
    r"|push|rebase|reset|restore|revert|rm|switch|update-index|update-ref)(?![\w-])"
    r"|stash(?![\w-])(?!\s+(?:list|show)\b)"
    r"|worktree\s+(?:add|remove|move|prune)(?![\w-])"
    r"|reflog\s+(?:expire|delete)(?![\w-])"
    r"|gc(?![\w-])[^\n;&|]*--prune"
    r"|(?:branch|tag)[ \t]+(?![-\s])"
    r"|branch(?![\w-])[^\n;&|]*?\s(?:-[A-Za-z]*[dDfmMc][A-Za-z]*|--delete|--force|--move|--copy)(?=\s|$)"
    r"|tag(?![\w-])[^\n;&|]*?\s(?:-[A-Za-z]*[dfas][A-Za-z]*|--delete|--force)(?=\s|$))"
)
_MUTATION = re.compile(GIT_INVOCATION + _SHELL_VERBS)
_DRY_RUN = re.compile(r"(?<![\w-])--dry-run\b")
# The same verbs as a subprocess argument list: ["git", "-C", d, "--no-pager", "push", ...].
_Q = r"""['"]"""
_ARGV_MUTATION = re.compile(
    rf"{_Q}git{_Q}\s*,\s*(?:{_Q}-[Cc]{_Q}\s*,\s*[^,\]]+,\s*|{_Q}--?[\w-]+(?:=[^'\"]*)?{_Q}\s*,\s*)*"
    rf"(?:{_Q}(?:add|am|apply|checkout|cherry-pick|clean|commit|filter-branch|filter-repo|merge|mv"
    rf"|pull|push|rebase|reset|restore|revert|rm|switch|update-index|update-ref){_Q}"
    rf"|{_Q}stash{_Q}(?!\s*,\s*{_Q}(?:list|show){_Q})"
    rf"|{_Q}worktree{_Q}\s*,\s*{_Q}(?:add|remove|move|prune){_Q}"
    rf"|{_Q}reflog{_Q}\s*,\s*{_Q}(?:expire|delete){_Q}"
    rf"|{_Q}(?:branch|tag){_Q}\s*,\s*{_Q}(?:-d|-D|-f|-m|-M|--delete|--force|--move){_Q})"
)

_SHELL_GIT_VARIABLE = re.compile(r"""(?<![\w-])([A-Za-z_]\w*)=["']?(?:\S*/)?git["']?(?=[\s;]|$)""", re.MULTILINE)
_SHELL_GIT_FUNCTION = re.compile(r"""(?<![\w-])([A-Za-z_][\w-]*)\s*\(\)\s*\{[^}]*\bgit\b[^}]*"\$@\"""")
_PYTHON_DYNAMIC_GIT = re.compile(rf"""\[\s*{_Q}git{_Q}\s*(?:,\s*\*|\]\s*\+)""")
_PYTHON_VERB_ARGUMENT = re.compile(
    rf"""\w+\([^()]*?{_Q}(?:add|am|apply|checkout|cherry-pick|clean|commit|merge|mv|pull|push|rebase"""
    rf"""|reset|restore|revert|rm|switch|update-ref){_Q}"""
)

_RULE = Rule(
    rule_id=RULE_ID,
    blocked="running an uncommitted script file whose body holds a git mutation",
    why="The git command guards judge only the Bash command line and cannot see inside a script",
    fix="Run each git command directly as its own Bash call, or make the script a committed tool",
    verbose=(
        "WHY: destructive_git, the branch and worktree guards, the WIP cap and the commit gates\n"
        "read the Bash command line. Every git command inside a script file runs past them unseen.\n"
        "A committed script is reviewed and QA-gated; an uncommitted one (scratch, /tmp,\n"
        "gitignored, only staged) is not.\n\n"
        "DO INSTEAD:\n"
        "  1. Run each git command directly, one Bash call at a time, so the guards judge it.\n"
        "  2. Or make the script a tracked tool: commit it where QA gates it, then run it."
    ),
)


def _tokens(segment: str) -> list[str]:
    try:
        tokens = shlex.split(segment, comments=True)
    except ValueError:
        tokens = segment.split()
    # A subshell or group: `(cd D && bash f)`, `{ bash f; }`.
    while tokens and tokens[0] in ("(", "{", "!"):
        tokens = tokens[1:]
    if tokens and tokens[0].startswith("(") and not tokens[0].startswith("(("):
        tokens = [tokens[0][1:], *tokens[1:]] if len(tokens[0]) > 1 else tokens[1:]
    while tokens and tokens[-1] in (")", "}"):
        tokens = tokens[:-1]
    if tokens and tokens[-1].endswith(")") and "(" not in tokens[-1]:
        tokens = [*tokens[:-1], tokens[-1][:-1]]
    return tokens


def _skip_options(tokens: list[str], index: int, with_value: frozenset[str]) -> int:
    while index < len(tokens) and tokens[index].startswith("-") and tokens[index] != "-":
        option = tokens[index]
        index += 2 if option in with_value else 1
        if option == "--":
            break
    return index


def _strip_wrappers(tokens: list[str]) -> list[str]:
    index = 0
    while index < len(tokens):
        word = tokens[index]
        if _ASSIGNMENT.match(word):
            index += 1
        elif word in _WRAPPERS:
            with_value, positionals = _WRAPPERS[word]
            index = _skip_options(tokens, index + 1, with_value) + positionals
        else:
            break
    return tokens[index:]


def _script_operand(tokens: list[str]) -> str | None:
    """The script file this one command runs, or None when it runs none."""
    tokens = _strip_wrappers(tokens)
    if not tokens:
        return None
    word = tokens[0]
    if word in _SOURCE_WORDS:
        return tokens[1] if len(tokens) > 1 else None
    name = os.path.basename(word)
    if name in _SHELLS:
        return _shell_operand(tokens)
    if _INTERPRETER.match(name):
        if any(arg in _INTERPRETER_INLINE for arg in tokens[1:]):
            return None
        index = _skip_options(tokens, 1, _INTERPRETER_OPTIONS_WITH_VALUE)
        return tokens[index] if index < len(tokens) else None
    if "/" in word:
        return word
    return None


def _shell_operand(tokens: list[str]) -> str | None:
    """The file a shell runs: its script operand, a `-c` body's script, or `< file` stdin.

    `-n` reads the script without running it, so it runs none.
    """
    index = 1
    while index < len(tokens):
        arg = tokens[index]
        single = arg.startswith("-") and not arg.startswith("--")
        if arg == "-c" or (single and "c" in arg[1:]):
            return _script_operand(tokens[index + 1 :])
        if single and "n" in arg[1:]:
            return None
        if arg in ("--", "<"):
            return tokens[index + 1] if index + 1 < len(tokens) else None
        if arg.startswith("<") and not arg.startswith(("<<", "<(")):
            return arg[1:]
        if arg in _SHELL_OPTIONS_WITH_VALUE:
            index += 2
            continue
        if arg.startswith(("-", "+")):
            index += 1
            continue
        return arg
    return None


def _is_bare_shell(tokens: list[str]) -> bool:
    tokens = _strip_wrappers(tokens)
    return bool(tokens) and os.path.basename(tokens[0]) in _SHELLS and _shell_operand(tokens) is None


def _changed_directory(tokens: list[str], cwd: str) -> str:
    """The directory a `cd`/`pushd` segment leaves the shell in."""
    if not tokens or tokens[0] not in ("cd", "pushd"):
        return cwd
    operands = [arg for arg in tokens[1:] if arg not in ("-P", "-L", "-e", "--")]
    if len(operands) == 1 and "$" not in operands[0] and operands[0] != "-":
        return os.path.normpath(os.path.join(cwd, os.path.expanduser(operands[0])))
    return cwd


def _operands(command: str, cwd: str) -> list[tuple[str, str]]:
    """(directory, operand) for each script file the command names to run."""
    operands: list[tuple[str, str]] = [(cwd, match) for match in _SUBSTITUTED_FILE.findall(command)]
    previous: list[str] = []
    for segment in command_position_segments(command):
        tokens = _tokens(segment)
        cwd = _changed_directory(tokens, cwd)
        operand = _script_operand(tokens)
        # `cat f | bash`: the file before the pipe is the script.
        if operand is None and _is_bare_shell(tokens) and len(previous) == 2 and previous[0] == "cat":
            operand = previous[1]
        if operand is not None and "$" not in operand:
            operands.append((cwd, operand))
        previous = tokens
    return operands


def _absolute(directory: str, operand: str) -> str:
    path = os.path.join(directory, os.path.expanduser(operand))
    if not os.path.isabs(path):
        raise ValueError(f"hidden_git_mutation: {path!r} is not absolute; the hook cwd must be")
    return os.path.realpath(path)


def _is_committed(path: str) -> bool:
    """Whether the file is in its repository's HEAD. Outside any repository, or on any error, no."""
    result = subprocess.run(  # FAIL-FAST-OK: a non-zero answer means uncommitted, read below
        ["git", "-C", os.path.dirname(path), "cat-file", "-e", f"HEAD:./{os.path.basename(path)}"],
        capture_output=True,
        check=False,
        timeout=_GIT_TIMEOUT_SECONDS,
    )
    return result.returncode == 0


def _is_python(path: str, text: str) -> bool:
    return path.endswith(".py") or (text.startswith("#!") and "python" in text.split("\n", 1)[0])


def _git_aliases(text: str) -> re.Pattern[str] | None:
    """The names shell text gives git: `G=git` (used as `$G`) and `g() { git "$@"; }`."""
    names = [rf"\$\{{?{name}\}}?" for name in _SHELL_GIT_VARIABLE.findall(text)]
    names += [rf"(?<![\w$-]){name}" for name in _SHELL_GIT_FUNCTION.findall(text)]
    if not names:
        return None
    return re.compile(rf"""(?:{"|".join(names)})["']?\s+(?:-\S+(?:\s+(?!-)\S+)?\s+)*?{_SHELL_VERBS}""")


def _shell_mutations(text: str) -> list[str]:
    """The commands in shell text that are git mutations, comments and data left out."""
    lines = [line for line in text.splitlines() if not line.lstrip().startswith("#")]
    joined = _QUOTED_GIT.sub("git", "\n".join(lines).replace("\\\n", " "))
    alias = _git_aliases(joined)
    found = []
    for segment in command_position_segments(joined):
        hit = _MUTATION.search(segment) or (alias is not None and alias.search(segment))
        if hit and not _DRY_RUN.search(segment):
            found.append(" ".join(segment.split()))
    return found


def _python_mutations(text: str) -> list[str]:
    found = [" ".join(match.group(0).split()) for match in _ARGV_MUTATION.finditer(text)]
    # git called through a helper that spreads its arguments: `["git", *args]`, `["git"] + a`.
    if _PYTHON_DYNAMIC_GIT.search(text):
        found += [" ".join(match.group(0).split()) for match in _PYTHON_VERB_ARGUMENT.finditer(text)]
    for line in text.splitlines():
        if not line.lstrip().startswith("#") and _MUTATION.search(_QUOTED_GIT.sub("git", line)):
            found.append(line.strip())
    return found


def _read_script(path: str) -> str | None:
    if not os.path.isfile(path) or os.path.getsize(path) > _MAX_SCRIPT_BYTES:
        return None
    data = Path(path).read_bytes()
    if b"\0" in data:
        return None
    return data.decode("utf-8", errors="replace")


class HiddenGitMutationHandler(PreToolUseHandlerBase):
    """Deny running an uncommitted script whose text holds a git mutation.

    Covered spellings: `bash X`, `sh X`, `./X`, `path/X`, `source X`, `. X`, `python3 X`,
    `perl X`, `bash < X`, `cat X | bash`, `bash -c "$(cat X)"`, behind `env`, `sudo`,
    `timeout`, `nice`, `nohup`, `stdbuf`, `flock`, `xargs` and assignments, after a `cd`, in a
    subshell, inside `bash -c '...'`, and a script the run script itself runs. Git verbs are
    found as shell commands, across line continuations, and as subprocess argument lists. A
    committed script is allowed: it is reviewed and QA-gated. A script the same command writes
    and then runs is not this handler's: the written text is on the command line, and the
    daemon's command view already judges it as commands.
    """

    def __init__(self) -> None:
        super().__init__(name="hidden-git-mutation", priority=11, terminal=True)

    def _offending(self, hook_input: dict[str, Any]) -> list[tuple[str, list[str]]]:
        command = get_bash_command(hook_input)
        cwd = hook_input.get("cwd")
        if not command:
            return []
        if not isinstance(cwd, str) or not os.path.isabs(cwd):
            cwd = os.environ.get("CLAUDE_PROJECT_DIR") or str(Path(__file__).resolve().parents[4])
        offending: list[tuple[str, list[str]]] = []
        seen: set[str] = set()
        pending = [(_absolute(directory, operand), 0) for directory, operand in _operands(command, cwd)]
        while pending:
            script, depth = pending.pop(0)
            if script in seen:
                continue
            seen.add(script)
            text = _read_script(script)
            if text is None:
                continue
            if _is_committed(script):
                # A committed tool, and what it runs, are reviewed and QA-gated.
                continue
            python = _is_python(script, text)
            lines = _python_mutations(text) if python else _shell_mutations(text)
            if lines:
                offending.append((script, lines))
            if not python and depth < _MAX_DEPTH:
                for directory in dict.fromkeys((cwd, os.path.dirname(script))):
                    pending.extend(
                        (_absolute(directory, operand), depth + 1) for _d, operand in _operands(text, directory)
                    )
        return offending

    def matches(self, hook_input: dict[str, Any]) -> bool:
        return bool(self._offending(hook_input))

    def handle(self, hook_input: dict[str, Any]) -> GatingResult:
        offending = self._offending(hook_input)
        if not offending:
            return GatingResult.allow()
        cwd = str(hook_input.get("cwd") or "")
        parts = []
        for script, lines in offending:
            shown = os.path.relpath(script, cwd) if cwd and script.startswith(cwd + os.sep) else script
            listed = "\n".join(f"    {line}" for line in lines[:8])
            more = f"\n    ... and {len(lines) - 8} more" if len(lines) > 8 else ""
            parts.append(f"  {shown} is not committed to git and holds:\n{listed}{more}")
        reason = (
            "Running an uncommitted script that holds git mutations.\n\n"
            + "\n".join(parts)
            + "\n\nThe git guards judge only the Bash command line: they cannot see inside a script.\n\n"
            + _RULE.verbose
        )
        return GatingResult.deny(reason).under_rule(_RULE)

    def get_rules(self) -> list[Rule]:
        return [_RULE]

    def get_claude_md(self) -> str | None:
        return (
            "## hidden_git_mutation — no git mutations hidden in an uncommitted script\n\n"
            "Running an uncommitted script file (`bash X`, `./X`, `source X`, `python3 X`) whose "
            "text holds a git mutation (add, commit, push, reset, checkout, branch -d, worktree "
            "add/remove, ...) is denied: the git guards cannot see inside it. Run each git "
            "command directly, or commit the script as a tracked tool."
        )

    def get_acceptance_tests(self) -> list[AcceptanceTest]:
        script = "untracked/scratch/acceptance-hidden-git-mutation.bash"
        return [
            AcceptanceTest(
                title="Block an uncommitted script holding a git mutation",
                command=f"bash {script}",
                description="An uncommitted script whose text holds `git commit` is not run",
                expected_decision=Decision.DENY,
                expected_message_patterns=[rf"BLOCKED \[{RULE_ID}\]", r"cannot see inside a script"],
                safety_notes="The script exits before its git line, and it is denied before it runs",
                setup_commands=[f"printf '#!/usr/bin/env bash\\nexit 0\\ngit commit -m x\\n' > {script}"],
                cleanup_commands=[f"rm -f {script}"],
                test_type=TestType.BLOCKING,
            ),
            AcceptanceTest(
                title="Allow an uncommitted script with no git mutation",
                command=f"bash {script}",
                description="An uncommitted script that only reads is left alone",
                expected_decision=Decision.ALLOW,
                expected_message_patterns=[],
                safety_notes="The script only echoes",
                setup_commands=[f"printf '#!/usr/bin/env bash\\necho hi\\ngit status --short\\n' > {script}"],
                cleanup_commands=[f"rm -f {script}"],
                test_type=TestType.BLOCKING,
            ),
        ]
