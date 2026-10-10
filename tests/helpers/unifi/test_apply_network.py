"""Tests for helpers.unifi.apply_network: check and apply desired radio state."""

import io
import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[3]))

from helpers.unifi.apply_network import main


class FakeClient:
    def __init__(self, devices):
        self.devices = devices
        self.logged_in = False
        self.puts = []
        self.posts = []

    def login(self):
        self.logged_in = True

    def get(self, path):
        assert self.logged_in, "GET before login"
        if path != "stat/device":
            raise AssertionError(f"unexpected GET {path}")
        return self.devices

    def put(self, path, body):
        assert self.logged_in, "PUT before login"
        self.puts.append((path, body))

    def post(self, path, body):
        assert self.logged_in, "POST before login"
        self.posts.append((path, body))


def ap(name, device_id, channel):
    return {
        "_id": device_id,
        "name": name,
        "mac": f"mac-{device_id}",
        "type": "uap",
        "radio_table": [{"radio": "na", "channel": channel, "ht": 80}],
    }


DESIRED = {"devices": {"AP1": {"radios": {"na": {"channel": 36}}}}}


class ApplyNetworkTest(unittest.TestCase):
    def run_main(self, args, devices, desired=DESIRED, env=None):
        client = FakeClient(devices)
        out, err = io.StringIO(), io.StringIO()
        environ = {"UNIFI_ADMIN_PASSWORD": "secret"} if env is None else env
        code = main(
            args,
            stdin=io.StringIO(json.dumps(desired)),
            stdout=out,
            stderr=err,
            environ=environ,
            client_factory=lambda url, user, password: client,
        )
        return code, out.getvalue(), err.getvalue(), client

    def test_check_reports_diffs_and_writes_nothing(self):
        code, out, _, client = self.run_main(["--check"], [ap("AP1", "1", 40)])
        self.assertEqual(code, 0)
        self.assertIn("UNIFI-DIFF AP1 na channel: 40 -> 36", out)
        self.assertIn("UNIFI-PENDING 1", out)
        self.assertEqual(client.puts, [])

    def test_apply_puts_the_full_radio_table_of_each_changed_device(self):
        code, out, _, client = self.run_main(
            ["--apply"], [ap("AP1", "1", 40), ap("AP2", "2", 100)]
        )
        self.assertEqual(code, 0)
        self.assertEqual(
            client.puts,
            [("rest/device/1", {"radio_table": [{"radio": "na", "channel": 36, "ht": 80}]})],
        )
        self.assertIn("UNIFI-APPLIED AP1", out)
        self.assertIn("UNIFI-CHANGED", out)

    def test_no_differences_report_no_changes(self):
        code, out, _, client = self.run_main(["--apply"], [ap("AP1", "1", 36)])
        self.assertEqual(code, 0)
        self.assertIn("UNIFI-NO-CHANGES", out)
        self.assertNotIn("UNIFI-CHANGED", out)
        self.assertEqual(client.puts, [])

    def test_check_reports_a_pending_adoption_and_sends_nothing(self):
        switch = {"_id": "s", "name": "Flex", "mac": "aa:20", "type": "usw", "adopted": False}
        code, out, _, client = self.run_main(["--check"], [switch], {"adopt": ["aa:20"]})
        self.assertEqual(code, 0)
        self.assertIn("UNIFI-ADOPT Flex aa:20", out)
        self.assertIn("UNIFI-PENDING 1", out)
        self.assertEqual(client.posts, [])

    def test_apply_adopts_listed_devices_before_changing_radios(self):
        switch = {"_id": "s", "name": "Flex", "mac": "aa:20", "type": "usw", "adopted": False}
        desired = {"adopt": ["aa:20"], **DESIRED}
        code, out, _, client = self.run_main(["--apply"], [switch, ap("AP1", "1", 40)], desired)
        self.assertEqual(code, 0)
        self.assertEqual(client.posts, [("cmd/devmgr", {"cmd": "adopt", "mac": "aa:20"})])
        self.assertIn("UNIFI-ADOPTED Flex", out)
        self.assertIn("UNIFI-APPLIED AP1", out)
        self.assertLess(out.index("UNIFI-ADOPTED"), out.index("UNIFI-APPLIED"))

    def test_settled_reports_settled_when_devices_run_the_desired_state(self):
        live = dict(ap("AP1", "1", 36), state=1, radio_table_stats=[{"radio": "na", "channel": 36}])
        code, out, _, client = self.run_main(["--settled"], [live])
        self.assertEqual(code, 0)
        self.assertEqual(out, "UNIFI-SETTLED\n")
        self.assertEqual(client.puts, [])

    def test_settled_names_what_is_still_provisioning_and_exits_3(self):
        live = dict(ap("AP1", "1", 36), state=5, radio_table_stats=[{"radio": "na", "channel": 40}])
        code, out, _, client = self.run_main(["--settled"], [live])
        self.assertEqual(code, 3)
        self.assertIn("UNIFI-UNSETTLED AP1 state 5, want 1 (connected)", out)
        self.assertIn("UNIFI-UNSETTLED AP1 na running channel 40, want 36", out)
        self.assertEqual(client.puts, [])

    def test_exactly_one_mode_is_required(self):
        for args in ([], ["--check", "--apply"], ["--check", "--settled"]):
            with self.subTest(args=args), self.assertRaises(SystemExit):
                self.run_main(args, [])

    def test_a_missing_password_is_fatal(self):
        code, _, err, _ = self.run_main(["--check"], [], env={})
        self.assertEqual(code, 2)
        self.assertIn("UNIFI_ADMIN_PASSWORD", err)

    def test_a_desired_state_error_is_fatal_and_names_the_problem(self):
        desired = {"devices": {"nope": {"radios": {}}}}
        code, out, err, client = self.run_main(["--apply"], [ap("AP1", "1", 40)], desired)
        self.assertEqual(code, 1)
        self.assertIn("no device named or with MAC 'nope'", err)
        self.assertEqual(client.puts, [])
        self.assertEqual(out, "")


if __name__ == "__main__":
    unittest.main()
