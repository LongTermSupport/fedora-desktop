"""HiddenGitMutationHandler - an untracked script holding git mutations is not run.

The command guards (destructive_git, the branch force-delete, the worktree-removal guards, the
commit gates) judge the Bash command line. A script file run from that line carries its git
commands past them unseen. A tracked script is reviewed and QA-gated; an untracked one (scratch,
a temporary file, anything gitignored) is neither, so running one that holds a git mutation is
denied (Plan 00250, TRIAGE cause 1).
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
_PYTHON = re.compile(r"^python(?:\d+(?:\.\d+)?)?$")
# `bash -c "$(cat f)"`, `eval "$(< f)"`: the script's text is substituted in, unseen.
_SUBSTITUTED_FILE = re.compile(r"\b(?:bash|sh|eval)\b[^;&|\n]*\$\(\s*(?:cat\s+|<\s*)([^\s)$]+)\s*\)")
_SOURCE_WORDS = frozenset({"source", "."})
# Words that run the command after them: (word, how many of their own arguments to skip).
_WRAPPERS = {"env": 0, "sudo": 0, "nohup": 0, "exec": 0, "command": 0, "time": 0, "setsid": 0, "nice": 0, "timeout": 1}
_SHELL_OPTIONS_WITH_VALUE = frozenset({"-o", "+o", "-O", "+O"})
_ASSIGNMENT = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*=")
_MAX_SCRIPT_BYTES = 2_000_000
_GIT_TIMEOUT_SECONDS = 5

# A git subcommand that changes the index, the work tree, a ref or a remote.
_MUTATION = re.compile(
    GIT_INVOCATION
    + r"(?P<verb>add|commit|push|reset|checkout|switch|restore|rm|merge|rebase|update-ref|clean"
    r"|stash(?!\s+(?:list|show)\b)"
    r"|worktree\s+(?:add|remove)"
    r"|branch\b[^\n;&|]*?\s(?:-[A-Za-z]*[dD][A-Za-z]*|--delete)(?=\s|$))\b"
)
# The same verbs as a subprocess argument list: ["git", "-C", d, "push", ...].
_ARGV_MUTATION = re.compile(
    r"""['"]git['"]\s*,\s*(?:['"]-[Cc]['"]\s*,\s*[^,\]]+,\s*)*"""
    r"""(?:['"](?:add|commit|push|reset|checkout|switch|restore|rm|merge|rebase|update-ref|clean)['"]"""
    r"""|['"]stash['"](?!\s*,\s*['"](?:list|show)['"])"""
    r"""|['"]worktree['"]\s*,\s*['"](?:add|remove)['"]"""
    r"""|['"]branch['"]\s*,\s*['"](?:-d|-D|--delete)['"])"""
)

_RULE = Rule(
    rule_id=RULE_ID,
    blocked="running an untracked script file whose body holds a git mutation",
    why="The git command guards judge only the Bash command line and cannot see inside a script",
    fix="Run each git command directly as its own Bash call, or make the script a tracked tool",
    verbose=(
        "WHY: destructive_git, the branch and worktree guards and the commit gates read the Bash\n"
        "command line. Every git command inside a script file runs past them unseen. A tracked\n"
        "script is reviewed and QA-gated; an untracked one (scratch, /tmp, gitignored) is not.\n\n"
        "DO INSTEAD:\n"
        "  1. Run each git command directly, one Bash call at a time, so the guards judge it.\n"
        "  2. Or make the script a tracked tool: commit it where QA gates it, then run it."
    ),
)


def _tokens(segment: str) -> list[str]:
    try:
        return shlex.split(segment, comments=True)
    except ValueError:
        return segment.split()


def _strip_wrappers(tokens: list[str]) -> list[str]:
    index = 0
    while index < len(tokens):
        word = tokens[index]
        if _ASSIGNMENT.match(word):
            index += 1
        elif word in _WRAPPERS:
            index += 1 + _WRAPPERS[word]
            while index < len(tokens) and tokens[index].startswith("-"):
                index += 1
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
    if _PYTHON.match(name):
        operands = [arg for arg in tokens[1:] if not arg.startswith("-")]
        if any(arg in ("-c", "-m") for arg in tokens[1:]) or not operands:
            return None
        return operands[0]
    if "/" in word:
        return word
    return None


def _shell_operand(tokens: list[str]) -> str | None:
    """The file a shell runs: its script operand, a `-c` body's script, or `< file` stdin."""
    index = 1
    while index < len(tokens):
        arg = tokens[index]
        if arg == "-c" or (arg.startswith("-") and not arg.startswith("--") and "c" in arg[1:]):
            return _script_operand(tokens[index + 1 :])
        if arg == "--":
            return tokens[index + 1] if index + 1 < len(tokens) else None
        if arg == "<":
            return tokens[index + 1] if index + 1 < len(tokens) else None
        if arg.startswith("<") and not arg.startswith("<<"):
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
    if len(tokens) == 2 and tokens[0] in ("cd", "pushd") and "$" not in tokens[1]:
        return os.path.normpath(os.path.join(cwd, os.path.expanduser(tokens[1])))
    return cwd


