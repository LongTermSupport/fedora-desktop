"""Unit tests for helpers/agent_bus/render.py: `tuwunel.toml` and the unit drop-in.

Run from the repo root:

    python3 -m unittest tests.helpers.agent_bus.test_render

DESIGN.md section 3.6 lists every `tuwunel.toml` key and its value; section 3.3 and 3.5
give the drop-in: the unit's IP filter (`IPAddressDeny=any`, `IPAddressAllow=` exactly
loopback, the listen addresses and `allow_from`), the socket-bind limit, the restart
policy, and an `After=`/`Wants=` on the device of every interface carrying a listen
address.
"""

from __future__ import annotations

import configparser
import pathlib
import sys
import tomllib
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.agent_bus import render, teamfile


def team_data(**overrides: object) -> dict:
    data = {
        "team": "team-a",
        "port": 8448,
        "listen": ["192.0.2.10", "198.51.100.7"],
        "allow_from": ["192.0.2.0/24", "2001:db8::/64"],
        "humans": ["alice"],
        "repos": [{"repo": "example-org/myrepo", "branches": ["main"]}],
        "path_prefixes": ["CLAUDE/Plan/", "docs/"],
        "forge_api": "https://api.github.com",
    }
    data.update(overrides)
    return data


def team(**overrides: object) -> teamfile.TeamFile:
    return teamfile.parse_team_file(team_data(**overrides))


#: DESIGN.md section 3.6, key for key.
EXPECTED_TOML = {
    "server_name": "team-a.agent-bus.internal",
    "address": ["127.0.0.1", "192.0.2.10", "198.51.100.7"],
    "port": 8448,
    "database_path": "/var/lib/agent-bus/team-a/db",
    "database_backup_path": "/var/lib/agent-bus/team-a/backups",
    "database_backups_to_keep": 7,
    "allow_registration": False,
    "allow_guest_registration": False,
    "registration_shared_secret_file": "/var/lib/agent-bus/team-a/secrets/registration_shared_secret",
    "grant_admin_to_first_user": False,
    "login_via_token": False,
    "login_via_existing_session": False,
    "allow_federation": False,
    "trusted_servers": [],
    "federate_admin_room": False,
    "admin_escape_commands": False,
    "allow_encryption": False,
    "auto_accept_invites": False,
    "new_user_displayname_suffix": "",
    "client_sync_timeout_min": 0,
    "default_room_version": "12",
    "sentry": False,
    "log": "warn",
    "admin_signal_execute": ["server backup-database"],
    "error_on_unknown_config_opts": True,
    "rocksdb_allow_fallocate": False,
}


class TomlTest(unittest.TestCase):
    def test_exact_key_set_and_values(self) -> None:
        # Top-level keys, as probe H4 started Tuwunel 1.9.3 with them.
        self.assertEqual(tomllib.loads(render.render_toml(team())), EXPECTED_TOML)

    def test_key_constant_matches_design_table(self) -> None:
        self.assertEqual(set(render.TOML_KEYS), set(EXPECTED_TOML))
        self.assertEqual(len(render.TOML_KEYS), len(set(render.TOML_KEYS)))

    def test_no_listen_means_loopback_only(self) -> None:
        parsed = tomllib.loads(render.render_toml(team(listen=[])))
        self.assertEqual(parsed["address"], ["127.0.0.1"])

    def test_explicit_server_name_is_used(self) -> None:
        parsed = tomllib.loads(render.render_toml(team(server_name="lab.internal")))
        self.assertEqual(parsed["server_name"], "lab.internal")

    def test_render_is_deterministic(self) -> None:
        self.assertEqual(render.render_toml(team()), render.render_toml(team()))


def parse_unit(text: str) -> dict[str, dict[str, list[str]]]:
    """A systemd unit file as section -> key -> every value, in order."""
    sections: dict[str, dict[str, list[str]]] = {}
    current = None
    for line in text.splitlines():
        if not line or line.startswith("#"):
            continue
        if line.startswith("["):
            current = sections.setdefault(line.strip("[]"), {})
            continue
        key, _, value = line.partition("=")
        current.setdefault(key, []).append(value)
    return sections


