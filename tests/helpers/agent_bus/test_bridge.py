"""Unit tests for helpers/agent_bus/bridge.py: the LXC bridge subnet every team admits.

Run from the repo root:

    python3 -m unittest tests.helpers.agent_bus.test_bridge

Addresses are RFC 5737 documentation ranges.
"""

from __future__ import annotations

import io
import json
import pathlib
import sys
import unittest
from contextlib import redirect_stderr, redirect_stdout

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.agent_bus import bridge

FACTS = {"ipv4": {"address": "192.0.2.1", "network": "192.0.2.0", "netmask": "255.255.255.0", "prefix": "24"}}


class BridgeSubnet(unittest.TestCase):
    def test_subnet_from_network_and_prefix(self):
        self.assertEqual(bridge.subnet("lxcbr0", FACTS), "192.0.2.0/24")

    def test_subnet_from_address_and_netmask_when_no_network(self):
        facts = {"ipv4": {"address": "192.0.2.1", "netmask": "255.255.255.0"}}
        self.assertEqual(bridge.subnet("lxcbr0", facts), "192.0.2.0/24")

    def test_absent_bridge_gives_none(self):
        self.assertIsNone(bridge.subnet("lxcbr0", None))

    def test_bridge_without_ipv4_fails(self):
        for facts in ({}, {"ipv4": {}}, {"ipv6": []}):
            with self.assertRaisesRegex(bridge.BridgeError, "lxcbr0.*no IPv4.*lxc-net"):
                bridge.subnet("lxcbr0", facts)


class EffectiveAllowFrom(unittest.TestCase):
    def test_declared_plus_bridge(self):
        self.assertEqual(bridge.effective(["198.51.100.0/24"], "192.0.2.0/24"), ["198.51.100.0/24", "192.0.2.0/24"])

    def test_no_duplicate_when_declared_already(self):
        self.assertEqual(bridge.effective(["192.0.2.0/24"], "192.0.2.0/24"), ["192.0.2.0/24"])

    def test_no_bridge_leaves_declared(self):
        self.assertEqual(bridge.effective(["198.51.100.0/24"], None), ["198.51.100.0/24"])

    def test_missing_declared_is_empty(self):
        self.assertEqual(bridge.effective(None, "192.0.2.0/24"), ["192.0.2.0/24"])


class Apply(unittest.TestCase):
    def teams(self):
        return [{"team": "a", "allow_from": []}, {"team": "b", "allow_from": ["198.51.100.0/24"], "port": 1}, {"team": "c"}]

    def test_every_team_gets_the_subnet_and_keeps_its_keys(self):
        out = bridge.apply("lxcbr0", FACTS, self.teams())
        self.assertEqual([t["allow_from"] for t in out["teams"]],
                         [["192.0.2.0/24"], ["198.51.100.0/24", "192.0.2.0/24"], ["192.0.2.0/24"]])
        self.assertEqual(out["teams"][1]["port"], 1)
        self.assertIn("192.0.2.0/24", out["note"])

    def test_absent_bridge_says_so_in_one_line_and_adds_nothing(self):
        out = bridge.apply("lxcbr0", None, self.teams())
        self.assertEqual(out["teams"][0]["allow_from"], [])
        self.assertNotIn("\n", out["note"])
        self.assertIn("lxcbr0", out["note"])
        self.assertIn("no LXC", out["note"])

    def test_input_not_mutated(self):
        teams = self.teams()
        bridge.apply("lxcbr0", FACTS, teams)
        self.assertEqual(teams[0]["allow_from"], [])

    def test_broken_bridge_raises(self):
        with self.assertRaises(bridge.BridgeError):
            bridge.apply("lxcbr0", {}, self.teams())


class Main(unittest.TestCase):
    def run_main(self, argv):
        out, err = io.StringIO(), io.StringIO()
        with redirect_stdout(out), redirect_stderr(err):
            code = bridge.main(argv)
        return code, out.getvalue(), err.getvalue()

    def test_prints_json(self):
        code, out, _ = self.run_main(["--bridge", "lxcbr0", "--facts", json.dumps(FACTS),
                                      "--teams", json.dumps([{"team": "a", "allow_from": []}])])
        self.assertEqual(code, 0)
        self.assertEqual(json.loads(out)["teams"][0]["allow_from"], ["192.0.2.0/24"])

    def test_null_facts_means_absent(self):
        code, out, _ = self.run_main(["--bridge", "lxcbr0", "--facts", "null", "--teams", "[]"])
        self.assertEqual(code, 0)
        self.assertEqual(json.loads(out)["teams"], [])

    def test_broken_bridge_exits_nonzero_with_message(self):
        code, out, err = self.run_main(["--bridge", "lxcbr0", "--facts", "{}", "--teams", "[]"])
        self.assertEqual(code, 1)
        self.assertEqual(out, "")
        self.assertIn("lxc-net", err)


if __name__ == "__main__":
    unittest.main()
