"""The plugin↔helper seam, driven through the REAL ansible that runs the plays.

This is the gate that did not exist when the ledger broke. `callback_plugins/play_ledger.py`
reads a play's source position and hands it to `plugin_support.source_position`; ansible-core
2.19 removed `ansible_pos` from the parsed mapping and moved the same fact into an `Origin`
tag, and because `_ds` still exists the defensive `getattr` never raised — it read `None` off
an object that was still there. Every play became a recorded hole, the ledger marked itself
BROKEN on every run, and the unit suite stayed green throughout, because both sides of the
seam were exercised only against a hand-written fake origin.

A fake cannot catch a rename in the thing it is faking. So this loads a real playbook through
the real `Play.load` and asserts the production helper gets the real file back.

`ansible` is not importable by the interpreter that runs this suite — it lives in its own
pipx venv — so the work happens in a SUBPROCESS under the interpreter from
`ansible-playbook`'s shebang. That keeps this file stdlib-only, per helpers/CLAUDE.md, while
still testing against the version that will actually parse the plays.
"""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
import textwrap
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))

#: Any tracked playbook with a top-level `- hosts:` will do; the assertion is that the
#: helper names THIS file, so the choice only has to be stable.
PLAYBOOK = os.path.join(REPO_ROOT, "playbooks", "imports", "play-claude-code.yml")

#: Runs inside the ansible venv. Prints the helper's answer as JSON, or exits non-zero
#: with its reason on stderr — never a bare traceback the caller cannot explain.
PROBE = textwrap.dedent(
    """
    import json, sys
    sys.path.insert(0, {repo!r})
    import ansible
    from ansible.parsing.dataloader import DataLoader
    from ansible.playbook.play import Play
    from ansible.vars.manager import VariableManager
    from ansible.inventory.manager import InventoryManager
    from ansible.plugins.loader import init_plugin_loader
    from helpers.play_ledger import plugin_support

    # Play.load resolves every task's module name, so the plugin loader has to be up
    # or a FQCN like ansible.builtin.stat fails to resolve.
    init_plugin_loader()

    loader = DataLoader()
    inventory = InventoryManager(loader=loader, sources=[])
    variables = VariableManager(loader=loader, inventory=inventory)
    data = loader.load_from_file({playbook!r})
    play = Play.load(data[0], variable_manager=variables, loader=loader)

    # The object a callback is handed is a COPY, not the parsed play
    # (executor/task_queue_manager.py). Assert against both, because a mechanism that
    # survives parsing and is lost in the copy is exactly the shape that shipped broken.
    answers = {{}}
    for label, obj in (("parsed", play), ("copy", play.copy())):
        answers[label] = plugin_support.source_position(
            getattr(obj, "_origin", None),
            getattr(getattr(obj, "_ds", None), "ansible_pos", None),
        )
    print(json.dumps({{"answers": answers, "ansible": ansible.__version__}}))
    """
)


def _ansible_python() -> str:
    """The interpreter `ansible-playbook` itself runs under, from its shebang."""
    launcher = shutil.which("ansible-playbook")
    if launcher is None:
        raise RuntimeError(
            "ansible-playbook is not on PATH. This repository's entire job is running "
            "Ansible, so that is an IaC gap to fix, not a reason to skip this gate."
        )
    with open(launcher, encoding="utf-8") as handle:
        first = handle.readline().strip()
    if not first.startswith("#!"):
        raise RuntimeError(f"{launcher} has no shebang, so its interpreter is unknown")
    return first[2:].split()[0]


class TestSourcePositionAgainstRealAnsible(unittest.TestCase):
    def _probe(self, playbook: str) -> dict:
        result = subprocess.run(
            [_ansible_python(), "-c", PROBE.format(repo=REPO_ROOT, playbook=playbook)],
            capture_output=True,
            text=True,
            check=False,
            cwd=REPO_ROOT,
        )
        if result.returncode != 0:
            self.fail(
                "the probe could not run under the real ansible interpreter:\n"
                f"{result.stderr.strip()}"
            )
        return json.loads(result.stdout)

    def test_the_production_helper_gets_the_real_file_from_a_real_play(self) -> None:
        probed = self._probe(PLAYBOOK)
        for label, answer in probed["answers"].items():
            with self.subTest(play=label, ansible=probed["ansible"]):
                self.assertIsNotNone(
                    answer,
                    f"source_position returned None for the {label} play under ansible "
                    f"{probed['ansible']} — this is the 2.19 regression recurring, and "
                    "every play would be recorded as a hole",
                )
                self.assertEqual(os.path.abspath(answer[0]), os.path.abspath(PLAYBOOK))
                self.assertIsInstance(answer[1], int)

    def test_the_parsed_play_and_the_callbacks_copy_agree(self) -> None:
        """A callback never sees the parsed play. If the position survives parsing but
        is lost in `copy()`, every unit test passes and every real run records a hole."""
        probed = self._probe(PLAYBOOK)
        self.assertEqual(probed["answers"]["parsed"], probed["answers"]["copy"])


if __name__ == "__main__":
    unittest.main()
