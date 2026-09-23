"""Tests for helpers.self_update.cycle — the unattended cycle's sequencing (Plan 00137 T3.3, T3.4, T4.1).

Every collaborator is a fake that records what it was asked, so each test states the
ORDER of the cycle's effects, not only its exit status: the reboot must be the last thing,
the owed-verify marker must exist before the sessions are warned, and a failure must never
reach a reboot.
"""

from __future__ import annotations

import io
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.self_update import cycle, published
from helpers.self_update.affected_plays import Report

OLD = "a" * 40
NEW = "b" * 40
PLAY = "playbooks/imports/play-claude-yolo.yml"

CONFIG_TEXT = (
    "USER=tester\n"
    "BRANCH=F44\n"
    "REMOTE_URL=https://github.com/example/fedora-desktop.git\n"
    "PRINCIPAL=owner@example.com\n"
    "WARN_MINUTES=3\n"
    "ALERT_SINKS=\n"
    "ANSIBLE_COLLECTIONS_DIR=/usr/share/fedora-desktop/collections\n"
)


class FakeHost:
    """Records every call in order; each answer is set per test."""

    def __init__(self) -> None:
        self.calls: list[str] = []
        self.remote_error: str | None = None
        self.update_result = cycle.UpdateResult(rc=0, old=OLD, new=NEW, target=None, nothing=None)
        self.report = Report(run=[PLAY], skipped=[], unresolved=[])
        self.report_error: str | None = None
        self.allowed = [PLAY]
        self.play_rc: dict[str, int] = {}
        self.notify_rc: dict[str, int] = {}
        self.reboot_rc = 0
        self.verify_rc = 0
        self.boot = "boot-1"
        self.cancel_on_sleep: int | None = None
        self.toolchain_error: str | None = None
        self.trusted_head = True
        self.state: cycle.State | None = None

    def check_remote(self, url: str) -> str | None:
        self.calls.append("check_remote")
        return self.remote_error

    def head_trusted(self) -> bool:
        self.calls.append("head_trusted")
        return self.trusted_head

    def check_toolchain(self) -> str | None:
        self.calls.append("check_toolchain")
        return self.toolchain_error

    def update(self, *, dry_run: bool) -> cycle.UpdateResult:
        self.calls.append(f"update dry_run={dry_run}")
        return self.update_result

    def changed_plays(self, old: str, new: str) -> Report:
        self.calls.append(f"changed_plays {old[:1]}..{new[:1]}")
        if self.report_error is not None:
            raise ValueError(self.report_error)
        return self.report

    def allowlist(self) -> list[str]:
        self.calls.append("allowlist")
        return list(self.allowed)

    def run_play(self, play: str) -> int:
        self.calls.append(f"play {play}")
        return self.play_rc.get(play, 0)

    def notify(self, args: list[str]) -> int:
        owed = self.state is not None and self.state.read_owed() is not None
        self.calls.append(f"notify {' '.join(args)} owed={owed}")
        return self.notify_rc.get(" ".join(args), 0)

    def verify_restore(self, wait_seconds: int) -> int:
        self.calls.append(f"verify-restore {wait_seconds}")
        return self.verify_rc

    def reboot(self) -> int:
        self.calls.append("reboot")
        return self.reboot_rc

    def boot_id(self) -> str:
        return self.boot

    def sleep(self, seconds: float) -> None:
        self.calls.append(f"sleep {int(seconds)}")
        if self.cancel_on_sleep is not None and int(seconds) == self.cancel_on_sleep:
            raise cycle.Cancelled()

    def now(self) -> str:
        return "2026-09-23T03:30:00Z"


