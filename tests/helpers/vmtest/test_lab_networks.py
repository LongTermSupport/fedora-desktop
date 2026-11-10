"""Unit tests for helpers/vmtest/lab_networks.py — the lab's subnets (Plan 00246 D1).

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.vmtest.test_lab_networks

The subnets a multi-network lab scenario puts on its links are input, never defaults: a
host_vars mapping `vm_test_lab_networks`, which the lab play checks here and writes into
lab.env. A subnet that overlaps another, or any route of the controller, is refused, because
passt copies the host's own address onto each guest's first NIC and a lab subnet over the host's
LAN would look routed by that NIC.
"""

from __future__ import annotations

import io
import json
import pathlib
import sys
import unittest
from contextlib import redirect_stderr, redirect_stdout

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.vmtest import lab_networks

ROUTES = [
    {"dst": "default", "gateway": "203.0.113.1", "dev": "eth0"},
    {"dst": "203.0.113.0/24", "dev": "eth0", "prefsrc": "203.0.113.5"},
    {"type": "local", "dst": "203.0.113.5", "table": "local", "dev": "eth0"},
    {"type": "local", "dst": "127.0.0.0/8", "table": "local", "dev": "lo"},
]

GOOD = {"n1": "192.0.2.0/24", "n2": "198.51.100.0/24", "n3": "192.0.2.128/25"}


def run_main(payload: object) -> tuple[int, str, str]:
    out, err = io.StringIO(), io.StringIO()
    stdin = sys.stdin
    sys.stdin = io.StringIO(json.dumps(payload))
    try:
        with redirect_stdout(out), redirect_stderr(err):
            code = lab_networks.main([])
    finally:
        sys.stdin = stdin
    return code, out.getvalue(), err.getvalue()


class CheckTest(unittest.TestCase):
    def test_disjoint_networks_render_lab_env_lines_in_name_order(self) -> None:
        nets = {"n2": "198.51.100.0/24", "n1": "192.0.2.0/24", "n3": "203.0.113.0/24"}
        parsed = lab_networks.check(nets, [])
        self.assertEqual(
            lab_networks.env_text(parsed),
            "VMTEST_NET_N1=192.0.2.0/24\nVMTEST_NET_N2=198.51.100.0/24\nVMTEST_NET_N3=203.0.113.0/24\n",
        )

    def test_nothing_declared_renders_nothing(self) -> None:
        self.assertEqual(lab_networks.env_text(lab_networks.check({}, ROUTES)), "")

    def test_overlapping_networks_are_refused_naming_both(self) -> None:
        with self.assertRaisesRegex(lab_networks.LabNetworksError, "n1 192.0.2.0/24 overlaps n3 192.0.2.128/25"):
            lab_networks.check(GOOD, [])

    def test_a_network_on_a_route_of_the_controller_is_refused(self) -> None:
        with self.assertRaisesRegex(
            lab_networks.LabNetworksError, "n1 203.0.113.64/26 overlaps the controller's route 203.0.113.0/24"
        ):
            lab_networks.check({"n1": "203.0.113.64/26"}, ROUTES)
        with self.assertRaisesRegex(lab_networks.LabNetworksError, "127.0.0.0/8"):
            lab_networks.check({"n1": "127.1.0.0/24"}, ROUTES)

    def test_the_default_route_covers_nothing_on_its_own(self) -> None:
        self.assertEqual(len(lab_networks.check({"n1": "192.0.2.0/24"}, ROUTES)), 1)

    def test_malformed_input_is_refused(self) -> None:
        bad = [
            ({"N1": "192.0.2.0/24"}, "name"),
            ({"n-1": "192.0.2.0/24"}, "name"),
            ({"n1": "192.0.2.1/24"}, "canonical"),
            ({"n1": "192.0.2.0"}, "CIDR"),
            ({"n1": "2001:db8::/64"}, "IPv4"),
            ({"n1": "192.0.2.0/28"}, "prefix"),
            ({"n1": "192.0.0.0/7"}, "prefix"),
            ({"n1": 24}, "CIDR"),
            (["192.0.2.0/24"], "mapping"),
        ]
        for nets, want in bad:
            with self.subTest(nets=nets), self.assertRaisesRegex(lab_networks.LabNetworksError, want):
                lab_networks.check(nets, [])

    def test_a_route_it_cannot_read_is_refused_not_skipped(self) -> None:
        for routes in ([{"dev": "eth0"}], [{"dst": "not-an-address"}], {"dst": "default"}):
            with self.subTest(routes=routes), self.assertRaises(lab_networks.LabNetworksError):
                lab_networks.check({"n1": "192.0.2.0/24"}, routes)


class MainTest(unittest.TestCase):
    def test_main_prints_the_lines_and_exits_zero(self) -> None:
        code, out, err = run_main({"networks": {"n1": "192.0.2.0/24"}, "routes": ROUTES})
        self.assertEqual((code, out, err), (0, "VMTEST_NET_N1=192.0.2.0/24\n", ""))

    def test_main_refuses_with_the_variable_named_and_prints_nothing(self) -> None:
        code, out, err = run_main({"networks": GOOD, "routes": []})
        self.assertEqual((code, out), (1, ""))
        self.assertIn("vm_test_lab_networks", err)

    def test_main_refuses_input_that_is_not_the_two_keys(self) -> None:
        for payload in ({"networks": {}}, {"networks": {}, "routes": [], "x": 1}, []):
            with self.subTest(payload=payload):
                code, out, _ = run_main(payload)
                self.assertEqual((code, out), (1, ""))


if __name__ == "__main__":
    unittest.main()
