"""Every play that vaults a value it was handed on `stdin:` turns off Ansible's newline.

`ansible.builtin.command` appends a newline to `stdin` unless `stdin_add_newline: false`,
and `ansible-vault encrypt_string --stdin-name` vaults every byte it reads. So the secret
is saved with a trailing newline. The Slack webhook (Plan 00137 T4.5) then fails its own
assert on the next run, or the cycle's webhook file gains a second line and it refuses to
start. Read from the plays themselves, so a new vaulting task that forgets it fails here.
"""

from __future__ import annotations

import os
import re
import subprocess
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
_TASK = re.compile(r"^[ \t]*- name:", re.MULTILINE)
_STDIN = re.compile(r"^[ \t]+stdin:", re.MULTILINE)
_NO_NEWLINE = re.compile(r"^[ \t]+stdin_add_newline:[ \t]*false[ \t]*$", re.MULTILINE)


def offending_tasks(text: str) -> list[str]:
    """The first line of each task that vaults `stdin:` with Ansible's newline still on."""
    starts = [match.start() for match in _TASK.finditer(text)] + [len(text)]
    found = []
    for begin, end in zip(starts, starts[1:]):
        task = text[begin:end]
        if "encrypt_string" in task and _STDIN.search(task) and not _NO_NEWLINE.search(task):
            found.append(task.splitlines()[0].strip())
    return found


def tracked_plays() -> list[str]:
    listed = subprocess.run(
        ["git", "-C", REPO, "ls-files", "-z", "--", "playbooks", "tasks"],
        capture_output=True, check=True,
    )
    return [name for name in listed.stdout.decode("utf-8").split("\0") if name.endswith((".yml", ".yaml"))]


class TestTheCheckItself(unittest.TestCase):
    TASK = (
        "    - name: Vault It\n"
        "      ansible.builtin.command:\n"
        "        argv: [ansible-vault, encrypt_string, --stdin-name, x]\n"
        "        stdin: \"{{ x }}\"\n"
    )

    def test_a_vaulting_task_with_the_newline_on_is_found(self) -> None:
        self.assertEqual(offending_tasks(self.TASK), ["- name: Vault It"])

    def test_the_same_task_with_it_off_is_not(self) -> None:
        self.assertEqual(offending_tasks(self.TASK + "        stdin_add_newline: false\n"), [])

    def test_a_task_that_only_mentions_encrypt_string_in_a_message_is_not(self) -> None:
        self.assertEqual(offending_tasks("    - name: Tell\n      debug:\n        msg: run encrypt_string\n"), [])

    def test_the_setting_in_the_next_task_does_not_count(self) -> None:
        text = self.TASK + "    - name: Other\n      command:\n        stdin_add_newline: false\n"
        self.assertEqual(offending_tasks(text), ["- name: Vault It"])


class TestThePlays(unittest.TestCase):
    def test_no_play_vaults_a_trailing_newline(self) -> None:
        plays = tracked_plays()
        self.assertGreater(len(plays), 10, "git ls-files found no plays; the check would pass on nothing")
        for play in plays:
            with open(os.path.join(REPO, play), encoding="utf-8") as handle:
                found = offending_tasks(handle.read())
            with self.subTest(play=play):
                self.assertEqual(found, [], "add stdin_add_newline: false beside stdin:")


if __name__ == "__main__":
    unittest.main()
