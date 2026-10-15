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

IP_JSON = json.dumps([{"ifindex": 3, "ifname": "lxcbr0", "flags": ["UP"], "addr_info": [
    {"family": "inet", "local": "192.0.2.1", "prefixlen": 24, "scope": "global", "label": "lxcbr0"}]}])


def fake_ip(code=0, out="", err=""):
    calls = []

    def run(argv):
        calls.append(list(argv))
        return code, out, err
    run.calls = calls
    return run


class BridgeProbe(unittest.TestCase):
    def test_subnet_from_ip_json_canonicalised(self):
        run = fake_ip(out=IP_JSON)
        self.assertEqual(bridge.live_subnet(run), "192.0.2.0/24")
        self.assertEqual(run.calls, [["ip", "-j", "-4", "addr", "show", "dev", "lxcbr0"]])

    def test_host_bits_are_dropped(self):
        out = json.dumps([{"addr_info": [{"family": "inet", "local": "198.51.100.77", "prefixlen": 25}]}])
        self.assertEqual(bridge.live_subnet(fake_ip(out=out)), "198.51.100.0/25")

    def test_absent_device_gives_none(self):
        run = fake_ip(code=1, err='Device "lxcbr0" does not exist.\n')
        self.assertIsNone(bridge.live_subnet(run))

    def test_other_failure_is_an_error(self):
        with self.assertRaisesRegex(bridge.BridgeError, "ip.*permission denied"):
            bridge.live_subnet(fake_ip(code=2, err="permission denied"))

    def test_unparseable_output_is_an_error(self):
        with self.assertRaisesRegex(bridge.BridgeError, "lxcbr0"):
            bridge.live_subnet(fake_ip(out="not json"))

    def test_bridge_without_inet_address_fails(self):
        for out in ("[]", json.dumps([{"addr_info": []}]), json.dumps([{"addr_info": [{"family": "inet6"}]}])):
            with self.assertRaisesRegex(bridge.BridgeError, "lxcbr0.*no IPv4.*lxc-net"):
                bridge.live_subnet(fake_ip(out=out))

    def test_missing_ip_binary_is_an_error(self):
        def run(argv):
            raise FileNotFoundError("ip")
        with self.assertRaisesRegex(bridge.BridgeError, "ip"):
            bridge.live_subnet(run)


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
        out = bridge.apply("192.0.2.0/24", self.teams())
        self.assertEqual([t["allow_from"] for t in out["teams"]],
                         [["192.0.2.0/24"], ["198.51.100.0/24", "192.0.2.0/24"], ["192.0.2.0/24"]])
        self.assertEqual(out["teams"][1]["port"], 1)
        self.assertIn("192.0.2.0/24", out["note"])

    def test_absent_bridge_says_so_in_one_line_and_adds_nothing(self):
        out = bridge.apply(None, self.teams())
        self.assertEqual(out["teams"][0]["allow_from"], [])
        self.assertNotIn("\n", out["note"])
        self.assertIn("lxcbr0", out["note"])
        self.assertIn("no LXC", out["note"])

    def test_input_not_mutated(self):
        teams = self.teams()
        bridge.apply("192.0.2.0/24", teams)
        self.assertEqual(teams[0]["allow_from"], [])


class Main(unittest.TestCase):
    def run_main(self, argv, run):
        out, err = io.StringIO(), io.StringIO()
        with redirect_stdout(out), redirect_stderr(err):
            code = bridge.main(argv, run=run)
        return code, out.getvalue(), err.getvalue()

    def test_prints_json(self):
        code, out, _ = self.run_main(["--teams", json.dumps([{"team": "a", "allow_from": []}])], fake_ip(out=IP_JSON))
        self.assertEqual(code, 0)
        self.assertEqual(json.loads(out)["teams"][0]["allow_from"], ["192.0.2.0/24"])

    def test_absent_bridge_changes_nothing(self):
        code, out, _ = self.run_main(["--teams", "[]"], fake_ip(code=1, err='Device "lxcbr0" does not exist.'))
        self.assertEqual(code, 0)
        self.assertEqual(json.loads(out)["teams"], [])

    def test_broken_bridge_exits_nonzero_with_message(self):
        code, out, err = self.run_main(["--teams", "[]"], fake_ip(out="[]"))
        self.assertEqual(code, 1)
        self.assertEqual(out, "")
        self.assertIn("lxc-net", err)


if __name__ == "__main__":
    unittest.main()
