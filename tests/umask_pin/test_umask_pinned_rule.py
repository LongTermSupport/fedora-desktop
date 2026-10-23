"""The rule ansible-umask-pinned (Defence Before Fix; CLAUDE/QA.md "ansible-umask-pinned").

Ansible runs with the umask of the shell that started it, and its root tasks inherit that
umask through sudo, which only ever adds 022 to the caller's. A task that writes a file with no
explicit mode (a cache rebuild such as `fc-cache -f`, a generated file, a `copy` with no
`mode:`) therefore takes the caller's umask. The host `cc` session sets 077 (Plan 00098), so a
play started from it left the system font cache readable only by root (Plan 00185).

The defence is one place every run passes through: `ansible.cfg` enables the `umask_pin`
callback (callback_plugins/umask_pin.py), which sets 022 before any task starts. The rule
reads code rather than running it, and reports:

1. an `ansible.cfg` whose `[defaults]` does not enable `umask_pin`, or whose
   `callback_plugins` does not name the repository's `callback_plugins` directory;
2. first-party code that sets `ANSIBLE_CALLBACKS_ENABLED` (or its old name
   `ANSIBLE_CALLBACK_WHITELIST`) without `umask_pin`, which replaces the list ansible.cfg gives;
3. first-party code that sets `ANSIBLE_CALLBACK_PLUGINS` without a `callback_plugins`
   directory, which replaces the directory the callback is loaded from;
4. first-party code that sets `ANSIBLE_CONFIG` to a file that is not an `ansible.cfg`.

Not caught: an `ANSIBLE_CONFIG` naming an `ansible.cfg` outside this repository, a run from a
directory with no ansible.cfg (whose plays then match no host), and a value built at run time
in a variable whose assignment is on another line.
"""

from __future__ import annotations

import configparser
import pathlib
import re
import subprocess
import textwrap
import unittest

RULE_ID = "ansible-umask-pinned"
DOCS = 'CLAUDE/QA.md "ansible-umask-pinned"'
REPO_ROOT = pathlib.Path(__file__).resolve().parents[2]
PIN = "umask_pin"
#: Trees that are not this repository's code. Vendored and daemon-owned trees are replaced
#: wholesale from upstream, untracked/ is never run from the checkout, and the scripts of an
#: archived plan are not run (meta-deploy refuses a folder that has moved into Completed/).
SKIPPED_PREFIXES = (
    "roles/vendor/",
    "src-go/vendor/",
    ".claude/hooks-daemon/",
    "untracked/",
    "CLAUDE/Plan/Completed/",
    "CLAUDE/Plan/Cancelled/",
)
#: This rule's own file, whose fixtures spell every pattern it looks for.
SELF = "tests/umask_pin/test_umask_pinned_rule.py"
#: The one line that turns the pin off: the end-to-end test's control run, an unprivileged
#: probe writing into a scratch directory, which shows the pin's absence reaches the tasks.
#: Matched as the whole line in that one file, so nothing else there is excused.
CONTROL_RUN = (
    "tests/umask_pin/test_umask_pin_against_real_ansible.py",
    'modes = self._run({"ANSIBLE_CALLBACKS_ENABLED": "ansible.builtin.default"})',
)

#: An assignment of the variable, in the forms this repository's languages write it: bash
#: `NAME=`, Go `"NAME=" +`, Python `env["NAME"] =` and `{"NAME": ...}`, YAML `NAME: ...`,
#: and a call that sets it (`setdefault`, `putenv`, Go `Setenv`). `==` is a comparison and a
#: bare quoted name in a list is not one.
_ASSIGN = r"[\"'\]]*\s*(?:=(?!=)|:(?!:))"
_SETTER = r"\b(?:setdefault|putenv|Setenv|setenv)\(\s*[\"']"


#: `<argv> | NAME=value` is how qa-changed's golden files render a recorded command's
#: environment (src-go/qa/changed golden_test.go and Plan 00192's capture.py): text compared
#: with text, never an environment a process is started with.
_RENDERED = r"(?<!\| )"


def _assignment(name: str) -> re.Pattern[str]:
    return re.compile(rf"{_RENDERED}\b{name}{_ASSIGN}|{_SETTER}{name}[\"']")


CALLBACKS_ENABLED = _assignment("ANSIBLE_CALLBACK(?:S_ENABLED|_WHITELIST)")
CALLBACK_PLUGINS = _assignment("ANSIBLE_CALLBACK_PLUGINS")
CONFIG = _assignment("ANSIBLE_CONFIG")


def _finding(path: str, line: int, what: str) -> str:
    return f"{path}:{line}: {RULE_ID}: {what}; see {DOCS}"


