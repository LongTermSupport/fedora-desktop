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
import tempfile
import textwrap
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.play_ledger import plugin_support

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))

#: Any tracked playbook with a top-level `- hosts:` will do; the assertion is that the
#: helper names THIS file, so the choice only has to be stable.
PLAYBOOK = os.path.join(REPO_ROOT, "playbooks", "imports", "play-claude-yolo.yml")

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


#: Parses one real ansible CLI's argv and prints what the ledger decides from it. One CLI
#: per process, because `context.CLIARGS` is a process-wide singleton set on first parse.
CLI_PROBE = textwrap.dedent(
    """
    import json, sys
    sys.path.insert(0, {repo!r})
    from ansible import context
    from ansible.cli.adhoc import AdHocCLI
    from ansible.cli.console import ConsoleCLI
    from ansible.cli.playbook import PlaybookCLI
    from helpers.play_ledger import plugin_support

    cli = {{"playbook": PlaybookCLI, "adhoc": AdHocCLI, "console": ConsoleCLI}}[{label!r}]
    cli({argv!r}).parse()
    print(json.dumps(plugin_support.names_playbooks(dict(context.CLIARGS))))
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


class TestFilelessRunsAgainstRealAnsible(unittest.TestCase):
    """The ledger skips plays with no file behind them — ad-hoc `ansible -m` and
    `ansible-console` — and knows them only by what the real CLIs set. A fake of those
    shapes cannot catch Ansible changing them, so these ask the real ones."""

    def _names_playbooks(self, label: str, argv: list[str]) -> bool:
        result = subprocess.run(
            [_ansible_python(), "-c", CLI_PROBE.format(repo=REPO_ROOT, label=label, argv=argv)],
            capture_output=True,
            text=True,
            check=False,
            cwd=REPO_ROOT,
            stdin=subprocess.DEVNULL,
        )
        if result.returncode != 0:
            self.fail(f"the {label} CLI probe could not run:\n{result.stderr.strip()}")
        return json.loads(result.stdout)

    def test_only_ansible_playbook_names_playbook_files(self) -> None:
        """If ansible-playbook stopped naming its files here, every run would go
        unrecorded — this is the test that says so, rather than an empty ledger."""
        self.assertIs(self._names_playbooks("playbook", ["ansible-playbook", "site.yml"]), True)
        self.assertIs(
            self._names_playbooks("adhoc", ["ansible", "localhost", "-m", "ping"]), False
        )
        self.assertIs(self._names_playbooks("console", ["ansible-console", "localhost"]), False)

    def _run(
        self, command: str, argv: list[str], state_home: str, stdin: str = ""
    ) -> subprocess.CompletedProcess:
        """A real ansible CLI run from the checkout, so the repo's own ansible.cfg
        enables the callback. The inventory is implicit localhost and the vault file a
        throwaway, so nothing of this host's configuration is read."""
        executable = shutil.which(command)
        if executable is None:
            self.fail(f"{command} is not on PATH beside ansible-playbook — an IaC gap")
        with tempfile.TemporaryDirectory() as scratch:
            vault = os.path.join(scratch, "vault-pass")
            with open(vault, "w", encoding="utf-8") as handle:
                handle.write("throwaway\n")
            return subprocess.run(
                [executable, "localhost", "-i", "localhost,", "-c", "local", *argv],
                input=stdin,
                capture_output=True,
                text=True,
                check=False,
                cwd=REPO_ROOT,
                env={**os.environ, "XDG_STATE_HOME": state_home,
                     "ANSIBLE_VAULT_PASSWORD_FILE": vault},
            )

    def _ad_hoc_ping(self, state_home: str) -> subprocess.CompletedProcess:
        return self._run("ansible", ["-m", "ping"], state_home)

    def test_a_real_console_run_leaves_the_ledger_untouched(self) -> None:
        """`ansible-console` sends no playbook-start event, so it has no ad-hoc marker;
        it is known by naming no playbook. `ok: [localhost]` proves the play ran."""
        with tempfile.TemporaryDirectory() as state_home:
            result = self._run("ansible-console", [], state_home, stdin="ping\nexit\n")
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("ok: [localhost]", result.stdout)
            self.assertNotIn(plugin_support.FAILURE_MARKER, result.stdout + result.stderr)
            self.assertEqual(os.listdir(state_home), [])

    def test_the_callback_is_live_for_an_ad_hoc_run(self) -> None:
        """Control: without it, a silent ledger below would prove nothing. A relative
        state home makes the callback's constructor refuse, loudly."""
        result = self._ad_hoc_ping("relative/state")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(plugin_support.FAILURE_MARKER, result.stderr)

    def test_a_real_ad_hoc_run_leaves_the_ledger_untouched(self) -> None:
        with tempfile.TemporaryDirectory() as state_home:
            result = self._ad_hoc_ping(state_home)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertNotIn(plugin_support.FAILURE_MARKER, result.stderr)
            self.assertEqual(os.listdir(state_home), [])


if __name__ == "__main__":
    unittest.main()
