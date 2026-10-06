"""Unit tests for helpers/pingbus/config.py: member bundles and the environment.

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_config

Spec: docs/agent-bus-protocol.md §12 (bundle, member.json, PINGBUS_HOME, PINGBUS_TEAMS, --team)
and DESIGN.md sections 5.1, 5.2 (Python gate, human_text) and 8 (no `host` member beside
an Element profile). Every bundle here is built in a temporary directory.
"""

from __future__ import annotations

import json
import os
import pathlib
import sys
import tempfile
import types
import unittest
from unittest import mock

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import config, limits

SN = "team-a.agent-bus.internal"
HANDLE = "myrepo.1+workstation.podman"
HOST_HANDLE = "myrepo.2+workstation.host"
ROOM = "!" + "A" * 43
TOKEN = "syt_ZXhhbXBsZQ_notarealtoken_0123"
USER_HOME = "/home/<user>"


def member_json(**overrides: object) -> dict[str, object]:
    data: dict[str, object] = {
        "protocol": 1,
        "team": "team-a",
        "user_id": f"@{HANDLE}:{SN}",
        "server_name": SN,
        "base_url": "http://192.0.2.10:8448",
        "plain_http_hosts": ["192.0.2.10"],
        "token_file": "token",
        "admin": f"@admin:{SN}",
        "room": ROOM,
    }
    data.update(overrides)
    return data


def team_json(team: str) -> dict[str, object]:
    """A valid member.json for `team`, on that team's own server."""
    sn = f"{team}.agent-bus.internal"
    return member_json(team=team, server_name=sn, user_id=f"@{HANDLE}:{sn}", admin=f"@admin:{sn}")


class BundleCase(unittest.TestCase):
    """A temporary PINGBUS_HOME and agent home; `write_bundle` builds one team's bundle."""

    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = pathlib.Path(self._tmp.name)
        self.home = self.root / "pingbus"
        self.user_home = self.root / "agent-home"
        self.user_home.mkdir()
        self.uid = os.getuid()

    def write_bundle(
        self,
        team: str = "team-a",
        data: object = None,
        token: str | None = None,
        token_mode: int = 0o600,
    ) -> pathlib.Path:
        """Each team gets its own server and its own token unless the test says otherwise."""
        if token is None:
            token = TOKEN if team == "team-a" else f"{TOKEN}_{team.replace('-', '_')}"
        bundle = self.home / team
        bundle.mkdir(parents=True, mode=0o700)
        payload = (member_json() if team == "team-a" else team_json(team)) if data is None else data
        (bundle / "member.json").write_text(json.dumps(payload), encoding="utf-8")
        token_path = bundle / "token"
        token_path.write_text(token, encoding="utf-8")
        token_path.chmod(token_mode)
        return bundle

    def load(self, team: str = "team-a", uid: int | None = None) -> config.Member:
        return config.load_bundle(
            self.home, team, uid=self.uid if uid is None else uid, user_home=self.user_home
        )

    def assert_refused(self, team: str = "team-a", contains: str = "") -> config.ConfigError:
        with self.assertRaises(config.ConfigError) as caught:
            self.load(team)
        self.assertIn(contains, str(caught.exception))
        self.assertNotIn(TOKEN, str(caught.exception))
        return caught.exception


