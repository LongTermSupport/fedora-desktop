"""Tests for helpers.unifi.inform_host: pin the address devices inform to."""

import io
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[3]))

from helpers.unifi.inform_host import main, plan


def settings(hostname="example-domain.ui.com", override=None):
    mgmt = {"_id": "m1", "key": "super_mgmt"}
    if override is not None:
        mgmt["override_inform_host"] = override
    return [{"_id": "i1", "key": "super_identity", "hostname": hostname}, mgmt]


class FakeClient:
    def __init__(self, data):
        self.data = data
        self.logged_in = False
        self.puts = []

    def login(self):
        self.logged_in = True

    def get(self, path):
        assert self.logged_in, "GET before login"
        assert path == "rest/setting", f"unexpected GET {path}"
        return self.data

    def put(self, path, body):
        assert self.logged_in, "PUT before login"
        self.puts.append((path, body))


class PlanTest(unittest.TestCase):
    def test_a_fresh_controller_needs_both_the_hostname_and_the_override(self):
        self.assertEqual(
            plan(settings(), "192.0.2.254"),
            [
                ("rest/setting/super_identity/i1", {"hostname": "192.0.2.254"}),
                ("rest/setting/super_mgmt/m1", {"override_inform_host": True}),
            ],
        )

    def test_nothing_to_do_when_already_pinned(self):
        self.assertEqual(plan(settings("192.0.2.254", True), "192.0.2.254"), [])

    def test_a_missing_setting_is_refused(self):
        with self.assertRaisesRegex(ValueError, "super_mgmt"):
            plan([{"_id": "i1", "key": "super_identity", "hostname": "x"}], "192.0.2.254")


class MainTest(unittest.TestCase):
    def run_main(self, args, data, env=None):
        client = FakeClient(data)
        out, err = io.StringIO(), io.StringIO()
        environ = {"UNIFI_ADMIN_PASSWORD": "secret"} if env is None else env
        code = main(args, stdout=out, stderr=err, environ=environ,
                    client_factory=lambda url, user, password: client)
        return code, out.getvalue(), err.getvalue(), client

    def test_check_reports_and_sends_nothing(self):
        code, out, _, client = self.run_main(["--check", "--host", "192.0.2.254"], settings())
        self.assertEqual(code, 0)
        self.assertIn("INFORM-HOST-PENDING", out)
        self.assertEqual(client.puts, [])

    def test_apply_sends_both_settings(self):
        code, out, _, client = self.run_main(["--apply", "--host", "192.0.2.254"], settings())
        self.assertEqual(code, 0)
        self.assertEqual(len(client.puts), 2)
        self.assertIn("INFORM-HOST-CHANGED", out)

    def test_already_pinned_reports_ok(self):
        code, out, _, client = self.run_main(
            ["--apply", "--host", "192.0.2.254"], settings("192.0.2.254", True))
        self.assertEqual(code, 0)
        self.assertIn("INFORM-HOST-OK", out)
        self.assertEqual(client.puts, [])

    def test_a_missing_password_is_fatal(self):
        code, _, err, _ = self.run_main(["--check", "--host", "192.0.2.254"], [], env={})
        self.assertEqual(code, 2)
        self.assertIn("UNIFI_ADMIN_PASSWORD", err)


if __name__ == "__main__":
    unittest.main()