def _scripts_run(command: str, cwd: str) -> list[str]:
    """Absolute paths of the existing script files the command runs."""
    operands: list[tuple[str, str]] = [(cwd, match) for match in _SUBSTITUTED_FILE.findall(command)]
    previous: list[str] = []
    for segment in command_position_segments(command):
        tokens = _tokens(segment)
        cwd = _changed_directory(tokens, cwd)
        operand = _script_operand(tokens)
        # `cat f | bash`: the file before the pipe is the script.
        if operand is None and _is_bare_shell(tokens) and len(previous) == 2 and previous[0] == "cat":
            operand = previous[1]
        if operand is not None:
            operands.append((cwd, operand))
        previous = tokens
    scripts: list[str] = []
    for directory, operand in operands:
        if "$" in operand:
            continue
        path = os.path.join(directory, os.path.expanduser(operand))
        if not os.path.isabs(path):
            raise ValueError(f"hidden_git_mutation: {path!r} is not absolute; the hook cwd must be")
        if os.path.isfile(path):
            scripts.append(os.path.realpath(path))
    return scripts


def _is_tracked(path: str) -> bool:
    """Whether git tracks the file. Outside any repository, or on any git error, it is not."""
    result = subprocess.run(  # FAIL-FAST-OK: a non-zero answer means untracked, read below
        ["git", "-C", os.path.dirname(path), "ls-files", "--error-unmatch", "--", os.path.basename(path)],
        capture_output=True,
        check=False,
        timeout=_GIT_TIMEOUT_SECONDS,
    )
    return result.returncode == 0


def _mutations(path: str) -> list[str]:
    """The git mutation lines the script holds, whole-line comments left out."""
    if os.path.getsize(path) > _MAX_SCRIPT_BYTES:
        return []
    data = Path(path).read_bytes()
    if b"\0" in data:
        return []
    found: list[str] = []
    for line in data.decode("utf-8", errors="replace").splitlines():
        if line.lstrip().startswith("#"):
            continue
        if _MUTATION.search(line) or _ARGV_MUTATION.search(line):
            found.append(line.strip())
    return found


class HiddenGitMutationHandler(PreToolUseHandlerBase):
    """Deny running an untracked script whose text holds a git mutation verb.

    Covered spellings: `bash X`, `sh X`, `./X`, `path/X`, `source X`, `. X`, `python3 X`,
    `bash < X`, `cat X | bash`, `bash -c "$(cat X)"`, behind `env`, `timeout`, `nohup`,
    assignments, a `cd`, or inside `bash -c '...'`. Git verbs are found as shell commands and
    as subprocess argument lists. A tracked script is allowed: it is reviewed and QA-gated.
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
        offending = []
        for script in _scripts_run(command, cwd):
            lines = _mutations(script)
            if lines and not _is_tracked(script):
                offending.append((script, lines))
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
            parts.append(f"  {shown} is not tracked by git and holds:\n{listed}{more}")
        reason = (
            "Running an untracked script that holds git mutations.\n\n"
            + "\n".join(parts)
            + "\n\nThe git guards judge only the Bash command line: they cannot see inside a script.\n\n"
            + _RULE.verbose
        )
        return GatingResult.deny(reason).under_rule(_RULE)

    def get_rules(self) -> list[Rule]:
        return [_RULE]

    def get_claude_md(self) -> str | None:
        return (
            "## hidden_git_mutation — no git mutations hidden in an untracked script\n\n"
            "Running an untracked script file (`bash X`, `./X`, `source X`) whose text holds a git "
            "mutation (add, commit, push, reset, checkout, branch -d, worktree add/remove, ...) "
            "is denied: the git guards cannot see inside it. Run each git command directly, or "
            "commit the script as a tracked tool."
        )

    def get_acceptance_tests(self) -> list[AcceptanceTest]:
        script = "untracked/scratch/acceptance-hidden-git-mutation.bash"
        return [
            AcceptanceTest(
                title="Block an untracked script holding a git mutation",
                command=f"bash {script}",
                description="An untracked script whose text holds `git commit` is not run",
                expected_decision=Decision.DENY,
                expected_message_patterns=[rf"BLOCKED \[{RULE_ID}\]", r"cannot see inside a script"],
                safety_notes="The script exits before its git line, and it is denied before it runs",
                setup_commands=[f"printf '#!/usr/bin/env bash\\nexit 0\\ngit commit --dry-run -m x\\n' > {script}"],
                cleanup_commands=[f"rm -f {script}"],
                test_type=TestType.BLOCKING,
            ),
            AcceptanceTest(
                title="Allow an untracked script with no git mutation",
                command=f"bash {script}",
                description="An untracked script that only reads is left alone",
                expected_decision=Decision.ALLOW,
                expected_message_patterns=[],
                safety_notes="The script only echoes",
                setup_commands=[f"printf '#!/usr/bin/env bash\\necho hi\\ngit status --short\\n' > {script}"],
                cleanup_commands=[f"rm -f {script}"],
                test_type=TestType.BLOCKING,
            ),
        ]