class TestSchema(BundleCase):
    def test_valid_bundle_loads(self) -> None:
        self.write_bundle()
        member = self.load()
        self.assertEqual(member.team, "team-a")
        self.assertEqual(member.user_id, f"@{HANDLE}:{SN}")
        self.assertEqual(member.handle, HANDLE)
        self.assertEqual(member.member_type, "podman")
        self.assertEqual(member.server_name, SN)
        self.assertEqual(member.base_url, "http://192.0.2.10:8448")
        self.assertEqual(member.plain_http_hosts, ("192.0.2.10",))
        self.assertEqual(member.admin, f"@admin:{SN}")
        self.assertEqual(member.room, ROOM)
        self.assertEqual(member.bundle_dir, self.home / "team-a")
        self.assertEqual(member.state_dir, self.home / "team-a" / "state")
        self.assertEqual(member.limits, limits.Limits())

    def test_human_text_defaults_to_true(self) -> None:
        self.write_bundle()
        self.assertIs(self.load().human_text, True)

    def test_human_text_false_is_kept(self) -> None:
        self.write_bundle(data=member_json(human_text=False))
        self.assertIs(self.load().human_text, False)

    def test_human_text_must_be_boolean(self) -> None:
        for bad in (0, 1, "false", None):
            with self.subTest(bad=bad):
                self.setUp()
                self.write_bundle(data=member_json(human_text=bad))
                self.assert_refused(contains="human_text")

    def test_unknown_key_refused(self) -> None:
        self.write_bundle(data=member_json(repos=[]))
        self.assert_refused(contains="repos")

    def test_each_required_key_missing_refused(self) -> None:
        for key in config.REQUIRED_KEYS:
            with self.subTest(key=key):
                self.setUp()
                data = member_json()
                del data[key]
                self.write_bundle(data=data)
                self.assert_refused(contains=key)

    def test_protocol_must_be_integer_one(self) -> None:
        for bad in (2, True, "1", 1.0):
            with self.subTest(bad=bad):
                self.setUp()
                self.write_bundle(data=member_json(protocol=bad))
                self.assert_refused(contains="protocol")

    def test_team_must_equal_directory(self) -> None:
        self.write_bundle(team="team-a", data=member_json(team="team-b"))
        self.assert_refused(contains="bundle directory")

    def test_bad_team_name_refused_before_any_read(self) -> None:
        for bad in ("Team", "../x", "1team", "a" * 25, ""):
            with self.subTest(bad=bad), self.assertRaises(config.ConfigError):
                self.load(team=bad)

    def test_user_id_must_be_a_handle_on_this_server(self) -> None:
        for bad in (
            f"@{HANDLE}:other.internal",
            f"@alice:{SN}",
            f"@admin:{SN}",
            f"{HANDLE}:{SN}",
            f"@{HANDLE}",
        ):
            with self.subTest(bad=bad):
                self.setUp()
                self.write_bundle(data=member_json(user_id=bad))
                self.assert_refused(contains="user_id")

    def test_server_name_must_be_a_dns_name(self) -> None:
        for bad in ("Team.Internal", "-a.internal", "a..internal", "a.internal:8448", "x" * 254):
            with self.subTest(bad=bad):
                self.setUp()
                self.write_bundle(
                    data=member_json(
                        server_name=bad, user_id=f"@{HANDLE}:{bad}", admin=f"@admin:{bad}"
                    )
                )
                self.assert_refused(contains="server_name")

    def test_admin_must_be_the_admin_account_on_this_server(self) -> None:
        for bad in ("@admin:other.internal", f"@alice:{SN}", f"@{HANDLE}:{SN}"):
            with self.subTest(bad=bad):
                self.setUp()
                self.write_bundle(data=member_json(admin=bad))
                self.assert_refused(contains="admin")

    def test_room_must_be_a_room_id(self) -> None:
        for bad in ("!short", "#alias:" + SN, "!" + "A" * 43 + ":" + SN):
            with self.subTest(bad=bad):
                self.setUp()
                self.write_bundle(data=member_json(room=bad))
                self.assert_refused(contains="room")

    def test_member_json_must_be_an_object(self) -> None:
        self.write_bundle(data=[])
        self.assert_refused(contains="member.json")

    def test_member_json_invalid_json_refused(self) -> None:
        bundle = self.write_bundle()
        (bundle / "member.json").write_text("{not json", encoding="utf-8")
        self.assert_refused(contains="member.json")

    def test_missing_bundle_refused(self) -> None:
        self.assert_refused(contains="team-a")

    def test_member_json_that_is_a_directory_refused(self) -> None:
        bundle = self.write_bundle()
        (bundle / "member.json").unlink()
        (bundle / "member.json").mkdir()
        self.assert_refused(contains="member.json")

    def test_member_json_that_is_a_fifo_refused_without_blocking(self) -> None:
        bundle = self.write_bundle()
        (bundle / "member.json").unlink()
        os.mkfifo(bundle / "member.json")
        self.assert_refused(contains="regular file")

    def test_member_json_unreadable_refused(self) -> None:
        self.write_bundle()
        denied = PermissionError(13, "Permission denied")
        with mock.patch.object(config.os, "open", side_effect=denied):
            self.assert_refused(contains="Permission denied")

    def test_member_json_duplicate_key_refused(self) -> None:
        bundle = self.write_bundle()
        text = json.dumps(member_json())
        (bundle / "member.json").write_text('{"protocol":1,' + text[1:], encoding="utf-8")
        self.assert_refused(contains="member.json")

    def test_member_json_size_limit(self) -> None:
        text = json.dumps(member_json())
        at_limit = text + " " * (config.MEMBER_FILE_MAX_BYTES - len(text))
        bundle = self.write_bundle()
        (bundle / "member.json").write_text(at_limit, encoding="utf-8")
        self.load()
        (bundle / "member.json").write_text(at_limit + " ", encoding="utf-8")
        self.assert_refused(contains="larger")

    def test_unknown_key_value_is_never_quoted(self) -> None:
        self.write_bundle(data=member_json(**{TOKEN: 1}))
        error = self.assert_refused(contains="unknown key")
        self.assertNotIn(TOKEN[:8], str(error))

    def test_limits_parsed_by_limits_module(self) -> None:
        self.write_bundle(data=member_json(limits={"send_burst": 5}))
        self.assertEqual(self.load().limits, limits.Limits(send_burst=5))

    def test_limits_refused(self) -> None:
        for bad in (
            [],
            None,
            {"send_burst": "5"},
            {"send_burst": True},
            {"send_burst": 1.5},
            {"send_burst": 31},
            {"send_burst": 0},
            {"duplicate_window_s": 10},
        ):
            with self.subTest(bad=bad):
                self.setUp()
                self.write_bundle(data=member_json(limits=bad))
                error = self.assert_refused(contains="limits")
                self.assertIn(str(self.home / "team-a" / "member.json"), str(error))

    def test_limits_unknown_key_never_quoted(self) -> None:
        self.write_bundle(data=member_json(limits={TOKEN: 1}))
        error = self.assert_refused(contains="limits")
        self.assertNotIn(TOKEN[:8], str(error))

    def test_member_repr_has_no_token(self) -> None:
        self.write_bundle()
        self.assertNotIn(TOKEN, repr(self.load()))


