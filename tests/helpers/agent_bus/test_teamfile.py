"""Unit tests for helpers/agent_bus/teamfile.py: the team file's schema and shape rules.

Run from the repo root:

    python3 -m unittest tests.helpers.agent_bus.test_teamfile

The team file is the JSON a human writes and `agent-bus-install team` applies
(DESIGN.md section 3.4). These tests cover the offline rules only; whether a listen
address is really on `lo`, `agentbus0` or a WireGuard interface is the installer's
check against the live host.
"""

from __future__ import annotations

import copy
import json
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.agent_bus import teamfile

VALID = {
    "team": "alpha",
    "state": "present",
    "server_name": "alpha.agent-bus.internal",
    "port": 8448,
    "listen": ["192.0.2.10", "198.51.100.1"],
    "allow_from": ["198.51.100.0/24", "203.0.113.0/24"],
    "humans": ["alice", "bob_2"],
    "repos": [{"repo": "example-org/myrepo", "branches": ["main"]}],
    "path_prefixes": ["CLAUDE/Plan/", "docs/"],
    "forge_api": "https://api.github.com",
}


def _with(**changes: object) -> dict:
    data = copy.deepcopy(VALID)
    data.update(changes)
    return data


def _without(key: str) -> dict:
    data = copy.deepcopy(VALID)
    del data[key]
    return data


class ValidTeamFileTest(unittest.TestCase):
    def test_full_file_parses(self) -> None:
        tf = teamfile.parse_team_file(VALID)
        self.assertEqual(tf.team, "alpha")
        self.assertEqual(tf.state, "present")
        self.assertEqual(tf.server_name, "alpha.agent-bus.internal")
        self.assertEqual(tf.port, 8448)
        self.assertEqual(tf.listen, ("192.0.2.10", "198.51.100.1"))
        self.assertEqual(tf.allow_from, ("198.51.100.0/24", "203.0.113.0/24"))
        self.assertEqual(tf.humans, ("alice", "bob_2"))
        self.assertEqual(tf.repos, (teamfile.RepoEntry("example-org/myrepo", ("main",)),))
        self.assertEqual(tf.path_prefixes, ("CLAUDE/Plan/", "docs/"))
        self.assertEqual(tf.forge_api, "https://api.github.com")

    def test_server_name_defaults_from_team(self) -> None:
        tf = teamfile.parse_team_file(_without("server_name"))
        self.assertEqual(tf.server_name, "alpha.agent-bus.internal")

    def test_state_defaults_to_present(self) -> None:
        self.assertEqual(teamfile.parse_team_file(_without("state")).state, "present")

    def test_state_absent_accepted(self) -> None:
        self.assertEqual(teamfile.parse_team_file(_with(state="absent")).state, "absent")

    def test_empty_listen_and_allow_from_accepted(self) -> None:
        tf = teamfile.parse_team_file(_with(listen=[], allow_from=[]))
        self.assertEqual((tf.listen, tf.allow_from), ((), ()))

    def test_ipv6_listen_and_cidr_accepted(self) -> None:
        tf = teamfile.parse_team_file(_with(listen=["2001:db8::1"], allow_from=["2001:db8::/64"]))
        self.assertEqual(tf.listen, ("2001:db8::1",))
        self.assertEqual(tf.allow_from, ("2001:db8::/64",))

    def test_exact_file_path_prefix_accepted(self) -> None:
        tf = teamfile.parse_team_file(_with(path_prefixes=["docs/agent-bus.md"]))
        self.assertEqual(tf.path_prefixes, ("docs/agent-bus.md",))

    def test_listen_addresses_include_loopback_first(self) -> None:
        tf = teamfile.parse_team_file(VALID)
        self.assertEqual(tf.listen_addresses(), ("127.0.0.1", "192.0.2.10", "198.51.100.1"))


