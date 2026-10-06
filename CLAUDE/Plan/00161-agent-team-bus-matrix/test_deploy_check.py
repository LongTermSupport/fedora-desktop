"""Tests for deploy_check.py, the bus-address check behind this plan's deploy.bash (U16).

The check reads `ip -j addr` and `ip -j route show table all match <address>`; here both are
handed in as decoded JSON, or the `ip` call is replaced, so nothing touches the network.

Run from anywhere:
    python3 CLAUDE/Plan/00161-agent-team-bus-matrix/test_deploy_check.py
"""

from __future__ import annotations

import importlib.util
import io
import pathlib
import sys
import unittest
from contextlib import redirect_stderr
from unittest import mock

_MODULE_PATH = pathlib.Path(__file__).resolve().parent / "deploy_check.py"
_SPEC = importlib.util.spec_from_file_location("deploy_check", _MODULE_PATH)
if _SPEC is None or _SPEC.loader is None:
    raise ImportError(f"cannot load {_MODULE_PATH}")
dc = importlib.util.module_from_spec(_SPEC)
sys.modules["deploy_check"] = dc
_SPEC.loader.exec_module(dc)

BUS = "203.0.113.9"


def link(name: str, *addresses: str) -> dict:
    return {"ifname": name, "addr_info": [{"local": a, "prefixlen": 32} for a in addresses]}


LINKS = [link("lo", "127.0.0.1", "::1"), link("wlan0", "198.51.100.7")]
DEFAULT = {"dst": "default", "gateway": "198.51.100.1", "dev": "wlan0", "protocol": "dhcp", "flags": []}


class ValidAddressTest(unittest.TestCase):
    def test_concrete_canonical_addresses_are_accepted(self) -> None:
        for text in (BUS, "2001:db8::1"):
            with self.subTest(text=text):
                self.assertTrue(dc.valid_address(text))

    def test_everything_the_installer_refuses_is_refused(self) -> None:
        for text in ("not-an-ip", "", "2001:DB8::1", "203.0.113.09", "0.0.0.0", "::",
                     "127.0.0.1", "::1", "169.254.0.5", "fe80::1", "224.0.0.1", "ff02::1"):
            with self.subTest(text=text):
                self.assertFalse(dc.valid_address(text))


class ConflictsTest(unittest.TestCase):
    def test_a_free_address_behind_only_the_default_route_has_none(self) -> None:
        self.assertEqual(dc.conflicts(BUS, LINKS, [DEFAULT]), [])

    def test_no_route_at_all_has_none(self) -> None:
        self.assertEqual(dc.conflicts(BUS, LINKS, []), [])

    def test_a_default_route_in_another_table_is_still_the_default(self) -> None:
        other = {"dst": "default", "dev": "wg0", "table": "51820", "flags": []}
        self.assertEqual(dc.conflicts(BUS, LINKS, [DEFAULT, other]), [])

    def test_the_address_already_on_agentbus0_is_a_rerun_not_a_conflict(self) -> None:
        links = [*LINKS, link("agentbus0", BUS)]
        routes = [DEFAULT, {"type": "local", "dst": BUS, "table": "local", "dev": "agentbus0", "flags": []}]
        self.assertEqual(dc.conflicts(BUS, links, routes), [])

    def test_the_address_on_another_interface_is_named(self) -> None:
        reasons = dc.conflicts(BUS, [*LINKS, link("eth1", BUS)], [DEFAULT])
        self.assertEqual(len(reasons), 1)
        self.assertIn("eth1", reasons[0])
        self.assertIn(BUS, reasons[0])

    def test_the_address_on_agentbus0_and_elsewhere_is_still_a_conflict(self) -> None:
        reasons = dc.conflicts(BUS, [*LINKS, link("agentbus0", BUS), link("eth1", BUS)], [DEFAULT])
        self.assertEqual(len(reasons), 1)
        self.assertIn("eth1", reasons[0])

    def test_a_covering_route_other_than_the_default_is_named(self) -> None:
        route = {"dst": "203.0.113.0/24", "dev": "wg0", "protocol": "kernel", "flags": []}
        reasons = dc.conflicts(BUS, LINKS, [DEFAULT, route])
        self.assertEqual(len(reasons), 1)
        self.assertIn("203.0.113.0/24", reasons[0])
        self.assertIn("wg0", reasons[0])

    def test_a_covering_route_through_a_gateway_names_it(self) -> None:
        route = {"dst": "203.0.0.0/8", "gateway": "198.51.100.254", "dev": "wlan0", "flags": []}
        reasons = dc.conflicts(BUS, LINKS, [route])
        self.assertEqual(len(reasons), 1)
        self.assertIn("via 198.51.100.254", reasons[0])

    def test_a_covering_route_with_no_device_is_a_conflict(self) -> None:
        route = {"type": "unreachable", "dst": "203.0.113.0/24", "flags": []}
        reasons = dc.conflicts(BUS, LINKS, [route])
        self.assertEqual(len(reasons), 1)
        self.assertIn("unreachable", reasons[0])

    def test_a_route_on_agentbus0_itself_is_ours(self) -> None:
        route = {"dst": BUS, "dev": "agentbus0", "protocol": "kernel", "flags": []}
        self.assertEqual(dc.conflicts(BUS, [*LINKS, link("agentbus0", BUS)], [DEFAULT, route]), [])