class TestOpenPrivateFile(unittest.TestCase):
    """The one owner-and-mode check every secret file passes (bundle and forge tokens)."""

    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.root = pathlib.Path(self._tmp.name)
        self.uid = os.getuid()

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def secret(self, name: str = "secret", mode: int = 0o600) -> pathlib.Path:
        path = self.root / name
        path.write_text("abc", encoding="utf-8")
        path.chmod(mode)
        return path

    def refused(self, path: pathlib.Path, contains: str, uid: int | None = None) -> None:
        with self.assertRaises(config.ConfigError) as caught:
            config.open_private_file(path, "WHERE", uid=self.uid if uid is None else uid)
        self.assertIn("WHERE", str(caught.exception))
        self.assertIn(contains, str(caught.exception))

    def test_private_file_opens(self) -> None:
        for mode in (0o600, 0o400):
            with self.subTest(mode=oct(mode)):
                path = self.secret(f"s{mode:o}", mode)
                with config.open_private_file(path, "WHERE", uid=self.uid) as handle:
                    self.assertEqual(handle.read(), b"abc")

    def test_refusals(self) -> None:
        loose = self.secret("loose", 0o644)
        self.refused(loose, "looser")
        self.refused(self.root / "absent", "no such file")
        link = self.root / "link"
        link.symlink_to(self.secret())
        self.refused(link, "regular file")
        self.refused(self.root, "regular file")
        self.refused(self.secret("other"), "owned", uid=self.uid + 1)

    def test_fifo_refused_without_blocking(self) -> None:
        fifo = self.root / "fifo"
        os.mkfifo(fifo, 0o600)
        self.refused(fifo, "regular file")


