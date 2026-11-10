"""The GitHub SSH passphrase in play-github-cli-multi.yml: on a task's stdin, never elsewhere.

Issue #220: the play wrote the vault passphrase to the fixed path /tmp/.github_ssh_pp (any
local user could pre-create, race or read a predictable name in a shared directory) and then
put it back on the key tool's command line, reading that file into the old-passphrase option
and rendering the vault variable into the new-passphrase option. The secret-in-argv gate pins the command-line half for
every file; this test pins both halves for this play, reading its text (stdlib only, so line
by line, comments skipped):

- no line names a path under /tmp;
- the passphrase is rendered only on a `stdin:` line, and the task holding it has no_log.

    python3 -m unittest tests.helpers.ssh_passphrase.test_github_cli_multi_play
"""

from __future__ import annotations

import pathlib
import re
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
PLAY = REPO_ROOT / "playbooks" / "imports" / "play-github-cli-multi.yml"

RENDERED_SECRET = re.compile(r"\{\{[^}]*\bgithub_ssh_passphrase\b[^}]*\}\}")
FIXED_TMP = re.compile(r"(?<![\w.$}-])/tmp/[\w.-]")
STDIN_LINE = re.compile(r"^\s*stdin:\s")
TASK_START = re.compile(r"^(\s*)- name:")

# The secret-in-argv fixture holds the issue #220 tasks as they were (882dd13c0): the red
# case. It lives there because the gate excludes only that file; a copy here would be a finding.
DEFECT = REPO_ROOT / ".semgrep" / "argv-leak.fixture.yml"


def task_of(lines: list[str], index: int) -> tuple[int, int]:
    """The (start, end) line range of the task holding line index."""
    start = index
    while start >= 0 and not TASK_START.match(lines[start]):
        start -= 1
    if start < 0:
        return index, index + 1
    indent = len(TASK_START.match(lines[start]).group(1))
    end = start + 1
    while end < len(lines):
        line = lines[end]
        if line.strip() and len(line) - len(line.lstrip()) <= indent:
            break
        end += 1
    return start, end


def findings(text: str) -> list[str]:
    lines = text.splitlines()
    out = []
    for number, line in enumerate(lines):
        if line.lstrip().startswith("#"):
            continue
        if FIXED_TMP.search(line):
            out.append(f"line {number + 1}: a path under /tmp")
        if RENDERED_SECRET.search(line):
            if not STDIN_LINE.match(line):
                out.append(f"line {number + 1}: the passphrase rendered outside stdin:")
                continue
            start, end = task_of(lines, number)
            if not any(re.match(r"^\s*no_log:\s*true\s*$", t) for t in lines[start:end]):
                out.append(f"line {number + 1}: the passphrase on stdin without no_log")
    return out


class TestThePassphraseStaysOnStdin(unittest.TestCase):
    def test_the_play_has_no_finding(self):
        self.assertEqual(findings(PLAY.read_text(encoding="utf-8")), [])

    def test_the_play_still_hands_the_passphrase_over(self):
        # A play that dropped the passphrase altogether would pass the test above vacuously.
        text = PLAY.read_text(encoding="utf-8")
        on_stdin = [line for line in text.splitlines()
                    if STDIN_LINE.match(line) and RENDERED_SECRET.search(line)]
        self.assertGreaterEqual(len(on_stdin), 3)

    def test_the_issue_220_shape_is_found(self):
        kinds = {f.split(": ", 1)[1] for f in findings(DEFECT.read_text(encoding="utf-8"))}
        self.assertEqual(
            kinds,
            {
                "a path under /tmp",
                "the passphrase rendered outside stdin:",
                "the passphrase on stdin without no_log",
            },
        )

if __name__ == "__main__":
    unittest.main()
