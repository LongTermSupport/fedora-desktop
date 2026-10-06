"""Unit tests for helpers/agent_bus/element.py: the Element Desktop team profiles.

Run from the repo root:

    python3 -m unittest tests.helpers.agent_bus.test_element

Plan 00161's DESIGN.md section 8 is the rule: per team, the Flatpak profile
`~/.var/app/im.riot.Riot/config/Element-<team>/config.json` carrying the locked-down keys
of the clients research (subagent-reports/261006-research-clients-opus-5-5.md §1.4), a
spell-check seed when absent, and a launcher running
`flatpak run im.riot.Riot --profile <team>`; and no profile for a user who has
`~/.config/pingbus/` (an agent user).
"""

from __future__ import annotations

import io
import json
import pathlib
import stat
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.agent_bus import element
from helpers.pingbus import config

BASE_URL = "http://192.0.2.10:8448"
HOME = pathlib.Path("/home/<user>")

#: The clients research §1.4, key for key, for team `team-a` at BASE_URL.
EXPECTED_CONFIG = {
    "default_server_config": {
        "m.homeserver": {"base_url": BASE_URL, "server_name": "team-a.agent-bus.internal"},
    },
    "disable_custom_urls": True,
    "disable_guests": True,
    "disable_3pid_login": True,
    "disable_login_language_selector": True,
    "enable_client_well_known_lookups": False,
    "update_base_url": None,
    "integrations_ui_url": None,
    "integrations_rest_url": None,
    "integrations_widgets_urls": [],
    "bug_report_endpoint_url": None,
    "posthog": None,
    "sentry": None,
    "privacy_policy_url": None,
    "terms_and_conditions_links": [],
    "map_style_url": None,
    "jitsi": {"preferred_domain": "192.0.2.10"},
    "element_call": {"disable": True},
    "features": {
        "feature_video_rooms": False,
        "feature_group_calls": False,
        "feature_element_call_video_rooms": False,
    },
    "room_directory": {"servers": ["team-a.agent-bus.internal"]},
    "show_labs_settings": False,
    "mobile_guide_toast": False,
    "setting_defaults": {
        "UIFeature.urlPreviews": False,
        "UIFeature.voip": False,
        "UIFeature.widgets": False,
        "UIFeature.locationSharing": False,
        "UIFeature.identityServer": False,
        "UIFeature.thirdPartyId": False,
        "UIFeature.registration": False,
        "UIFeature.passwordReset": False,
        "UIFeature.deactivate": False,
        "UIFeature.feedback": False,
        "UIFeature.shareSocial": False,
        "UIFeature.allowCreatingPublicRooms": False,
        "UIFeature.allowCreatingPublicSpaces": False,
        "fallbackICEServerAllowed": False,
    },
}


def profile(**overrides: object) -> element.Profile:
    data = {"team": "team-a", "base_url": BASE_URL}
    data.update(overrides)
    return element.parse_profiles([data])[0]


class TestConfigJson(unittest.TestCase):
    def test_every_key_is_the_research_value(self) -> None:
        self.assertEqual(element.config_json(profile()), EXPECTED_CONFIG)

    def test_no_identity_server_is_named(self) -> None:
        server_config = element.config_json(profile())["default_server_config"]
        self.assertEqual(list(server_config), ["m.homeserver"])

    def test_jitsi_points_at_the_homeserver_host(self) -> None:
        cfg = element.config_json(profile(base_url="https://bus.example.com"))
        self.assertEqual(cfg["jitsi"], {"preferred_domain": "bus.example.com"})

    def test_ipv6_host_has_no_brackets_in_jitsi(self) -> None:
        cfg = element.config_json(profile(base_url="http://[2001:db8::1]:8448"))
        self.assertEqual(cfg["jitsi"], {"preferred_domain": "2001:db8::1"})

    def test_explicit_server_name_is_used_everywhere(self) -> None:
        cfg = element.config_json(profile(server_name="other.agent-bus.internal"))
        self.assertEqual(cfg["default_server_config"]["m.homeserver"]["server_name"],
                         "other.agent-bus.internal")
        self.assertEqual(cfg["room_directory"], {"servers": ["other.agent-bus.internal"]})