class TestToken(BundleCase):
    def test_token_read_from_the_bundle(self) -> None:
        self.write_bundle()
        member = self.load()
        self.assertEqual(member.token_path, self.home / "team-a" / "token")
        self.assertEqual(config.read_token(member, uid=self.uid), TOKEN)

    def test_token_file_must_be_token_inside_the_bundle(self) -> None:
        for bad in ("/etc/passwd", "../token", "sub/token", "", ".", "token2"):
            with self.subTest(bad=bad):
                self.setUp()
                self.write_bundle(data=member_json(token_file=bad))
                self.assert_refused(contains="token_file")

    def test_stricter_modes_accepted(self) -> None:
        for mode in (0o600, 0o400):
            with self.subTest(mode=oct(mode)):
                self.setUp()
                self.write_bundle(token_mode=mode)
                self.load()

    def test_mode_looser_than_0600_refused(self) -> None:
        for mode in (0o640, 0o604, 0o644, 0o660, 0o700, 0o601, 0o4600):
            with self.subTest(mode=oct(mode)):
                self.setUp()
                self.write_bundle(token_mode=mode)
                self.assert_refused(contains="mode")

    def test_token_owned_by_another_user_refused(self) -> None:
        self.write_bundle()
        with self.assertRaises(config.ConfigError) as caught:
            self.load(uid=self.uid + 1)
        self.assertIn("owned", str(caught.exception))

    def test_symlinked_token_refused(self) -> None:
        bundle = self.write_bundle()
        real = self.root / "elsewhere"
        real.write_text(TOKEN, encoding="utf-8")
        real.chmod(0o600)
        (bundle / "token").unlink()
        (bundle / "token").symlink_to(real)
        self.assert_refused(contains="regular file")

    def test_missing_token_refused(self) -> None:
        bundle = self.write_bundle()
        (bundle / "token").unlink()
        self.assert_refused(contains="token")

    def test_token_content_rules(self) -> None:
        for bad in ("", TOKEN + "\n", "two words", "tok\ten", "tést"):
            with self.subTest(bad=repr(bad)):
                self.setUp()
                self.write_bundle(token=bad)
                self.assert_refused(contains="token")

    def test_token_size_limit(self) -> None:
        self.write_bundle(token="a" * config.TOKEN_MAX_BYTES)
        self.load()
        self.setUp()
        self.write_bundle(token="a" * (config.TOKEN_MAX_BYTES + 1))
        self.assert_refused(contains="longer")

    def test_read_token_rechecks_mode(self) -> None:
        self.write_bundle()
        member = self.load()
        member.token_path.chmod(0o644)
        with self.assertRaises(config.ConfigError):
            config.read_token(member, uid=self.uid)

    def test_read_token_file_names_the_caller_and_never_the_token(self) -> None:
        path = self.root / "a-token"
        path.write_text(TOKEN, encoding="utf-8")
        path.chmod(0o600)
        self.assertEqual(config.read_token_file(path, "SOME_SOURCE", uid=self.uid), TOKEN)
        path.chmod(0o640)
        with self.assertRaises(config.ConfigError) as caught:
            config.read_token_file(path, "SOME_SOURCE", uid=self.uid)
        self.assertTrue(str(caught.exception).startswith("SOME_SOURCE: "))
        self.assertNotIn(TOKEN, str(caught.exception))

    def test_directory_token_refused(self) -> None:
        bundle = self.write_bundle()
        (bundle / "token").unlink()
        (bundle / "token").mkdir(mode=0o700)
        self.assert_refused(contains="regular file")

    def test_is_printable_token(self) -> None:
        self.assertTrue(config.is_printable_token(TOKEN))
        for bad in ("", TOKEN + "\n", "two words", "tok\ten", "tést", None, 5):
            with self.subTest(bad=repr(bad)):
                self.assertFalse(config.is_printable_token(bad))