class DropinTest(unittest.TestCase):
    IFACES = {"192.0.2.10": "agentbus0", "198.51.100.7": "wg-lab"}

    def dropin(self, tf: teamfile.TeamFile | None = None, ifaces: dict | None = None) -> dict:
        text = render.render_dropin(tf or team(), self.IFACES if ifaces is None else ifaces)
        configparser.ConfigParser(strict=False, interpolation=None).read_string(text)
        return parse_unit(text)

    def test_ip_filter_is_exact(self) -> None:
        service = self.dropin()["Service"]
        self.assertEqual(service["IPAddressDeny"], ["any"])
        self.assertEqual(
            service["IPAddressAllow"],
            ["127.0.0.1/32 ::1/128 192.0.2.10/32 198.51.100.7/32 192.0.2.0/24 2001:db8::/64"],
        )

    def test_socket_bind_is_the_port_only(self) -> None:
        service = self.dropin()["Service"]
        self.assertEqual(service["SocketBindDeny"], ["any"])
        self.assertEqual(service["SocketBindAllow"], ["tcp:8448"])

    def test_restart_policy(self) -> None:
        unit = self.dropin()
        self.assertEqual(unit["Service"]["Restart"], ["on-failure"])
        self.assertEqual(unit["Service"]["RestartSec"], ["5s"])
        self.assertEqual(unit["Unit"]["StartLimitIntervalSec"], ["0"])

    def test_device_dependencies_escaped_and_deduplicated(self) -> None:
        tf = team(listen=["192.0.2.10", "192.0.2.11", "198.51.100.7"])
        ifaces = {"192.0.2.10": "agentbus0", "192.0.2.11": "agentbus0", "198.51.100.7": "wg-lab"}
        unit = self.dropin(tf, ifaces)["Unit"]
        expected = "sys-subsystem-net-devices-agentbus0.device sys-subsystem-net-devices-wg\\x2dlab.device"
        self.assertEqual(unit["After"], [expected])
        self.assertEqual(unit["Wants"], [expected])

    def test_no_listen_has_no_device_dependency(self) -> None:
        unit = self.dropin(team(listen=[]), {})["Unit"]
        self.assertNotIn("After", unit)
        self.assertNotIn("Wants", unit)

    def test_every_listen_address_needs_exactly_its_interface(self) -> None:
        with self.assertRaisesRegex(render.RenderError, "198.51.100.7"):
            render.render_dropin(team(), {"192.0.2.10": "agentbus0"})
        with self.assertRaisesRegex(render.RenderError, "203.0.113.1"):
            render.render_dropin(team(), {**self.IFACES, "203.0.113.1": "eth0"})

    def test_device_unit_name(self) -> None:
        self.assertEqual(render.device_unit("eth0"), "sys-subsystem-net-devices-eth0.device")
        self.assertEqual(render.device_unit("wg_lab.1"), "sys-subsystem-net-devices-wg_lab.1.device")
        for bad in ("", ".", "..", "a/b", "a b", "x" * 16, "a\nb", "a:b"):
            with self.subTest(bad=bad), self.assertRaises(render.RenderError):
                render.device_unit(bad)


class ParseInterfacesTest(unittest.TestCase):
    def test_pairs(self) -> None:
        self.assertEqual(
            render.parse_interfaces(["192.0.2.10=agentbus0", "2001:db8::1=wg0"]),
            {"192.0.2.10": "agentbus0", "2001:db8::1": "wg0"},
        )

    def test_refusals(self) -> None:
        for bad in (["192.0.2.10"], ["=eth0"], ["192.0.2.10=eth0", "192.0.2.10=eth1"], ["host=eth0"]):
            with self.subTest(bad=bad), self.assertRaises(render.RenderError):
                render.parse_interfaces(bad)


class CheckTest(unittest.TestCase):
    def test_server_name_never_changes(self) -> None:
        previous = team()
        render.check_unchanged(team(), previous)
        render.check_unchanged(team(), None)
        with self.assertRaisesRegex(render.RenderError, "server_name"):
            render.check_unchanged(team(server_name="other.internal"), previous)

    def test_team_name_never_changes(self) -> None:
        with self.assertRaisesRegex(render.RenderError, "team"):
            render.check_unchanged(team(team="team-b"), team())


if __name__ == "__main__":
    unittest.main()