def fake_ip(links: list[dict], routes: list[dict]):
    calls: list[list[str]] = []

    def ip_json(argv: list[str]) -> list[dict]:
        calls.append(argv)
        return links if argv[:2] == ["addr", "show"] else routes

    return ip_json, calls


class MainTest(unittest.TestCase):
    def run_main(self, argv: list[str], links: list[dict], routes: list[dict]) -> tuple[int, str, list]:
        ip_json, calls = fake_ip(links, routes)
        err = io.StringIO()
        with mock.patch.object(dc, "ip_json", ip_json), redirect_stderr(err):
            status = dc.main(argv)
        return status, err.getvalue(), calls

    def test_syntax_accepts_and_runs_no_ip(self) -> None:
        status, _, calls = self.run_main(["syntax", BUS], LINKS, [])
        self.assertEqual(status, 0)
        self.assertEqual(calls, [])

    def test_syntax_refuses_with_64(self) -> None:
        status, err, calls = self.run_main(["syntax", "127.0.0.1"], LINKS, [])
        self.assertEqual(status, 64)
        self.assertIn("--bus-address=", err)
        self.assertEqual(calls, [])

    def test_free_asks_ip_for_the_covering_routes_in_every_table(self) -> None:
        status, err, calls = self.run_main(["free", BUS], LINKS, [DEFAULT])
        self.assertEqual(status, 0)
        self.assertIn(["route", "show", "table", "all", "match", BUS], calls)
        self.assertIn(["addr", "show"], calls)
        self.assertIn("free", err)

    def test_free_fails_with_1_and_says_pass_bus_address(self) -> None:
        status, err, _ = self.run_main(["free", BUS], [*LINKS, link("eth1", BUS)], [DEFAULT])
        self.assertEqual(status, 1)
        self.assertIn("eth1", err)
        self.assertIn("--bus-address=", err)

    def test_free_refuses_a_malformed_address_before_asking_ip(self) -> None:
        status, _, calls = self.run_main(["free", "not-an-ip"], LINKS, [])
        self.assertEqual(status, 64)
        self.assertEqual(calls, [])


class IpJsonTest(unittest.TestCase):
    def test_runs_ip_dash_j_and_decodes(self) -> None:
        done = mock.Mock(returncode=0, stdout='[{"ifname": "lo"}]', stderr="")
        with mock.patch.object(dc.subprocess, "run", return_value=done) as run:
            self.assertEqual(dc.ip_json(["addr", "show"]), [{"ifname": "lo"}])
        self.assertEqual(run.call_args.args[0], ["ip", "-j", "addr", "show"])
        self.assertTrue(run.call_args.kwargs["check"])

    def test_empty_output_is_an_empty_list(self) -> None:
        done = mock.Mock(returncode=0, stdout="", stderr="")
        with mock.patch.object(dc.subprocess, "run", return_value=done):
            self.assertEqual(dc.ip_json(["route", "show"]), [])


if __name__ == "__main__":
    unittest.main()