class TestPlainHttp(BundleCase):
    def test_https_needs_no_listing(self) -> None:
        self.write_bundle(
            data=member_json(base_url="https://hs.example.com:8448", plain_http_hosts=[])
        )
        self.assertEqual(self.load().base_url, "https://hs.example.com:8448")

    def test_http_to_listed_ipv6_literal(self) -> None:
        self.write_bundle(
            data=member_json(base_url="http://[2001:db8::1]:8448", plain_http_hosts=["2001:db8::1"])
        )
        self.load()

    def test_http_refused_unless_listed_ip_literal(self) -> None:
        cases = (
            ("http://hs.example.com:8448", ["192.0.2.10"]),
            ("http://192.0.2.11:8448", ["192.0.2.10"]),
            ("http://192.0.2.10:8448", []),
            ("http://localhost:8448", ["127.0.0.1"]),
        )
        for url, hosts in cases:
            with self.subTest(url=url, hosts=hosts):
                self.setUp()
                self.write_bundle(data=member_json(base_url=url, plain_http_hosts=hosts))
                self.assert_refused(contains="base_url")

    def test_base_url_shape(self) -> None:
        for bad in (
            "ftp://192.0.2.10",
            "http://user@192.0.2.10:8448",
            "http://192.0.2.10:8448/path",
            "http://192.0.2.10:8448/?q=1",
            "http://192.0.2.10:8448#f",
            "http://192.0.2.10:99999",
            "http://192.0.2.10:x",
            "192.0.2.10:8448",
            "http://",
        ):
            with self.subTest(bad=bad):
                self.setUp()
                self.write_bundle(data=member_json(base_url=bad))
                self.assert_refused(contains="base_url")

    def test_plain_http_hosts_must_be_canonical_ip_literals(self) -> None:
        for bad in (
            ["hs.example.com"],
            ["192.0.2.010"],
            ["2001:DB8::1"],
            ["fe80::1%eth0"],
            ["192.0.2.10", "192.0.2.10"],
            "192.0.2.10",
            [7],
        ):
            with self.subTest(bad=bad):
                self.setUp()
                self.write_bundle(data=member_json(plain_http_hosts=bad))
                self.assert_refused(contains="plain_http_hosts")


class TestEnvironment(unittest.TestCase):
    def test_pingbus_home_from_environment(self) -> None:
        env = {"PINGBUS_HOME": "/srv/pingbus", "HOME": USER_HOME}
        self.assertEqual(config.resolve_home(env), pathlib.Path("/srv/pingbus"))

    def test_pingbus_home_must_be_absolute(self) -> None:
        with self.assertRaises(config.ConfigError):
            config.resolve_home({"PINGBUS_HOME": "rel/pingbus", "HOME": USER_HOME})

    def test_default_uses_xdg_config_home(self) -> None:
        env = {"XDG_CONFIG_HOME": f"{USER_HOME}/cfg", "HOME": USER_HOME}
        self.assertEqual(config.resolve_home(env), pathlib.Path(USER_HOME, "cfg", "pingbus"))

    def test_default_falls_back_to_dot_config(self) -> None:
        for env in (
            {"HOME": USER_HOME},
            {"HOME": USER_HOME, "PINGBUS_HOME": "", "XDG_CONFIG_HOME": ""},
            {"HOME": USER_HOME, "XDG_CONFIG_HOME": "relative/cfg"},
        ):
            with self.subTest(env=env):
                self.assertEqual(
                    config.resolve_home(env), pathlib.Path(USER_HOME, ".config", "pingbus")
                )

    def test_no_home_at_all_refused(self) -> None:
        for env in ({}, {"HOME": ""}, {"HOME": "relative"}):
            with self.subTest(env=env), self.assertRaises(config.ConfigError):
                config.resolve_home(env)

    def test_active_teams_parsed_in_order(self) -> None:
        self.assertEqual(config.active_teams({"PINGBUS_TEAMS": "team-a"}), ("team-a",))
        self.assertEqual(
            config.active_teams({"PINGBUS_TEAMS": "team-b,team-a"}), ("team-b", "team-a")
        )

    def test_active_teams_unset_or_empty_refused(self) -> None:
        for env in ({}, {"PINGBUS_TEAMS": ""}):
            with self.subTest(env=env), self.assertRaises(config.ConfigError) as caught:
                config.active_teams(env)
            self.assertIn("PINGBUS_TEAMS", str(caught.exception))

    def test_active_teams_malformed_refused(self) -> None:
        for value in ("team-a,", ",team-a", "team-a,,team-b", "team-a, team-b", "Team", "a,a"):
            with self.subTest(value=value), self.assertRaises(config.ConfigError):
                config.active_teams({"PINGBUS_TEAMS": value})

    def test_commands_without_config(self) -> None:
        self.assertEqual(
            config.COMMANDS_WITHOUT_CONFIG, frozenset({"version", "validate", "suggest-handle"})
        )