class TestParseProfiles(unittest.TestCase):
    def test_empty_list_is_no_profiles(self) -> None:
        self.assertEqual(element.parse_profiles([]), ())

    def test_default_server_name_is_the_team_files(self) -> None:
        self.assertEqual(profile().server_name, "team-a.agent-bus.internal")

    def test_refusals(self) -> None:
        cases = {
            "not a list": {"team": "team-a", "base_url": BASE_URL},
            "entry not an object": ["team-a"],
            "bad team": [{"team": "Team_A", "base_url": BASE_URL}],
            "team missing": [{"base_url": BASE_URL}],
            "base_url missing": [{"team": "team-a"}],
            "unknown key": [{"team": "team-a", "base_url": BASE_URL, "port": 8448}],
            "duplicate team": [{"team": "team-a", "base_url": BASE_URL},
                               {"team": "team-a", "base_url": "http://192.0.2.11:8448"}],
            "http to a name": [{"team": "team-a", "base_url": "http://bus.example.com:8448"}],
            "a path": [{"team": "team-a", "base_url": BASE_URL + "/_matrix"}],
            "credentials": [{"team": "team-a", "base_url": "http://u:p@192.0.2.10:8448"}],
            "another scheme": [{"team": "team-a", "base_url": "ftp://192.0.2.10"}],
            "server_name not .internal": [{"team": "team-a", "base_url": BASE_URL,
                                          "server_name": "team-a.example.com"}],
        }
        for name, data in cases.items():
            with self.subTest(name), self.assertRaises(element.ElementError):
                element.parse_profiles(data)

    def test_a_refusal_never_quotes_the_url(self) -> None:
        secret_url = "http://user:hunter2@192.0.2.10:8448"
        with self.assertRaises(element.ElementError) as caught:
            element.parse_profiles([{"team": "team-a", "base_url": secret_url}])
        self.assertNotIn("hunter2", str(caught.exception))


class TestPaths(unittest.TestCase):
    def test_profile_dir_is_the_flatpak_config_named_by_profile(self) -> None:
        self.assertEqual(element.profile_dir(HOME, "team-a"),
                         HOME / ".var/app/im.riot.Riot/config/Element-team-a")

    def test_profile_dir_is_inside_what_the_member_refusal_looks_for(self) -> None:
        self.assertTrue(element.profile_dir(HOME, "team-a").is_relative_to(
            HOME / config.ELEMENT_FLATPAK_DIR))

    def test_pingbus_dir_is_the_default_pingbus_home(self) -> None:
        self.assertEqual(element.pingbus_dir(HOME), config.resolve_home({"HOME": str(HOME)}))

    def test_launcher(self) -> None:
        self.assertEqual(element.launcher_path(HOME, "team-a"),
                         HOME / ".local/share/applications/agent-bus-team-a-element.desktop")
        text = element.launcher_text(profile())
        self.assertTrue(text.startswith("[Desktop Entry]\n"))
        self.assertIn("\nExec=flatpak run im.riot.Riot --profile team-a\n", text)
        self.assertIn("\nIcon=im.riot.Riot\n", text)
        self.assertIn("\nTerminal=false\n", text)
        self.assertTrue(text.endswith("\n"))