def config_findings(path: str, text: str) -> list[str]:
    """Each way an ansible.cfg leaves the umask unpinned."""
    parser = configparser.ConfigParser(interpolation=None, strict=False)
    parser.read_string(text, source=path)
    defaults = parser["defaults"] if parser.has_section("defaults") else {}
    found = []
    enabled = [name.strip() for name in defaults.get("callbacks_enabled", "").split(",")]
    if PIN not in enabled:
        found.append(_finding(path, _line_of(text, "callbacks_enabled"), f"callbacks_enabled does "
                              f"not name {PIN}, so Ansible runs with the umask of the shell that started it"))
    plugin_dirs = [entry.strip() for entry in defaults.get("callback_plugins", "").split(":")]
    if not any(pathlib.PurePath(entry).name == "callback_plugins" for entry in plugin_dirs if entry):
        found.append(_finding(path, _line_of(text, "callback_plugins"), f"callback_plugins does not "
                              f"name the repository's callback_plugins directory, so {PIN} cannot load"))
    return found


def _line_of(text: str, key: str) -> int:
    """The line that sets the key, or 1 when nothing does."""
    for number, line in enumerate(text.splitlines(), start=1):
        if re.match(rf"\s*{key}\s*=", line):
            return number
    return 1


def code_findings(path: str, text: str) -> list[str]:
    """Each line that replaces, for one process, what ansible.cfg says about the callback."""
    found = []
    for number, line in enumerate(text.splitlines(), start=1):
        if CALLBACKS_ENABLED.search(line) and PIN not in line:
            found.append(_finding(path, number, f"sets the enabled callbacks without {PIN}"))
        if CALLBACK_PLUGINS.search(line) and "callback_plugins" not in line:
            found.append(_finding(path, number, "sets the callback directories without the "
                                  "repository's callback_plugins"))
        if CONFIG.search(line) and "ansible.cfg" not in line:
            found.append(_finding(path, number, "sets ANSIBLE_CONFIG to a file that is not an "
                                  "ansible.cfg, which this rule cannot judge"))
    return found


def _tracked_files() -> list[str]:
    listed = subprocess.run(
        ["git", "-C", str(REPO_ROOT), "ls-files", "-z"],
        capture_output=True, text=True, check=True,
    ).stdout
    return [path for path in listed.split("\0") if path]


def repo_findings() -> list[str]:
    found = []
    for path in _tracked_files():
        if path.startswith(SKIPPED_PREFIXES) or path == SELF:
            continue
        full = REPO_ROOT / path
        if not full.is_file() or full.is_symlink():
            continue
        if pathlib.PurePath(path).name == "ansible.cfg":
            found.extend(config_findings(path, full.read_text(encoding="utf-8")))
            continue
        # Markdown is documentation: nothing here runs a .md file or starts a process with
        # its text as the environment, and reports and docs quote the patterns on purpose.
        if path.endswith(".md"):
            continue
        try:
            text = full.read_text(encoding="utf-8")
        except UnicodeDecodeError:
            continue
        found.extend(code_findings(path, _without_control_run(path, text)))
    return found


def _without_control_run(path: str, text: str) -> str:
    """The text with the control run's one line blanked, keeping every other line number."""
    if path != CONTROL_RUN[0]:
        return text
    return "\n".join("" if line.strip() == CONTROL_RUN[1] else line for line in text.splitlines())


PINNED_CFG = textwrap.dedent(
    """\
    [defaults]
    callback_plugins = ./callback_plugins
    callbacks_enabled = ansible.builtin.default, play_ledger, umask_pin
    """
)