class TestTeamSelection(unittest.TestCase):
    def test_no_flag_covers_every_active_team(self) -> None:
        self.assertEqual(config.select_teams(("a", "b"), None), ("a", "b"))

    def test_flag_selects_one_active_team(self) -> None:
        self.assertEqual(config.select_teams(("a", "b"), "b"), ("b",))

    def test_flag_naming_an_inactive_team_is_usage_error(self) -> None:
        with self.assertRaises(config.UsageError):
            config.select_teams(("a", "b"), "c")

    def test_flag_naming_a_malformed_team_is_usage_error(self) -> None:
        with self.assertRaises(config.UsageError):
            config.select_teams(("a",), "../a")

    def test_single_team_command_needs_flag_when_several_active(self) -> None:
        with self.assertRaises(config.UsageError) as caught:
            config.select_teams(("a", "b"), None, single=True)
        self.assertIn("--team", str(caught.exception))

    def test_single_team_command_with_one_active(self) -> None:
        self.assertEqual(config.select_teams(("a",), None, single=True), ("a",))
        self.assertEqual(config.select_teams(("a", "b"), "a", single=True), ("a",))

    def test_exit_codes(self) -> None:
        self.assertEqual(config.ConfigError.EXIT_CODE, 78)
        self.assertEqual(config.UsageError.EXIT_CODE, 64)


class TestPythonGate(unittest.TestCase):
    def test_minimum_is_3_11(self) -> None:
        self.assertEqual(config.MIN_PYTHON, (3, 11))

    def test_older_python_refused(self) -> None:
        for version in ((3, 10, 14), (3, 9, 0), (2, 7, 18)):
            with self.subTest(version=version), self.assertRaises(config.ConfigError) as caught:
                config.check_python(version)
            self.assertIn("3.11", str(caught.exception))

    def test_supported_python_accepted(self) -> None:
        for version in ((3, 11, 0), (3, 12, 1), (3, 14, 0), (4, 0, 0)):
            with self.subTest(version=version):
                config.check_python(version)

    def test_running_interpreter_accepted(self) -> None:
        config.check_python(sys.version_info)


