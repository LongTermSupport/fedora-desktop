"""The umask pin, driven through the REAL ansible-playbook that runs the plays.

Plan 00185's deploy ran `fc-cache -f` as root from a `cc` session, whose umask is 077, and the
system font cache came out 0600. This reproduces the defect without root: a real
`ansible-playbook`, started from the checkout (so the repository's ansible.cfg is loaded) under
an inherited umask of 077, runs a probe play that writes two files with no explicit mode, one
through a module (`copy`) and one through a command (`touch`, as `fc-cache` writes its cache).
Each must come out 0644, the mode it has when the play is started from an ordinary terminal.

The control run disables the callbacks from the environment and must come out 0600: without it
a green result could mean the probe never saw the inherited umask at all.
"""

from __future__ import annotations

import os
import shutil
import stat
import subprocess
import tempfile
import textwrap
import unittest

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
RESTRICTIVE = 0o077

PLAYBOOK_TEXT = textwrap.dedent(
    """\
    - name: Umask probe
      hosts: localhost
      gather_facts: false
      tasks:
        - name: A module writes a file with no mode
          ansible.builtin.copy:
            content: "probe\\n"
            dest: "{{ probe_dir }}/by-module"
        - name: A command writes a file, as fc-cache writes its cache
          ansible.builtin.command:
            argv: [touch, "{{ probe_dir }}/by-command"]
            creates: "{{ probe_dir }}/by-command"
    """
)


class TestUmaskPinAgainstRealAnsible(unittest.TestCase):
    def _run(self, extra_env: dict[str, str]) -> dict[str, int]:
        executable = shutil.which("ansible-playbook")
        if executable is None:
            self.fail("ansible-playbook is not on PATH. This repository's job is running Ansible, "
                      "so that is an IaC gap to fix, not a reason to skip this gate.")
        with tempfile.TemporaryDirectory() as scratch, tempfile.TemporaryDirectory() as state:
            os.chmod(scratch, 0o755)
            playbook = os.path.join(scratch, "probe.yml")
            with open(playbook, "w", encoding="utf-8") as handle:
                handle.write(PLAYBOOK_TEXT)
            probe_dir = os.path.join(scratch, "out")
            os.mkdir(probe_dir, 0o755)
            vault = os.path.join(scratch, "vault-pass")
            with open(vault, "w", encoding="utf-8") as handle:
                handle.write("throwaway\n")
            # The child inherits this process's umask, as a play inherits the cc session's.
            inherited = os.umask(RESTRICTIVE)
            try:
                result = subprocess.run(
                    [executable, playbook, "-i", "localhost,", "-c", "local", "-e", f"probe_dir={probe_dir}"],
                    capture_output=True,
                    text=True,
                    check=False,
                    cwd=REPO_ROOT,
                    stdin=subprocess.DEVNULL,
                    env={**os.environ, "XDG_STATE_HOME": state, "ANSIBLE_VAULT_PASSWORD_FILE": vault,
                         **extra_env},
                )
            finally:
                os.umask(inherited)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            return {name: stat.S_IMODE(os.stat(os.path.join(probe_dir, name)).st_mode)
                    for name in ("by-module", "by-command")}

    def test_files_written_with_no_mode_are_readable_by_everyone(self) -> None:
        for name, mode in self._run({}).items():
            with self.subTest(file=name):
                self.assertEqual(mode, 0o644, f"{name} is {mode:04o}: the play ran with the "
                                 "umask of the shell that started it (ansible-umask-pinned)")

    def test_control_without_the_callbacks_the_inherited_umask_reaches_the_tasks(self) -> None:
        modes = self._run({"ANSIBLE_CALLBACKS_ENABLED": "ansible.builtin.default"})
        self.assertEqual(modes, {"by-module": 0o600, "by-command": 0o600})


if __name__ == "__main__":
    unittest.main()