class TestTheRuleFiresOnItsFixtures(unittest.TestCase):
    """Red first: each shape of the class, as a fixture the rule must report."""

    def test_a_config_that_does_not_enable_the_pin(self) -> None:
        unpinned = PINNED_CFG.replace(", umask_pin", "")
        self.assertEqual(len(config_findings("ansible.cfg", unpinned)), 1)

    def test_a_config_with_no_callbacks_enabled_at_all(self) -> None:
        self.assertEqual(len(config_findings("ansible.cfg", "[defaults]\ninventory = x\n")), 2)

    def test_a_config_with_no_defaults_section(self) -> None:
        self.assertEqual(len(config_findings("ansible.cfg", "[inventory]\nenable_plugins = yaml\n")), 2)

    def test_a_config_whose_plugin_directory_is_not_the_repositorys(self) -> None:
        moved = PINNED_CFG.replace("./callback_plugins", "/usr/share/ansible/plugins/callback")
        self.assertEqual(len(config_findings("ansible.cfg", moved)), 1)

    def test_the_pin_as_a_substring_of_another_name_is_not_the_pin(self) -> None:
        lookalike = PINNED_CFG.replace("umask_pin", "umask_pin_old")
        self.assertEqual(len(config_findings("ansible.cfg", lookalike)), 1)

    def test_the_enabled_list_replaced_from_the_environment(self) -> None:
        for line in (
            "ANSIBLE_CALLBACKS_ENABLED=ansible.builtin.default ansible-playbook site.yml",
            "export ANSIBLE_CALLBACK_WHITELIST=profile_tasks",
            'env["ANSIBLE_CALLBACKS_ENABLED"] = "ansible.builtin.default"',
            '{"ANSIBLE_CALLBACKS_ENABLED": "timer"}',
            '"ANSIBLE_CALLBACKS_ENABLED=" + names,',
            "      ANSIBLE_CALLBACKS_ENABLED: profile_tasks",
            'os.environ.setdefault("ANSIBLE_CALLBACKS_ENABLED", "timer")',
            "os.putenv('ANSIBLE_CALLBACK_WHITELIST', names)",
            'os.Setenv("ANSIBLE_CALLBACKS_ENABLED", names)',
        ):
            with self.subTest(line=line):
                self.assertEqual(len(code_findings("x", line)), 1)

    def test_a_configuration_that_is_not_an_ansible_cfg(self) -> None:
        for line in (
            "ANSIBLE_CONFIG=/etc/ansible/site.conf ansible-playbook x.yml",
            'env["ANSIBLE_CONFIG"] = other_config',
            'os.environ.setdefault("ANSIBLE_CONFIG", path)',
        ):
            with self.subTest(line=line):
                self.assertEqual(len(code_findings("x", line)), 1)

    def test_the_plugin_directory_replaced_from_the_environment(self) -> None:
        for line in (
            "ANSIBLE_CALLBACK_PLUGINS=/usr/share/ansible/plugins/callback ansible-playbook x.yml",
            'env["ANSIBLE_CALLBACK_PLUGINS"] = "/opt/callbacks"',
            '"ANSIBLE_CALLBACK_PLUGINS=" + dir,',
        ):
            with self.subTest(line=line):
                self.assertEqual(len(code_findings("x", line)), 1)


class TestTheRuleIsQuietWhereTheHazardIsAbsent(unittest.TestCase):
    def test_a_pinned_config(self) -> None:
        self.assertEqual(config_findings("ansible.cfg", PINNED_CFG), [])

    def test_settings_that_keep_the_pin(self) -> None:
        for line in (
            "ANSIBLE_CALLBACKS_ENABLED=ansible.builtin.default,play_ledger,umask_pin",
            'paths["ANSIBLE_CALLBACK_PLUGINS"] = f"{clone}/callback_plugins:/usr/share/x"',
            'export ANSIBLE_CONFIG="$REPO_ROOT/ansible.cfg"',
            '"ANSIBLE_CONFIG=" + filepath.Join(root, "ansible.cfg"),',
        ):
            with self.subTest(line=line):
                self.assertEqual(code_findings("x", line), [])

    def test_reading_or_naming_the_variable_is_not_setting_it(self) -> None:
        for line in (
            'echo "callbacks=${given[ANSIBLE_CALLBACK_PLUGINS]}"',
            '"ANSIBLE_ACTION_PLUGINS", "ANSIBLE_CALLBACK_PLUGINS",',
            'if env["ANSIBLE_CALLBACKS_ENABLED"] == "x":',
            'line += " | ANSIBLE_CONFIG=" + envOf(c.Env, "ANSIBLE_CONFIG")',
        ):
            with self.subTest(line=line):
                self.assertEqual(code_findings("x", line), [])


class TestTheRepository(unittest.TestCase):
    """The gate: every Ansible run this repository starts has its umask pinned."""

    def test_no_finding(self) -> None:
        found = repo_findings()
        self.assertEqual(found, [], "\n" + "\n".join(found))

    def test_the_repository_config_is_read(self) -> None:
        """Control: a scan that never reaches ansible.cfg would report nothing, and prove nothing."""
        self.assertIn("ansible.cfg", _tracked_files())

    def test_the_control_run_exclusion_excuses_its_one_line_and_nothing_else(self) -> None:
        path, line = CONTROL_RUN
        text = (REPO_ROOT / path).read_text(encoding="utf-8")
        self.assertEqual([entry.strip() for entry in text.splitlines()].count(line), 1,
                         "the control run's line moved: update CONTROL_RUN")
        self.assertEqual(len(code_findings(path, text)), 1)
        self.assertEqual(code_findings(path, _without_control_run(path, text)), [])
        widened = text + "\n" + 'self._run({"ANSIBLE_CALLBACKS_ENABLED": "timer"})\n'
        self.assertEqual(len(code_findings(path, _without_control_run(path, widened))), 1)


if __name__ == "__main__":
    unittest.main()