class RefusedTeamFileTest(unittest.TestCase):
    """Every rule refuses with TeamFileError naming the offending key."""

    CASES = {
        "not an object": ([], "team file"),
        "unknown key": (_with(extra=1), "extra"),
        "missing team": (_without("team"), "team"),
        "missing port": (_without("port"), "port"),
        "missing humans": (_without("humans"), "humans"),
        "missing repos": (_without("repos"), "repos"),
        "missing path_prefixes": (_without("path_prefixes"), "path_prefixes"),
        "missing forge_api": (_without("forge_api"), "forge_api"),
        "missing listen": (_without("listen"), "listen"),
        "missing allow_from": (_without("allow_from"), "allow_from"),
        "team upper case": (_with(team="Alpha"), "team"),
        "team leading digit": (_with(team="1alpha"), "team"),
        "team too long": (_with(team="a" * 25), "team"),
        "team not a string": (_with(team=5), "team"),
        "state unknown": (_with(state="gone"), "state"),
        "server_name not internal": (_with(server_name="alpha.example.com"), "server_name"),
        "server_name bare internal": (_with(server_name="internal"), "server_name"),
        "server_name with port": (_with(server_name="alpha.internal:8448"), "server_name"),
        "server_name upper case": (_with(server_name="Alpha.internal"), "server_name"),
        "port bool": (_with(port=True), "port"),
        "port string": (_with(port="8448"), "port"),
        "port float": (_with(port=8448.0), "port"),
        "port privileged": (_with(port=443), "port"),
        "port too high": (_with(port=65536), "port"),
        "listen not a list": (_with(listen="192.0.2.10"), "listen"),
        "listen hostname": (_with(listen=["bus.example.com"]), "listen"),
        "listen cidr": (_with(listen=["192.0.2.0/24"]), "listen"),
        "listen any v4": (_with(listen=["0.0.0.0"]), "listen"),
        "listen any v6": (_with(listen=["::"]), "listen"),
        "listen multicast": (_with(listen=["224.0.0.1"]), "listen"),
        "listen 127.0.0.1 is implicit": (_with(listen=["127.0.0.1"]), "listen"),
        "listen duplicate": (_with(listen=["192.0.2.10", "192.0.2.10"]), "listen"),
        "listen non-canonical": (_with(listen=["2001:DB8::1"]), "listen"),
        "listen zone id": (_with(listen=["fe80::1%eth0"]), "listen"),
        "allow_from host bits set": (_with(allow_from=["198.51.100.1/24"]), "allow_from"),
        "allow_from default route v4": (_with(allow_from=["0.0.0.0/0"]), "allow_from"),
        "allow_from default route v6": (_with(allow_from=["::/0"]), "allow_from"),
        "allow_from bare address": (_with(allow_from=["198.51.100.1"]), "allow_from"),
        "allow_from multicast": (_with(allow_from=["224.0.0.0/4"]), "allow_from"),
        "allow_from not cidr": (_with(allow_from=["lan"]), "allow_from"),
        "allow_from duplicate": (_with(allow_from=["203.0.113.0/24", "203.0.113.0/24"]), "allow_from"),
        "humans empty": (_with(humans=[]), "humans"),
        "humans too many": (_with(humans=[f"h{i}" for i in range(17)]), "humans"),
        "human reserved admin": (_with(humans=["admin"]), "humans"),
        "human reserved conduit": (_with(humans=["conduit"]), "humans"),
        "human with plus": (_with(humans=["al+ice"]), "humans"),
        "human upper case": (_with(humans=["Alice"]), "humans"),
        "human duplicate": (_with(humans=["alice", "alice"]), "humans"),
        "repos empty": (_with(repos=[]), "repos"),
        "repos too many": (
            _with(repos=[{"repo": f"o/r{i}", "branches": ["main"]} for i in range(33)]),
            "repos",
        ),
        "repo entry unknown key": (_with(repos=[{"repo": "o/r", "branches": ["main"], "x": 1}]), "repos"),
        "repo upper case": (_with(repos=[{"repo": "Example/r", "branches": ["main"]}]), "repos"),
        "repo no owner": (_with(repos=[{"repo": "myrepo", "branches": ["main"]}]), "repos"),
        "repo ends .git": (_with(repos=[{"repo": "o/r.git", "branches": ["main"]}]), "repos"),
        "repo is dot": (_with(repos=[{"repo": "o/.", "branches": ["main"]}]), "repos"),
        "repo duplicate": (
            _with(repos=[{"repo": "o/r", "branches": ["main"]}, {"repo": "o/r", "branches": ["dev"]}]),
            "repos",
        ),
        "branches empty": (_with(repos=[{"repo": "o/r", "branches": []}]), "repos"),
        "branches too many": (_with(repos=[{"repo": "o/r", "branches": [f"b{i}" for i in range(9)]}]), "repos"),
        "branch with space": (_with(repos=[{"repo": "o/r", "branches": ["my branch"]}]), "repos"),
        "branch duplicate": (_with(repos=[{"repo": "o/r", "branches": ["main", "main"]}]), "repos"),
        "path_prefixes empty": (_with(path_prefixes=[]), "path_prefixes"),
        "path prefix leading slash": (_with(path_prefixes=["/docs/"]), "path_prefixes"),
        "path prefix dotdot": (_with(path_prefixes=["docs/../"]), "path_prefixes"),
        "path prefix dot": (_with(path_prefixes=["./docs/"]), "path_prefixes"),
        "path prefix double slash": (_with(path_prefixes=["docs//"]), "path_prefixes"),
        "path prefix too deep": (_with(path_prefixes=["a/b/c/d/e/f/g/h/i/"]), "path_prefixes"),
        "path prefix duplicate": (_with(path_prefixes=["docs/", "docs/"]), "path_prefixes"),
        "forge_api http": (_with(forge_api="http://api.github.com"), "forge_api"),
        "forge_api not url": (_with(forge_api="api.github.com"), "forge_api"),
        "forge_api with credentials": (_with(forge_api="https://user:pw@example.com"), "forge_api"),
        "forge_api with query": (_with(forge_api="https://api.github.com/?x=1"), "forge_api"),
        "forge_api trailing slash": (_with(forge_api="https://api.github.com/"), "forge_api"),
    }

    def test_each_rule_refuses(self) -> None:
        for name, (data, key) in self.CASES.items():
            with self.subTest(name), self.assertRaises(teamfile.TeamFileError) as caught:
                teamfile.parse_team_file(data)
            self.assertIn(key, str(caught.exception), name)

    def test_refusal_is_a_value_error(self) -> None:
        self.assertTrue(issubclass(teamfile.TeamFileError, ValueError))