class CycleCase(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.published_dir = os.path.join(self._tmp.name, "published")
        os.mkdir(self.published_dir)
        self.state = cycle.State(self._tmp.name, published_dir=self.published_dir)
        self.host = FakeHost()
        self.host.state = self.state
        self.config = cycle.parse_config(CONFIG_TEXT)

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def run_cycle(self, *, dry_run: bool = False) -> tuple[int, str, str]:
        out, err = io.StringIO(), io.StringIO()
        code = cycle.run_cycle(self.config, self.host, self.state, dry_run=dry_run, stdout=out, stderr=err)
        return code, out.getvalue(), err.getvalue()

    def verify(self) -> tuple[int, str, str]:
        out, err = io.StringIO(), io.StringIO()
        code = cycle.verify(self.config, self.host, self.state, stdout=out, stderr=err)
        return code, out.getvalue(), err.getvalue()

    def result(self) -> dict[str, str]:
        record = self.state.read_result()
        self.assertIsNotNone(record)
        assert record is not None
        return record

    def mutating_calls(self) -> list[str]:
        return [c for c in self.host.calls if c.startswith(("play ", "notify", "reboot", "sleep"))]


class TestConfig(unittest.TestCase):
    def test_a_complete_config_parses(self) -> None:
        config = cycle.parse_config(CONFIG_TEXT)
        self.assertEqual(config.user, "tester")
        self.assertEqual(config.warn_minutes, 3)
        self.assertEqual(config.alert_sinks, ())

    def test_every_key_is_required(self) -> None:
        for key in ("USER", "BRANCH", "REMOTE_URL", "PRINCIPAL", "WARN_MINUTES", "ALERT_SINKS",
                    "ANSIBLE_COLLECTIONS_DIR"):
            text = "".join(line + "\n" for line in CONFIG_TEXT.splitlines() if not line.startswith(key + "="))
            with self.subTest(missing=key), self.assertRaises(cycle.ConfigError):
                cycle.parse_config(text)

    def test_an_unknown_key_is_refused(self) -> None:
        with self.assertRaises(cycle.ConfigError):
            cycle.parse_config(CONFIG_TEXT + "EXTRA=1\n")

    def test_a_repeated_key_is_refused(self) -> None:
        with self.assertRaises(cycle.ConfigError):
            cycle.parse_config(CONFIG_TEXT + "USER=other\n")

    def test_a_line_that_is_not_key_value_is_refused(self) -> None:
        with self.assertRaises(cycle.ConfigError):
            cycle.parse_config(CONFIG_TEXT + "$(reboot)\n")

    def test_comments_and_blank_lines_are_allowed(self) -> None:
        cycle.parse_config("# managed by Ansible\n\n" + CONFIG_TEXT)

    def test_values_are_not_shell(self) -> None:
        """Read, never sourced: quotes are part of the value, and that is refused for a user."""
        with self.assertRaises(cycle.ConfigError):
            cycle.parse_config(CONFIG_TEXT.replace("USER=tester", 'USER="tester"'))

    def test_the_remote_must_be_https(self) -> None:
        with self.assertRaises(cycle.ConfigError):
            cycle.parse_config(CONFIG_TEXT.replace(
                "REMOTE_URL=https://github.com/example/fedora-desktop.git",
                "REMOTE_URL=git@github.com:example/fedora-desktop.git",
            ))

    def test_warn_minutes_is_a_bounded_whole_number(self) -> None:
        for bad in ("0", "-1", "3.5", "x", "61"):
            with self.subTest(value=bad), self.assertRaises(cycle.ConfigError):
                cycle.parse_config(CONFIG_TEXT.replace("WARN_MINUTES=3", f"WARN_MINUTES={bad}"))

    def test_an_unimplemented_alert_sink_is_refused_not_ignored(self) -> None:
        for sinks in ("slack", "github", "carrier-pigeon"):
            with self.subTest(sinks=sinks), self.assertRaises(cycle.ConfigError):
                cycle.parse_config(CONFIG_TEXT.replace("ALERT_SINKS=", f"ALERT_SINKS={sinks}"))

    def test_none_is_an_explicit_empty_sink_list(self) -> None:
        config = cycle.parse_config(CONFIG_TEXT.replace("ALERT_SINKS=", "ALERT_SINKS=none"))
        self.assertEqual(config.alert_sinks, ())

    def test_the_collections_dir_is_an_absolute_normalised_path(self) -> None:
        config = cycle.parse_config(CONFIG_TEXT)
        self.assertEqual(config.ansible_collections_dir, "/usr/share/fedora-desktop/collections")
        for bad in ("", "collections", "/usr/share/../tmp/x", "/usr/share/x/", "/a b"):
            with self.subTest(value=bad), self.assertRaises(cycle.ConfigError):
                cycle.parse_config(CONFIG_TEXT.replace(
                    "ANSIBLE_COLLECTIONS_DIR=/usr/share/fedora-desktop/collections",
                    f"ANSIBLE_COLLECTIONS_DIR={bad}",
                ))


class TestPlayEnvironment(unittest.TestCase):
    """What a play is handed: the pinned system ansible, and every password on a descriptor."""

    CLONE = "/var/lib/fedora-desktop/deploy"
    # Every ansible-core 2.19 setting whose default searches ANSIBLE_HOME (~/.ansible, which
    # the user can write) for code, by the env var that overrides it. From base.yml.
    PLUGIN_PATH_ENV = (
        "ANSIBLE_ACTION_PLUGINS", "ANSIBLE_BECOME_PLUGINS", "ANSIBLE_CACHE_PLUGINS", "ANSIBLE_CALLBACK_PLUGINS",
        "ANSIBLE_CLICONF_PLUGINS", "ANSIBLE_CONNECTION_PLUGINS", "ANSIBLE_DOC_FRAGMENT_PLUGINS",
        "ANSIBLE_FILTER_PLUGINS", "ANSIBLE_HTTPAPI_PLUGINS", "ANSIBLE_INVENTORY_PLUGINS", "ANSIBLE_LIBRARY",
        "ANSIBLE_LOOKUP_PLUGINS", "ANSIBLE_MODULE_UTILS", "ANSIBLE_NETCONF_PLUGINS", "ANSIBLE_ROLES_PATH",
        "ANSIBLE_STRATEGY_PLUGINS", "ANSIBLE_TERMINAL_PLUGINS", "ANSIBLE_TEST_PLUGINS", "ANSIBLE_VARS_PLUGINS",
    )

    def env(self) -> dict[str, str]:
        return cycle.play_environment(
            home="/home/<user>", user="tester", runtime="/run/user/1000", clone=self.CLONE,
            ansible_playbook="/usr/bin/ansible-playbook", collections_dir="/usr/share/fedora-desktop/collections",
            lock_fd=5, become_fd=6, vault_fd=7,
        )

    def test_no_code_is_searched_for_under_the_users_home(self) -> None:
        env = self.env()
        for name in self.PLUGIN_PATH_ENV:
            with self.subTest(name=name):
                self.assertIn(name, env)
                for directory in env[name].split(":"):
                    self.assertTrue(directory.startswith(("/usr/share/", "/etc/ansible/", f"{self.CLONE}/")),
                                    f"{name} searches {directory}")

    def test_the_clones_own_plugins_and_roles_still_load(self) -> None:
        # The env beats the clone's ansible.cfg, so it must name what that file names:
        # callback_plugins = ./callback_plugins (the play ledger) and roles_path = ./roles/vendor.
        env = self.env()
        self.assertIn(f"{self.CLONE}/callback_plugins", env["ANSIBLE_CALLBACK_PLUGINS"].split(":"))
        self.assertEqual(env["ANSIBLE_ROLES_PATH"], f"{self.CLONE}/roles/vendor")

    def test_the_users_python_site_packages_are_not_imported(self) -> None:
        self.assertEqual(self.env()["PYTHONNOUSERSITE"], "1")

    def test_the_system_ansible_comes_before_any_user_path(self) -> None:
        path = self.env()["PATH"].split(":")
        self.assertEqual(path[0], "/usr/bin")
        self.assertLess(path.index("/usr/bin"), path.index("/home/<user>/.local/bin"))
        self.assertEqual(len(path), len(set(path)), "no directory twice")

    def test_the_pinned_ansible_directory_leads_the_path(self) -> None:
        env = cycle.play_environment(
            home="/home/<user>", user="tester", runtime="/run/user/1000", clone=self.CLONE,
            ansible_playbook="/opt/system-ansible/bin/ansible-playbook", collections_dir="/c",
            lock_fd=5, become_fd=6, vault_fd=7,
        )
        self.assertEqual(env["PATH"].split(":")[0], "/opt/system-ansible/bin")
        self.assertEqual(env["RUN_BASH_ANSIBLE_PLAYBOOK"], "/opt/system-ansible/bin/ansible-playbook")

    def test_the_play_is_pinned_to_the_system_ansible_and_its_collections(self) -> None:
        env = self.env()
        self.assertEqual(env["RUN_BASH_ANSIBLE_PLAYBOOK"], "/usr/bin/ansible-playbook")
        self.assertEqual(env["ANSIBLE_COLLECTIONS_PATH"], "/usr/share/fedora-desktop/collections")

    def test_facts_are_cached_in_memory_not_in_the_root_owned_clone(self) -> None:
        self.assertEqual(self.env()["ANSIBLE_CACHE_PLUGIN"], "memory")

    def test_every_password_is_a_descriptor_and_the_lock_is_delegated(self) -> None:
        env = self.env()
        self.assertEqual(env["RUN_BASH_SUDO_PASSWORD_FILE"], "/dev/fd/6")
        self.assertEqual(env["ANSIBLE_VAULT_PASSWORD_FILE"], "/dev/fd/7")
        self.assertEqual(env["FEDORA_DESKTOP_PLAY_LOCK_FD"], "5")

    def test_the_user_identity_is_the_configured_user(self) -> None:
        env = self.env()
        self.assertEqual((env["HOME"], env["USER"], env["LOGNAME"]), ("/home/<user>", "tester", "tester"))
        self.assertEqual(env["XDG_RUNTIME_DIR"], "/run/user/1000")


class TestSearchPathsUnderHome(unittest.TestCase):
    """The pinned list names the search paths one ansible-core release has. A later release
    can add one whose default is under ~/.ansible, and neither the list nor a test that
    copies it would notice. So the cycle asks the system ansible for its effective settings,
    and any search path under the user's home is a refusal. These dumps are fakes shaped
    like `ansible-config dump --format json`, so the rule is tested rather than a list."""

    HOME = "/home/<user>"
    CLONE = "/var/lib/fedora-desktop/deploy"

    def findings(self, dump: object) -> list[str]:
        return cycle.home_search_paths(dump, home=self.HOME, cwd=self.CLONE)

    def test_a_clean_dump_has_no_findings(self) -> None:
        dump = [
            {"name": "DEFAULT_ACTION_PLUGIN_PATH", "origin": "env", "value": ["/usr/share/ansible/plugins/action"]},
            {"name": "DEFAULT_ROLES_PATH", "origin": "env", "value": [f"{self.CLONE}/roles/vendor"]},
            {"name": "COLLECTIONS_PATHS", "origin": "env", "value": ["/usr/local/share/c"]},
        ]
        self.assertEqual(self.findings(dump), [])

    def test_a_search_path_this_code_has_never_heard_of_is_caught(self) -> None:
        dump = [{"name": "FUTURE_WIDGET_PLUGIN_PATH", "origin": "default",
                 "value": [f"{self.HOME}/.ansible/plugins/widget", "/usr/share/ansible/plugins/widget"]}]
        self.assertEqual(self.findings(dump), [f"FUTURE_WIDGET_PLUGIN_PATH={self.HOME}/.ansible/plugins/widget"])

    def test_the_home_directory_itself_counts(self) -> None:
        dump = [{"name": "DEFAULT_MODULE_PATH", "origin": "env", "value": [self.HOME]}]
        self.assertEqual(self.findings(dump), [f"DEFAULT_MODULE_PATH={self.HOME}"])

    def test_a_sibling_whose_name_starts_like_home_does_not(self) -> None:
        dump = [{"name": "DEFAULT_MODULE_PATH", "origin": "env", "value": [f"{self.HOME}-other/x"]}]
        self.assertEqual(self.findings(dump), [])

    def test_a_relative_path_is_judged_where_ansible_would_resolve_it(self) -> None:
        dump = [
            {"name": "DEFAULT_ROLES_PATH", "origin": "cfg", "value": ["./roles/vendor"]},
            {"name": "DEFAULT_LOOKUP_PLUGIN_PATH", "origin": "cfg", "value": ["../../../../home/<user>/x"]},
        ]
        self.assertEqual(self.findings(dump), ["DEFAULT_LOOKUP_PLUGIN_PATH=../../../../home/<user>/x"])

    def test_a_tilde_is_the_users_home(self) -> None:
        dump = [{"name": "DEFAULT_FILTER_PLUGIN_PATH", "origin": "cfg", "value": ["~/.ansible/plugins/filter"]}]
        self.assertEqual(self.findings(dump), ["DEFAULT_FILTER_PLUGIN_PATH=~/.ansible/plugins/filter"])

    def test_data_paths_that_are_not_searched_for_code_are_not_findings(self) -> None:
        """ansible-config dumps every search path as a list. A string names one file or working
        directory (the local tmp, the galaxy token, the persistent-connection sockets): data,
        not a place code is looked up, and named as open in DESIGN-cycle.md."""
        dump = [
            {"name": "DEFAULT_LOCAL_TMP", "origin": "default", "value": f"{self.HOME}/.ansible/tmp"},
            {"name": "GALAXY_TOKEN_PATH", "origin": "default", "value": f"{self.HOME}/.ansible/galaxy_token"},
            {"name": "PERSISTENT_CONTROL_PATH_DIR", "origin": "default", "value": f"{self.HOME}/.ansible/pc"},
            {"name": "DEFAULT_LOG_PATH", "origin": "default", "value": None},
        ]
        self.assertEqual(self.findings(dump), [])

    def test_the_inventory_is_judged_as_well(self) -> None:
        """Its host_vars choose the interpreter a root play runs."""
        dump = [{"name": "DEFAULT_HOST_LIST", "origin": "env", "value": [f"{self.HOME}/inventory"]}]
        self.assertEqual(self.findings(dump), [f"DEFAULT_HOST_LIST={self.HOME}/inventory"])

    def test_a_list_whose_name_does_not_say_path_is_judged_too(self) -> None:
        """DEFAULT_HOST_LIST already showed the naming is not a rule, so no name is trusted."""
        dump = [{"name": "FUTURE_WIDGET_SOURCES", "origin": "default", "value": [f"{self.HOME}/.ansible/widgets"]}]
        self.assertEqual(self.findings(dump), [f"FUTURE_WIDGET_SOURCES={self.HOME}/.ansible/widgets"])

    def test_a_bare_tilde_is_a_suffix_not_the_home(self) -> None:
        """INVENTORY_IGNORE_EXTS and MODULE_IGNORE_EXTS really do hold "~", a backup-file
        suffix. ansible expands `~` in every path-typed setting before dumping it, so a bare
        one that survives into the dump is never a path."""
        dump = [{"name": "INVENTORY_IGNORE_EXTS", "origin": "default", "value": [".pyc", "~"]},
                {"name": "MODULE_IGNORE_EXTS", "origin": "default", "value": [".bak", "~", ".rpm"]}]
        self.assertEqual(self.findings(dump), [])

    def test_lists_of_names_and_patterns_are_not_findings(self) -> None:
        """They resolve under the clone, where the play runs, not under the home."""
        dump = [
            {"name": "CALLBACKS_ENABLED", "origin": "cfg", "value": ["ansible.builtin.default", "play_ledger"]},
            {"name": "GALAXY_ROLE_SKELETON_IGNORE", "origin": "default", "value": ["^.git$", "^.*/.git_keep$"]},
            {"name": "INTERPRETER_PYTHON_FALLBACK", "origin": "default", "value": ["python3.13"]},
            {"name": "TAGS_RUN", "origin": "default", "value": []},
        ]
        self.assertEqual(self.findings(dump), [])

    def test_the_galaxy_servers_entry_a_real_dump_ends_with_is_accepted(self) -> None:
        dump = [{"name": "DEFAULT_MODULE_PATH", "origin": "env", "value": ["/usr/share/ansible/plugins/modules"]},
                {"GALAXY_SERVERS": {}}]
        self.assertEqual(self.findings(dump), [])

    def test_a_dump_that_is_not_the_expected_shape_is_refused(self) -> None:
        for bad in ({"not": "a list"}, [["not", "a dict"]], [{"value": ["/x"]}], [{"GALAXY_SERVERS": {}, "x": 1}]):
            with self.subTest(dump=bad):
                with self.assertRaises(ValueError):
                    self.findings(bad)


class TestPasswordPipe(unittest.TestCase):
    """ansible opens a vault password file BY PATH, and reopening /dev/fd/N re-checks the
    permissions of what the descriptor refers to: a root-only file, or a pipe root made, is
    EACCES to the user. The password therefore travels in a pipe the user owns."""

    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.path = os.path.join(self._tmp.name, "password")
        with open(self.path, "w", encoding="utf-8") as handle:
            handle.write("correct horse\n")
        os.chmod(self.path, 0o600)

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def test_the_pipe_holds_the_file_and_can_be_reopened_by_path(self) -> None:
        fd = cycle.password_pipe(self.path, os.getuid(), os.getgid())
        try:
            with open(f"/dev/fd/{fd}", "rb") as reopened:
                self.assertEqual(reopened.read(), b"correct horse\n")
        finally:
            os.close(fd)

    def test_the_pipe_is_owned_by_the_user_it_is_for(self) -> None:
        fd = cycle.password_pipe(self.path, os.getuid(), os.getgid())
        try:
            info = os.fstat(fd)
            self.assertEqual((info.st_uid, info.st_gid), (os.getuid(), os.getgid()))
        finally:
            os.close(fd)

    def test_the_read_end_is_not_inherited_unless_passed(self) -> None:
        fd = cycle.password_pipe(self.path, os.getuid(), os.getgid())
        try:
            self.assertFalse(os.get_inheritable(fd))
        finally:
            os.close(fd)

    def test_a_password_too_big_for_one_pipe_write_is_refused(self) -> None:
        with open(self.path, "w", encoding="utf-8") as handle:
            handle.write("x" * (cycle.PIPE_MAX_BYTES + 1))
        with self.assertRaises(cycle.ConfigError):
            cycle.password_pipe(self.path, os.getuid(), os.getgid())


class TestTrustedPath(unittest.TestCase):
    """The system ansible and its collections must be files only root (or the caller) can change."""

    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.root = self._tmp.name
        self.tool = os.path.join(self.root, "ansible-playbook")
        with open(self.tool, "w", encoding="utf-8") as handle:
            handle.write("#!/bin/sh\n")
        os.chmod(self.tool, 0o755)
        self.collections = os.path.join(self.root, "collections")
        os.mkdir(self.collections, 0o755)

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def test_a_file_and_a_directory_only_the_owner_can_write_are_trusted(self) -> None:
        cycle.require_trusted(self.tool, "ansible-playbook", directory=False)
        cycle.require_trusted(self.collections, "collections dir", directory=True)

    def test_a_group_or_world_writable_file_is_refused(self) -> None:
        for mode in (0o775, 0o757):
            os.chmod(self.tool, mode)
            with self.subTest(mode=oct(mode)), self.assertRaises(cycle.ConfigError):
                cycle.require_trusted(self.tool, "ansible-playbook", directory=False)

    def test_a_writable_directory_is_refused(self) -> None:
        os.chmod(self.collections, 0o777)
        with self.assertRaises(cycle.ConfigError):
            cycle.require_trusted(self.collections, "collections dir", directory=True)

    def test_a_file_in_a_writable_directory_is_refused(self) -> None:
        os.chmod(self.root, 0o777)
        try:
            with self.assertRaises(cycle.ConfigError):
                cycle.require_trusted(self.tool, "ansible-playbook", directory=False)
        finally:
            os.chmod(self.root, 0o700)

    def test_the_wrong_kind_or_a_missing_path_is_refused(self) -> None:
        for path, directory in ((self.collections, False), (self.tool, True), (self.tool + ".missing", False)):
            with self.subTest(path=path), self.assertRaises(cycle.ConfigError):
                cycle.require_trusted(path, "x", directory=directory)

    def test_a_symlinked_directory_is_refused(self) -> None:
        link = os.path.join(self.root, "link")
        os.symlink(self.collections, link)
        with self.assertRaises(cycle.ConfigError):
            cycle.require_trusted(link, "collections dir", directory=True)

    def test_a_symlinked_file_is_judged_by_its_target(self) -> None:
        link = os.path.join(self.root, "link")
        os.symlink(self.tool, link)
        cycle.require_trusted(link, "ansible-playbook", directory=False)
        os.chmod(self.tool, 0o777)
        with self.assertRaises(cycle.ConfigError):
            cycle.require_trusted(link, "ansible-playbook", directory=False)


class TestPrivateFile(unittest.TestCase):
    """The config decides the cycle, so others must not write it; a password file must not
    even be readable by them, or the secret is already out whatever the cycle does."""

    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.path = os.path.join(self._tmp.name, "file")
        with open(self.path, "w", encoding="utf-8") as handle:
            handle.write("words\n")

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def test_a_readable_config_is_accepted_and_a_writable_one_refused(self) -> None:
        os.chmod(self.path, 0o644)
        self.assertEqual(cycle.read_private(self.path, "config file"), "words\n")
        for mode in (0o664, 0o646):
            os.chmod(self.path, mode)
            with self.subTest(mode=oct(mode)), self.assertRaises(cycle.ConfigError):
                cycle.read_private(self.path, "config file")

    def test_a_secret_only_its_owner_can_read_is_accepted(self) -> None:
        os.chmod(self.path, 0o600)
        self.assertEqual(cycle.read_private(self.path, "become password file", secret=True), "words\n")

    def test_a_secret_the_group_or_others_can_read_is_refused(self) -> None:
        for mode in (0o640, 0o604, 0o610):
            os.chmod(self.path, mode)
            with self.subTest(mode=oct(mode)), self.assertRaises(cycle.ConfigError):
                cycle.read_private(self.path, "become password file", secret=True)

    def test_a_symlink_is_refused(self) -> None:
        os.chmod(self.path, 0o600)
        link = os.path.join(self._tmp.name, "link")
        os.symlink(self.path, link)
        with self.assertRaises(cycle.ConfigError):
            cycle.read_private(link, "vault password file", secret=True)


class TestNothingToDo(CycleCase):
    def test_nothing_signed_and_nothing_owed_does_nothing(self) -> None:
        self.state.write_deployed(OLD)
        self.host.update_result = cycle.UpdateResult(rc=0, old=None, new=None, target=None, nothing=OLD)
        code, out, _ = self.run_cycle()
        self.assertEqual(code, cycle.EXIT_OK)
        self.assertEqual(self.mutating_calls(), [])
        self.assertEqual(self.result()["outcome"], "nothing")
        self.assertIn("SELF-UPDATE-CYCLE nothing", out)

    def test_a_new_commit_that_touches_no_allowlisted_play_runs_nothing(self) -> None:
        self.state.write_deployed(OLD)
        self.host.report = Report(run=[], skipped=["playbooks/imports/play-basic-configs.yml"], unresolved=[])
        code, _, _ = self.run_cycle()
        self.assertEqual(code, cycle.EXIT_OK)
        self.assertEqual(self.mutating_calls(), [])
        self.assertEqual(self.state.read_deployed(), NEW, "nothing to apply is still deployed")
        self.assertEqual(self.result()["outcome"], "nothing")

    def test_the_basis_is_the_last_deployed_commit_not_the_pre_update_head(self) -> None:
        """A cycle whose plays failed leaves the clone moved on and `deployed` behind it; the
        next cycle must still see those plays as owed, even with nothing new upstream."""
        self.state.write_deployed(OLD)
        self.host.update_result = cycle.UpdateResult(rc=0, old=None, new=None, target=None, nothing=NEW)
        self.run_cycle()
        self.assertIn(f"changed_plays {OLD[:1]}..{NEW[:1]}", self.host.calls)
        self.assertIn(f"play {PLAY}", self.host.calls)


class TestRefusals(CycleCase):
    def test_a_gate_refusal_is_20_and_nothing_else_happens(self) -> None:
        self.host.update_result = cycle.UpdateResult(rc=17, old=None, new=None, target=None, nothing=None)
        code, _, err = self.run_cycle()
        self.assertEqual(code, cycle.EXIT_REFUSED)
        self.assertEqual(self.mutating_calls(), [])
        self.assertEqual(self.result()["outcome"], "refused")
        self.assertIn("ALERT", err)

    def test_a_remote_that_is_not_the_configured_one_is_refused_before_any_fetch(self) -> None:
        self.host.remote_error = "origin is not the configured remote"
        code, _, _ = self.run_cycle()
        self.assertEqual(code, cycle.EXIT_REFUSED)
        self.assertNotIn("update dry_run=False", self.host.calls)

    def test_a_reference_the_mapper_cannot_follow_in_an_allowlisted_play_refuses(self) -> None:
        self.state.write_deployed(OLD)
        self.host.report = Report(run=[PLAY], skipped=[], unresolved=[(PLAY, "x.yml:3: {{ mystery }}")])
        code, _, _ = self.run_cycle()
        self.assertEqual(code, cycle.EXIT_REFUSED)
        self.assertEqual(self.mutating_calls(), [])

    def test_a_diff_that_cannot_be_computed_refuses(self) -> None:
        self.state.write_deployed(OLD)
        self.host.report_error = "git diff failed"
        code, _, _ = self.run_cycle()
        self.assertEqual(code, cycle.EXIT_REFUSED)
        self.assertEqual(self.mutating_calls(), [])


class TestPlays(CycleCase):
    def test_a_failed_play_is_21_with_no_reboot_and_an_alert(self) -> None:
        self.state.write_deployed(OLD)
        self.host.report = Report(run=[PLAY, "playbooks/imports/play-other.yml"], skipped=[], unresolved=[])
        self.host.play_rc[PLAY] = 2
        code, _, err = self.run_cycle()
        self.assertEqual(code, cycle.EXIT_PLAY_FAILED)
        self.assertNotIn("reboot", self.host.calls)
        self.assertFalse(any(c.startswith("notify") for c in self.host.calls))
        self.assertNotIn("play playbooks/imports/play-other.yml", self.host.calls, "stops at the first failure")
        self.assertEqual(self.state.read_deployed(), OLD, "a failed play is retried next cycle")
        self.assertIsNone(self.state.read_owed())
        self.assertEqual(self.result()["outcome"], "play-failed")
        self.assertIn("ALERT", err)

    def test_the_toolchain_is_checked_before_the_first_play(self) -> None:
        self.state.write_deployed(OLD)
        self.run_cycle()
        self.assertLess(self.host.calls.index("check_toolchain"), self.host.calls.index(f"play {PLAY}"))

    def test_an_untrusted_toolchain_is_70_with_no_play_and_an_alert(self) -> None:
        self.state.write_deployed(OLD)
        self.host.toolchain_error = "ansible-playbook resolves to a file the user can write"
        code, _, err = self.run_cycle()
        self.assertEqual(code, cycle.EXIT_CONFIG)
        self.assertEqual(self.mutating_calls(), [])
        self.assertEqual(self.state.read_deployed(), OLD, "the plays stay owed")
        self.assertEqual(self.result()["outcome"], "config-invalid")
        self.assertIn("ALERT", err)

    def test_a_dry_run_reports_an_untrusted_toolchain_without_recording(self) -> None:
        self.state.write_deployed(OLD)
        self.host.toolchain_error = "no system ansible-playbook"
        code, _, err = self.run_cycle(dry_run=True)
        self.assertEqual(code, cycle.EXIT_CONFIG)
        self.assertIn("no system ansible-playbook", err)
        self.assertIsNone(self.state.read_result())

    def test_no_plays_needs_no_toolchain(self) -> None:
        self.state.write_deployed(OLD)
        self.host.report = Report(run=[], skipped=[], unresolved=[])
        self.host.toolchain_error = "no system ansible-playbook"
        code, _, _ = self.run_cycle()
        self.assertEqual(code, cycle.EXIT_OK)
        self.assertNotIn("check_toolchain", self.host.calls)

    def test_the_first_cycle_with_no_deployed_record_runs_every_allowlisted_play(self) -> None:
        code, _, _ = self.run_cycle()
        self.assertEqual(code, cycle.EXIT_OK)
        self.assertIn("allowlist", self.host.calls)
        self.assertNotIn(f"changed_plays {OLD[:1]}..{NEW[:1]}", self.host.calls)
        self.assertIn(f"play {PLAY}", self.host.calls)

    def test_the_first_cycle_refuses_a_clone_whose_head_nobody_signed(self) -> None:
        self.host.trusted_head = False
        code, out, err = self.run_cycle()
        self.assertEqual(code, cycle.EXIT_REFUSED)
        self.assertEqual(self.host.calls, ["check_remote", "head_trusted"])
        record = self.state.read_result()
        assert record is not None
        self.assertEqual((record["phase"], record["outcome"]), ("trust", "refused"))
        self.assertIn("ALERT refused", err)
        self.assertIn("SELF-UPDATE-CYCLE refused", out)
        self.assertIsNone(self.state.read_deployed())

    def test_a_dry_run_of_that_first_cycle_refuses_and_records_nothing(self) -> None:
        self.host.trusted_head = False
        code, _, _ = self.run_cycle(dry_run=True)
        self.assertEqual(code, cycle.EXIT_REFUSED)
        self.assertIsNone(self.state.read_result())

    def test_a_cycle_with_a_deployed_record_does_not_ask(self) -> None:
        self.state.write_deployed(OLD)
        self.host.trusted_head = False
        self.run_cycle()
        self.assertNotIn("head_trusted", self.host.calls)


class TestWarnAndReboot(CycleCase):
    def setUp(self) -> None:
        super().setUp()
        self.state.write_deployed(OLD)

    def test_full_success_warns_counts_down_and_reboots_last(self) -> None:
        code, _, _ = self.run_cycle()
        self.assertEqual(code, cycle.EXIT_OK)
        self.assertEqual(
            self.mutating_calls(),
            [
                f"play {PLAY}",
                "notify going-down --minutes 3 owed=True",
                "sleep 120",
                "notify going-down --minutes 1 owed=True",
                "sleep 60",
                "reboot",
            ],
        )
        self.assertEqual(self.host.calls[-1], "reboot")
        self.assertEqual(self.state.read_deployed(), NEW)
        owed = self.state.read_owed()
        assert owed is not None
        self.assertEqual((owed.boot, owed.new, owed.plays), ("boot-1", NEW, (PLAY,)))
        self.assertEqual(self.result()["outcome"], "rebooting")

    def test_a_one_minute_window_warns_once(self) -> None:
        self.config = cycle.parse_config(CONFIG_TEXT.replace("WARN_MINUTES=3", "WARN_MINUTES=1"))
        self.run_cycle()
        self.assertEqual(
            self.mutating_calls(),
            [f"play {PLAY}", "notify going-down --minutes 1 owed=True", "sleep 60", "reboot"],
        )

    def test_an_unwarnable_session_is_22_with_no_reboot(self) -> None:
        self.host.notify_rc["going-down --minutes 3"] = 1
        code, _, err = self.run_cycle()
        self.assertEqual(code, cycle.EXIT_UNWARNABLE)
        self.assertNotIn("reboot", self.host.calls)
        self.assertIsNotNone(self.state.read_owed(), "the reboot stays owed for the next cycle")
        self.assertEqual(self.result()["outcome"], "unwarnable")
        self.assertIn("ALERT", err)

    def test_a_failed_one_minute_warning_withdraws_the_first(self) -> None:
        self.host.notify_rc["going-down --minutes 1"] = 1
        code, _, _ = self.run_cycle()
        self.assertEqual(code, cycle.EXIT_UNWARNABLE)
        self.assertIn("notify reboot-cancelled owed=True", self.host.calls)
        self.assertNotIn("reboot", self.host.calls)

    def test_an_interrupted_countdown_withdraws_the_warning(self) -> None:
        self.host.cancel_on_sleep = 120
        code, _, _ = self.run_cycle()
        self.assertEqual(code, cycle.EXIT_CANCELLED)
        self.assertIn("notify reboot-cancelled owed=True", self.host.calls)
        self.assertNotIn("reboot", self.host.calls)
        self.assertEqual(self.result()["outcome"], "cancelled")

    def test_a_failed_reboot_request_withdraws_the_warning(self) -> None:
        self.host.reboot_rc = 1
        code, _, _ = self.run_cycle()
        self.assertEqual(code, cycle.EXIT_REBOOT_FAILED)
        self.assertEqual(self.host.calls[-1], "notify reboot-cancelled owed=True")
        self.assertEqual(self.result()["outcome"], "reboot-failed")

    def test_a_reboot_still_owed_from_this_boot_is_retried_with_nothing_new(self) -> None:
        self.state.write_deployed(NEW)
        self.state.write_owed(boot="boot-1", new=NEW, plays=(PLAY,))
        self.host.update_result = cycle.UpdateResult(rc=0, old=None, new=None, target=None, nothing=NEW)
        code, _, _ = self.run_cycle()
        self.assertEqual(code, cycle.EXIT_OK)
        self.assertNotIn(f"play {PLAY}", self.host.calls, "the plays already ran")
        self.assertEqual(self.host.calls[-1], "reboot")


class TestDryRun(CycleCase):
    def test_a_dry_run_names_the_plays_and_changes_nothing(self) -> None:
        self.state.write_deployed(OLD)
        self.host.update_result = cycle.UpdateResult(rc=0, old=OLD, new=None, target=NEW, nothing=None)
        code, out, _ = self.run_cycle(dry_run=True)
        self.assertEqual(code, cycle.EXIT_OK)
        self.assertIn("update dry_run=True", self.host.calls)
        self.assertEqual(self.mutating_calls(), [])
        self.assertIn(f"RUN {PLAY}", out)
        self.assertEqual(self.state.read_deployed(), OLD)
        self.assertIsNone(self.state.read_owed())
        self.assertIsNone(self.state.read_result(), "a dry run records nothing")


class TestVerify(CycleCase):
    def test_nothing_owed_is_a_no_op(self) -> None:
        code, _, _ = self.verify()
        self.assertEqual(code, cycle.EXIT_OK)
        self.assertEqual(self.host.calls, [])

    def test_a_marker_from_this_boot_is_a_no_op(self) -> None:
        self.state.write_owed(boot="boot-1", new=NEW, plays=(PLAY,))
        code, _, _ = self.verify()
        self.assertEqual(code, cycle.EXIT_OK)
        self.assertEqual(self.host.calls, [])
        self.assertIsNotNone(self.state.read_owed())

    def test_after_the_reboot_a_passing_check_records_the_deploy(self) -> None:
        self.state.write_owed(boot="boot-0", new=NEW, plays=(PLAY,))
        code, _, err = self.verify()
        self.assertEqual(code, cycle.EXIT_OK)
        self.assertEqual(self.host.calls, [f"verify-restore {cycle.VERIFY_WAIT_SECONDS}"])
        self.assertIsNone(self.state.read_owed())
        self.assertEqual(self.result()["outcome"], "deployed")
        self.assertIn("ALERT", err, "a completed cycle's summary is announced too (D8)")

    def test_a_failing_check_is_23_and_alerts(self) -> None:
        self.state.write_owed(boot="boot-0", new=NEW, plays=(PLAY,))
        self.host.verify_rc = 1
        code, _, err = self.verify()
        self.assertEqual(code, cycle.EXIT_VERIFY_FAILED)
        self.assertIsNone(self.state.read_owed())
        self.assertEqual(self.result()["outcome"], "verify-failed")
        self.assertIn("ALERT", err)


class TestState(CycleCase):
    def test_the_result_record_has_exactly_the_contract_keys(self) -> None:
        self.run_cycle()
        self.assertEqual(set(self.result()), {"at", "phase", "outcome", "old", "new", "plays", "detail"})

    def test_every_result_is_published_with_the_owed_boot(self) -> None:
        """The first cycle ends at the countdown, owing a check from this boot."""
        self.run_cycle()
        copy = published.read(self.published_dir)
        self.assertEqual({key: copy[key] for key in cycle.RESULT_KEYS}, self.result())
        self.assertEqual(copy["owed_boot"], self.host.boot)

    def test_the_published_copy_owes_nothing_once_the_verify_has_run(self) -> None:
        self.run_cycle()
        self.host.boot = "boot-2"
        self.verify()
        copy = published.read(self.published_dir)
        self.assertEqual(copy["outcome"], "deployed")
        self.assertEqual(copy["owed_boot"], "")

    def test_a_failed_play_is_published_owing_nothing(self) -> None:
        self.host.play_rc[PLAY] = 2
        self.run_cycle()
        copy = published.read(self.published_dir)
        self.assertEqual(copy["outcome"], "play-failed")
        self.assertEqual(copy["owed_boot"], "")

    def test_a_malformed_record_is_refused_not_guessed(self) -> None:
        with open(os.path.join(self._tmp.name, "owed-verify"), "w", encoding="utf-8") as handle:
            handle.write("nonsense\n")
        with self.assertRaises(cycle.StateError):
            self.state.read_owed()

    def test_a_malformed_deployed_sha_is_refused(self) -> None:
        with open(os.path.join(self._tmp.name, "deployed"), "w", encoding="utf-8") as handle:
            handle.write("not-a-sha\n")
        with self.assertRaises(cycle.StateError):
            self.state.read_deployed()

    def test_status_prints_the_last_result(self) -> None:
        self.run_cycle()
        out = io.StringIO()
        code = cycle.status(self.state, stdout=out)
        self.assertEqual(code, cycle.EXIT_OK)
        self.assertIn("outcome=rebooting", out.getvalue())
        self.assertIn(f"deployed={NEW}", out.getvalue())


if __name__ == "__main__":
    unittest.main()
