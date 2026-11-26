"""play-git-hooks-security.yml's pinned-list tasks, templated by the REAL ansible (Plan 00265 R79).

The play hands `qa-private-names pin` the machine's team declarations and the owner's
`private_name_terms`, each item of which must be vault-encrypted. The Go side is tested in
src-go/qa/privatenames; what only Ansible can show is how the play's own Jinja treats a vaulted
item, a plaintext one and a single vaulted string. So the expressions are read out of the play
file and templated by ansible's own Templar over host_vars text holding real `!vault` items,
encrypted here with a throwaway password. No playbook runs.

`ansible` lives in its own pipx venv, so the templating runs in a SUBPROCESS under the
interpreter from `ansible-playbook`'s shebang, as test_source_position_against_real_ansible.py
does; this file stays stdlib-only. Every name here is invented.
"""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import textwrap
import unittest

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
PLAY = os.path.join(REPO_ROOT, "playbooks", "imports", "play-git-hooks-security.yml")

#: The tasks of the pinned list, by name: each that holds a name must be no_log.
PLAINTEXT_TASK = "Count the private_name_terms Items That Are Not Vault-Encrypted"
REFUSE_TASK = "Refuse a private_name_terms Item That Is Not Vault-Encrypted"
FIND_TASK = "Read the Teams This Bus Host Serves"
PIN_TASK = "Write the Private-Name Guard's Pinned List"
CHECK_TASK = "Check the Pinned List Was Written"