class LoadAndRoundTripTest(unittest.TestCase):
    def _write(self, text: str) -> pathlib.Path:
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        path = pathlib.Path(tmp.name) / "team.json"
        path.write_text(text, encoding="utf-8")
        return path

    def test_load_reads_a_file(self) -> None:
        tf = teamfile.load_team_file(self._write(json.dumps(VALID)))
        self.assertEqual(tf, teamfile.parse_team_file(VALID))

    def test_round_trip_is_stable(self) -> None:
        first = teamfile.parse_team_file(_without("server_name"))
        text = teamfile.dump_team_file(first)
        second = teamfile.parse_team_file(json.loads(text))
        self.assertEqual(first, second)
        self.assertEqual(text, teamfile.dump_team_file(second))
        self.assertTrue(text.endswith("\n"))
        self.assertEqual(json.loads(text)["server_name"], "alpha.agent-bus.internal")

    def test_load_refuses_duplicate_keys(self) -> None:
        text = json.dumps(VALID)[:-1] + ', "team": "beta"}'
        with self.assertRaises(teamfile.TeamFileError):
            teamfile.load_team_file(self._write(text))

    def test_load_refuses_invalid_json(self) -> None:
        with self.assertRaises(teamfile.TeamFileError):
            teamfile.load_team_file(self._write("{not json"))

    def test_load_refuses_nan(self) -> None:
        with self.assertRaises(teamfile.TeamFileError):
            teamfile.load_team_file(self._write(json.dumps(VALID)[:-1] + ', "port2": NaN}'))

    def test_load_missing_file_raises(self) -> None:
        with self.assertRaises(FileNotFoundError):
            teamfile.load_team_file(pathlib.Path("/nonexistent/agent-bus/team.json"))


if __name__ == "__main__":
    unittest.main()