class HomeCase(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.home = pathlib.Path(self._tmp.name)

    def written(self) -> list[pathlib.Path]:
        return sorted(p for p in self.home.rglob("*") if p.is_file() or p.is_symlink())


class TestRefusal(HomeCase):
    def test_refuses_a_user_with_a_pingbus_dir_and_writes_nothing(self) -> None:
        (self.home / ".config" / "pingbus").mkdir(parents=True)
        with self.assertRaises(element.ElementError) as caught:
            element.apply(self.home, (profile(),))
        self.assertEqual(caught.exception.EXIT_CODE, 78)
        self.assertIn("pingbus", str(caught.exception))
        self.assertFalse((self.home / ".var").exists())
        self.assertFalse((self.home / ".local").exists())

    def test_a_dangling_symlink_counts(self) -> None:
        (self.home / ".config").mkdir()
        (self.home / ".config" / "pingbus").symlink_to(self.home / "nowhere")
        with self.assertRaises(element.ElementError):
            element.check_user(self.home)

    def test_a_pingbus_file_counts(self) -> None:
        (self.home / ".config").mkdir()
        (self.home / ".config" / "pingbus").write_text("")
        with self.assertRaises(element.ElementError):
            element.check_user(self.home)

    def test_no_pingbus_dir_passes(self) -> None:
        element.check_user(self.home)
        (self.home / ".config").mkdir()
        element.check_user(self.home)

    def test_an_unsearchable_config_refuses(self) -> None:
        denied = PermissionError(13, "Permission denied")
        with mock.patch.object(element.os, "lstat", side_effect=denied):
            with self.assertRaisesRegex(element.ElementError, "cannot be checked"):
                element.check_user(self.home)


class TestApply(HomeCase):
    def test_writes_profile_seed_and_launcher(self) -> None:
        changed = element.apply(self.home, (profile(),))
        pdir = self.home / ".var/app/im.riot.Riot/config/Element-team-a"
        launcher = self.home / ".local/share/applications/agent-bus-team-a-element.desktop"
        self.assertEqual(changed, [pdir / "config.json", pdir / "electron-config.json", launcher])
        self.assertEqual(json.loads((pdir / "config.json").read_text()), EXPECTED_CONFIG)
        self.assertEqual(json.loads((pdir / "electron-config.json").read_text()),
                         {"spellCheckerEnabled": False})
        self.assertEqual(launcher.read_text(), element.launcher_text(profile()))
        self.assertEqual(stat.S_IMODE(pdir.stat().st_mode), 0o700)
        self.assertEqual(stat.S_IMODE((pdir / "config.json").stat().st_mode), 0o600)
        self.assertEqual(stat.S_IMODE(launcher.stat().st_mode), 0o644)

    def test_second_run_changes_nothing(self) -> None:
        element.apply(self.home, (profile(),))
        self.assertEqual(element.apply(self.home, (profile(),)), [])

    def test_existing_spellcheck_store_is_left_alone(self) -> None:
        pdir = self.home / ".var/app/im.riot.Riot/config/Element-team-a"
        pdir.mkdir(parents=True)
        (pdir / "electron-config.json").write_text('{"spellCheckerEnabled": true}')
        changed = element.apply(self.home, (profile(),))
        self.assertNotIn(pdir / "electron-config.json", changed)
        self.assertEqual((pdir / "electron-config.json").read_text(),
                         '{"spellCheckerEnabled": true}')

    def test_a_changed_address_rewrites_config_only(self) -> None:
        element.apply(self.home, (profile(),))
        moved = profile(base_url="http://192.0.2.11:8448")
        pdir = self.home / ".var/app/im.riot.Riot/config/Element-team-a"
        self.assertEqual(element.apply(self.home, (moved,)), [pdir / "config.json"])
        self.assertEqual(
            json.loads((pdir / "config.json").read_text())["default_server_config"]
            ["m.homeserver"]["base_url"], "http://192.0.2.11:8448")

    def test_a_symlinked_config_is_refused(self) -> None:
        pdir = self.home / ".var/app/im.riot.Riot/config/Element-team-a"
        pdir.mkdir(parents=True)
        target = self.home / "elsewhere"
        (pdir / "config.json").symlink_to(target)
        with self.assertRaises(element.ElementError):
            element.apply(self.home, (profile(),))
        self.assertFalse(target.exists())


class TestMain(HomeCase):
    def run_main(self, argv: list[str], stdin: str) -> tuple[int, str, str]:
        out, err = io.StringIO(), io.StringIO()
        with mock.patch.object(element, "_running_home", return_value=self.home), \
                mock.patch.object(element.os, "geteuid", return_value=1000), \
                mock.patch("sys.stdin", io.StringIO(stdin)), \
                mock.patch("sys.stdout", out), mock.patch("sys.stderr", err):
            code = element.main(argv)
        return code, out.getvalue(), err.getvalue()

    def test_apply_prints_changed_markers(self) -> None:
        data = json.dumps([{"team": "team-a", "base_url": BASE_URL}])
        code, out, _ = self.run_main(["apply"], data)
        self.assertEqual(code, 0)
        lines = out.splitlines()
        self.assertEqual(len(lines), 3)
        self.assertTrue(all(line.startswith("CHANGED\t") for line in lines))
        code, out, _ = self.run_main(["apply"], data)
        self.assertEqual((code, out), (0, ""))

    def test_check_writes_nothing(self) -> None:
        data = json.dumps([{"team": "team-a", "base_url": BASE_URL}])
        code, out, _ = self.run_main(["check"], data)
        self.assertEqual((code, out), (0, ""))
        self.assertEqual(self.written(), [])

    def test_refusal_exits_78_on_stderr(self) -> None:
        (self.home / ".config" / "pingbus").mkdir(parents=True)
        code, out, err = self.run_main(["check"], "[]")
        self.assertEqual((code, out), (78, ""))
        self.assertIn("pingbus", err)

    def test_bad_json_exits_78(self) -> None:
        code, out, _ = self.run_main(["apply"], '[{"team": "a", "team": "b"}]')
        self.assertEqual((code, out), (78, ""))
        self.assertEqual(self.written(), [])

    def test_root_is_refused(self) -> None:
        with mock.patch.object(element, "_running_home", return_value=self.home), \
                mock.patch.object(element.os, "geteuid", return_value=0), \
                mock.patch("sys.stdin", io.StringIO("[]")), \
                mock.patch("sys.stdout", io.StringIO()), mock.patch("sys.stderr", io.StringIO()):
            self.assertEqual(element.main(["check"]), 78)


if __name__ == "__main__":
    unittest.main()