#: Runs inside the ansible venv: reads {play, host_vars} on stdin, prints the templated results.
PROBE = textwrap.dedent(
    r"""
    import json, re, sys
    import yaml
    from ansible.parsing.dataloader import DataLoader
    from ansible.parsing.vault import VaultLib, VaultSecret, VaultSecretsContext
    from ansible.plugins.loader import init_plugin_loader
    from ansible.template import Templar, trust_as_template

    init_plugin_loader()
    req = json.load(sys.stdin)
    secret = VaultSecret(b"throwaway-test-password")
    VaultSecretsContext.initialize(VaultSecretsContext(secrets=[("default", secret)]))
    vault = VaultLib([("default", secret)])

    def vaulted(name, indent):
        lines = vault.encrypt(name).decode().splitlines()
        return "!vault |\n" + "\n".join(" " * indent + line for line in lines)

    built = re.sub(r"@VAULTITEM\[([^]]*)\]", lambda m: vaulted(m.group(1), 6), req["host_vars"])
    built = re.sub(r"@VAULTVAR\(([^)]*)\)", lambda m: vaulted(m.group(1), 2), built)

    with open(req["play"], encoding="utf-8") as handle:
        play = next(p for p in yaml.safe_load(handle) if "hosts" in p)
    tasks = {t["name"]: t for t in play["tasks"]}
    loader = DataLoader()
    variables = loader.load(built) or {}
    variables["pn_terms_is_list"] = trust_as_template(play["vars"]["pn_terms_is_list"])
    variables["pn_served"] = {"rc": 0, "stdout_lines": ["brambleton"]}
    templar = Templar(loader=loader, variables=variables)

    def render(expr):
        return templar.template(trust_as_template(expr))

    count = render(tasks[req["plaintext"]]["ansible.builtin.set_fact"]["pn_plaintext_items"])
    variables["pn_plaintext_items"] = count
    templar = Templar(loader=loader, variables=variables)
    that = [bool(templar.template(trust_as_template("{{ " + c + " }}"))) for c in tasks[req["refuse"]]["ansible.builtin.assert"]["that"]]
    stdin = templar.template(trust_as_template(tasks[req["pin"]]["ansible.builtin.command"]["stdin"]))
    print(json.dumps({"count": int(count), "that": that, "stdin": json.loads(stdin)}))
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


def _probe(test: unittest.TestCase, host_vars: str) -> dict:
    """host_vars marks a vaulted list item `@VAULTITEM[name]` and a vaulted variable `@VAULTVAR(name)`."""
    request = {
        "play": PLAY,
        "host_vars": host_vars,
        "plaintext": PLAINTEXT_TASK,
        "refuse": REFUSE_TASK,
        "pin": PIN_TASK,
    }
    result = subprocess.run(
        [_ansible_python(), "-I", "-c", PROBE],
        input=json.dumps(request),
        capture_output=True,
        text=True,
        check=False,
        # Not the checkout: its ansible.cfg names a vault id this throwaway secret lacks.
        cwd=os.path.dirname(__file__),
        env={k: v for k, v in os.environ.items() if not k.startswith("ANSIBLE_")},
    )
    if result.returncode != 0:
        test.fail(f"the probe could not run under the real ansible interpreter:\n{result.stderr.strip()}")
    return json.loads(result.stdout)


TEAMS = textwrap.dedent(
    """\
    teamchat_teams:
      - team: quillworks
        coordinator: inkwell-press.1+local.host
    agent_bus_element_teams:
      - team: saltmarsh
    """
)


class TestPrivateNameTermsMustBeVaulted(unittest.TestCase):
    def test_every_item_vaulted_passes_and_reaches_pin_decrypted(self) -> None:
        probed = _probe(self, TEAMS + "private_name_terms:\n  - @VAULTITEM[Kestrel Analytics]\n  - @VAULTITEM[kiwi]\n")
        self.assertEqual(probed["count"], 0)
        self.assertEqual(probed["that"], [True, True])
        stdin = probed["stdin"]
        self.assertEqual(stdin["private_name_terms"], ["Kestrel Analytics", "kiwi"])
        self.assertEqual(stdin["teamchat_teams"][0]["team"], "quillworks")
        self.assertEqual(stdin["agent_bus_element_teams"], [{"team": "saltmarsh"}])
        self.assertEqual(stdin["served_teams"], ["brambleton"])

    def test_a_plaintext_item_is_refused(self) -> None:
        probed = _probe(self, TEAMS + "private_name_terms:\n  - @VAULTITEM[Kestrel Analytics]\n  - otterbank\n")
        self.assertEqual(probed["count"], 1)
        self.assertEqual(probed["that"], [True, False])

    def test_a_single_vaulted_string_is_not_a_list_and_is_refused(self) -> None:
        probed = _probe(self, TEAMS + "private_name_terms: @VAULTVAR(kiwi)\n")
        self.assertFalse(probed["that"][0])

    def test_no_private_name_terms_passes_with_an_empty_list(self) -> None:
        probed = _probe(self, TEAMS)
        self.assertEqual(probed["that"], [True, True])
        self.assertEqual(probed["stdin"]["private_name_terms"], [])
        self.assertEqual(probed["stdin"]["teamchat_lxc_members"], [])


class TestNoNameReachesPlayOutput(unittest.TestCase):
    """Every task that holds a name is no_log, and the tasks that print hold none."""

    def _tasks(self) -> dict:
        probe = "import json, sys, yaml; play = next(p for p in yaml.safe_load(open(sys.argv[1])) if 'hosts' in p); print(json.dumps(play['tasks']))"
        result = subprocess.run(
            [_ansible_python(), "-I", "-c", probe, PLAY], capture_output=True, text=True, check=False
        )
        if result.returncode != 0:
            self.fail(result.stderr)
        return {t["name"]: t for t in json.loads(result.stdout)}

    def test_the_tasks_that_hold_names_are_no_log(self) -> None:
        tasks = self._tasks()
        for name in (PLAINTEXT_TASK, FIND_TASK, PIN_TASK):
            with self.subTest(task=name):
                self.assertIs(tasks[name].get("no_log"), True)

    def test_the_tasks_that_print_name_no_variable_that_holds_a_name(self) -> None:
        tasks = self._tasks()
        holders = ("private_name_terms }}", "teamchat_teams", "agent_bus", "pn_served.stdout", "pn_served.stderr")
        for name in (REFUSE_TASK, CHECK_TASK):
            with self.subTest(task=name):
                args = tasks[name]["ansible.builtin.assert"]
                printed = args.get("fail_msg", "") + args.get("success_msg", "")
                for holder in holders:
                    self.assertNotIn(holder, printed)
                self.assertNotIn("no_log", tasks[name])


if __name__ == "__main__":
    unittest.main()