class TestElementProfile(BundleCase):
    def host_bundle(self) -> None:
        self.write_bundle(data=member_json(user_id=f"@{HOST_HANDLE}:{SN}"))

    def test_host_member_without_element_loads(self) -> None:
        self.host_bundle()
        self.assertEqual(self.load().member_type, "host")

    def test_host_member_beside_element_profile_refused(self) -> None:
        for rel in (
            ".var/app/im.riot.Riot",
            ".config/Element",
            ".config/Element-team-a",
        ):
            with self.subTest(rel=rel):
                self.setUp()
                self.host_bundle()
                (self.user_home / rel).mkdir(parents=True)
                self.assert_refused(contains="Element")

    def test_element_profile_found_through_a_dangling_symlink(self) -> None:
        self.host_bundle()
        (self.user_home / ".config").mkdir()
        (self.user_home / ".config" / "Element").symlink_to(self.root / "gone")
        self.assert_refused(contains="Element")

    def test_non_host_member_not_checked(self) -> None:
        self.write_bundle()
        (self.user_home / ".var/app/im.riot.Riot").mkdir(parents=True)
        self.assertEqual(self.load().member_type, "podman")

    def test_unrelated_config_dirs_ignored(self) -> None:
        self.host_bundle()
        (self.user_home / ".config" / "Elemental").mkdir(parents=True)
        (self.user_home / ".var/app/org.example.App").mkdir(parents=True)
        self.load()

    def test_unreadable_config_dir_refused(self) -> None:
        self.host_bundle()
        (self.user_home / ".config").mkdir()
        denied = PermissionError(13, "Permission denied")
        with mock.patch.object(pathlib.Path, "iterdir", side_effect=denied):
            self.assert_refused(contains="Permission denied")


class TestLoadActive(BundleCase):
    def env(self, teams: str) -> dict[str, str]:
        return {"PINGBUS_HOME": str(self.home), "PINGBUS_TEAMS": teams, "HOME": "/nonexistent"}

    def load_active(self, teams: str, team: str | None = None) -> tuple[config.Member, ...]:
        return config.load_active(
            self.env(teams), team=team, uid=self.uid, user_home=self.user_home
        )

    def test_every_active_team_loaded(self) -> None:
        self.write_bundle("team-a")
        self.write_bundle("team-b")
        members = self.load_active("team-a,team-b")
        self.assertEqual([m.team for m in members], ["team-a", "team-b"])

    def test_team_flag_loads_only_that_team(self) -> None:
        self.write_bundle("team-a")
        self.write_bundle("team-b")
        members = self.load_active("team-a,team-b", team="team-b")
        self.assertEqual([m.team for m in members], ["team-b"])

    def test_listed_team_without_bundle_refused(self) -> None:
        self.write_bundle("team-a")
        with self.assertRaises(config.ConfigError) as caught:
            self.load_active("team-a,team-b")
        self.assertIn("team-b", str(caught.exception))

    def test_unlisted_bundle_is_not_loaded(self) -> None:
        self.write_bundle("team-a")
        bundle = self.write_bundle("team-b")
        (bundle / "member.json").write_text("broken", encoding="utf-8")
        self.assertEqual([m.team for m in self.load_active("team-a")], ["team-a"])

    def test_accounts_on_different_teams_never_share_a_user_id(self) -> None:
        self.write_bundle("team-a")
        self.write_bundle("team-b", data=member_json(team="team-b"))
        with self.assertRaises(config.ConfigError) as caught:
            self.load_active("team-a,team-b")
        self.assertIn("user_id", str(caught.exception))

    def test_accounts_on_different_teams_never_share_a_token(self) -> None:
        self.write_bundle("team-a")
        self.write_bundle("team-b", token=TOKEN)
        with self.assertRaises(config.ConfigError) as caught:
            self.load_active("team-a,team-b")
        self.assertIn("token", str(caught.exception))
        self.assertNotIn(TOKEN, str(caught.exception))

    def test_older_python_refused_before_any_bundle_is_read(self) -> None:
        self.write_bundle("team-a")
        old = types.SimpleNamespace(version_info=(3, 10, 0))
        with mock.patch.object(config, "sys", old), self.assertRaises(config.ConfigError) as caught:
            self.load_active("team-a")
        self.assertEqual(caught.exception.EXIT_CODE, 78)
        self.assertIn("3.11", str(caught.exception))


if __name__ == "__main__":
    unittest.main()
