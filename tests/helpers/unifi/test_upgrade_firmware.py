"""Tests for helpers.unifi.upgrade_firmware: one-at-a-time device firmware upgrades."""

import io
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[3]))

from helpers.unifi.upgrade_firmware import main, plan_upgrades, upgrade_done


def device(name, mac, kind="uap", version="1.0", target="2.0", upgradable=True, state=1):
    return {
        "name": name,
        "mac": mac,
        "type": kind,
        "version": version,
        "upgradable": upgradable,
        "upgrade_to_firmware": target if upgradable else None,
        "state": state,
    }


class FakeClient:
    """Serves a scripted sequence of stat/device answers; records every POST."""

    def __init__(self, snapshots):
        self.snapshots = list(snapshots)
        self.logged_in = False
        self.posts = []

    def login(self):
        self.logged_in = True

    def get(self, path):
        assert self.logged_in, "GET before login"
        assert path == "stat/device", f"unexpected GET {path}"
        if len(self.snapshots) > 1:
            return self.snapshots.pop(0)
        return self.snapshots[0]

    def post(self, path, body):
        assert self.logged_in, "POST before login"
        self.posts.append((path, body))


class FakeClock:
    def __init__(self):
        self.now = 0.0

    def time(self):
        return self.now

    def sleep(self, seconds):
        self.now += seconds


class PlanUpgradesTest(unittest.TestCase):
    def test_access_points_come_before_switches_and_each_kind_is_ordered_by_name(self):
        devices = [
            device("Switch", "s1", kind="usw"),
            device("Upstairs", "a2"),
            device("Kitchen", "a1"),
        ]
        names = [u.name for u in plan_upgrades(devices)]
        self.assertEqual(names, ["Kitchen", "Upstairs", "Switch"])

    def test_an_explicit_order_comes_first_and_the_rest_follow_by_kind_and_name(self):
        devices = [
            device("Switch", "s1", kind="usw"),
            device("Upstairs", "a3"),
            device("Living room", "a1"),
            device("Kitchen", "a2"),
        ]
        names = [u.name for u in plan_upgrades(devices, order=["Living room", "Kitchen"])]
        self.assertEqual(names, ["Living room", "Kitchen", "Upstairs", "Switch"])

    def test_an_order_naming_an_unknown_device_is_refused(self):
        with self.assertRaisesRegex(ValueError, "order names 'Garage'"):
            plan_upgrades([device("AP", "a1")], order=["Garage"])

    def test_current_devices_are_left_out(self):
        devices = [device("AP", "a1"), device("Flex", "s1", kind="usw", upgradable=False)]
        self.assertEqual([u.name for u in plan_upgrades(devices)], ["AP"])

    def test_a_disconnected_device_is_refused_rather_than_skipped(self):
        with self.assertRaisesRegex(ValueError, "AP is not connected"):
            plan_upgrades([device("AP", "a1", state=0)])

    def test_kinds_limits_the_plan_to_those_device_types(self):
        devices = [device("Switch", "s1", kind="usw"), device("AP", "a1")]
        self.assertEqual([u.name for u in plan_upgrades(devices, kinds=("uap",))], ["AP"])

    def test_a_disconnected_device_of_another_kind_does_not_block(self):
        devices = [device("Switch", "s1", kind="usw", state=0), device("AP", "a1")]
        self.assertEqual([u.name for u in plan_upgrades(devices, kinds=("uap",))], ["AP"])

    def test_the_plan_carries_from_and_to_versions(self):
        upgrade = plan_upgrades([device("AP", "a1", version="8.4.6", target="8.7.11")])[0]
        self.assertEqual((upgrade.mac, upgrade.current, upgrade.target), ("a1", "8.4.6", "8.7.11"))


class UpgradeDoneTest(unittest.TestCase):
    def test_done_needs_connected_on_the_target_with_nothing_newer(self):
        self.assertTrue(upgrade_done(device("AP", "a1", version="2.0", upgradable=False), "2.0"))

    def test_not_done_while_rebooting(self):
        self.assertFalse(upgrade_done(device("AP", "a1", version="2.0", state=5), "2.0"))

    def test_not_done_on_the_old_version(self):
        self.assertFalse(upgrade_done(device("AP", "a1", version="1.0"), "2.0"))


class MainTest(unittest.TestCase):
    def run_main(self, args, snapshots, env=None, timeout="900"):
        client = FakeClient(snapshots)
        clock = FakeClock()
        out, err = io.StringIO(), io.StringIO()
        environ = {"UNIFI_ADMIN_PASSWORD": "secret"} if env is None else env
        code = main(
            [*args, "--timeout", timeout, "--interval", "10"],
            stdout=out,
            stderr=err,
            environ=environ,
            client_factory=lambda url, user, password: client,
            clock=clock,
        )
        return code, out.getvalue(), err.getvalue(), client

    def test_check_lists_upgrades_and_sends_nothing(self):
        code, out, _, client = self.run_main(["--check"], [[device("AP", "a1")]])
        self.assertEqual(code, 0)
        self.assertIn("FIRMWARE-UPGRADE AP 1.0 -> 2.0", out)
        self.assertIn("FIRMWARE-PENDING 1", out)
        self.assertEqual(client.posts, [])

    def test_nothing_to_do_reports_current(self):
        code, out, _, client = self.run_main(["--apply"], [[device("AP", "a1", upgradable=False)]])
        self.assertEqual(code, 0)
        self.assertIn("FIRMWARE-CURRENT", out)
        self.assertEqual(client.posts, [])

    def test_apply_upgrades_one_device_and_waits_until_it_is_back_before_the_next(self):
        before = [device("AP1", "a1"), device("AP2", "a2")]
        rebooting = [device("AP1", "a1", state=5), device("AP2", "a2")]
        first_done = [device("AP1", "a1", version="2.0", upgradable=False), device("AP2", "a2")]
        all_done = [
            device("AP1", "a1", version="2.0", upgradable=False),
            device("AP2", "a2", version="2.0", upgradable=False),
        ]
        snapshots = [before, rebooting, first_done, first_done, all_done]
        code, out, _, client = self.run_main(["--apply"], snapshots)
        self.assertEqual(code, 0)
        self.assertEqual(
            client.posts,
            [
                ("cmd/devmgr", {"cmd": "upgrade", "mac": "a1"}),
                ("cmd/devmgr", {"cmd": "upgrade", "mac": "a2"}),
            ],
        )
        self.assertLess(out.index("FIRMWARE-UPGRADED AP1 2.0"), out.index("FIRMWARE-STARTED AP2"))
        self.assertIn("FIRMWARE-CHANGED", out)

    def test_the_order_option_sets_the_sequence(self):
        before = [device("AP1", "a1"), device("AP2", "a2")]
        done = [
            device("AP1", "a1", version="2.0", upgradable=False),
            device("AP2", "a2", version="2.0", upgradable=False),
        ]
        code, out, _, client = self.run_main(["--check", "--order", "AP2,AP1"], [before, done])
        self.assertEqual(code, 0)
        self.assertLess(out.index("FIRMWARE-UPGRADE AP2"), out.index("FIRMWARE-UPGRADE AP1"))

    def test_the_kinds_option_leaves_the_switch_alone(self):
        before = [device("Switch", "s1", kind="usw"), device("AP", "a1")]
        code, out, _, _ = self.run_main(["--check", "--kinds", "uap"], [before])
        self.assertEqual(code, 0)
        self.assertIn("FIRMWARE-UPGRADE AP", out)
        self.assertNotIn("Switch", out)
        self.assertIn("FIRMWARE-PENDING 1", out)

    def test_a_device_that_does_not_come_back_stops_the_run(self):
        before = [device("AP1", "a1"), device("AP2", "a2")]
        stuck = [device("AP1", "a1", state=5), device("AP2", "a2")]
        code, out, err, client = self.run_main(["--apply"], [before, stuck], timeout="60")
        self.assertEqual(code, 1)
        self.assertIn("FIRMWARE-TIMEOUT AP1", out)
        self.assertIn("AP1", err)
        self.assertEqual(client.posts, [("cmd/devmgr", {"cmd": "upgrade", "mac": "a1"})])
        self.assertNotIn("FIRMWARE-CHANGED", out)

    def test_a_disconnected_device_is_fatal_before_anything_is_sent(self):
        code, _, err, client = self.run_main(["--apply"], [[device("AP", "a1", state=0)]])
        self.assertEqual(code, 1)
        self.assertIn("AP is not connected", err)
        self.assertEqual(client.posts, [])

    def test_a_missing_password_is_fatal(self):
        code, _, err, _ = self.run_main(["--check"], [[]], env={})
        self.assertEqual(code, 2)
        self.assertIn("UNIFI_ADMIN_PASSWORD", err)

    def test_exactly_one_mode_is_required(self):
        for args in ([], ["--check", "--apply"]):
            with self.subTest(args=args), self.assertRaises(SystemExit):
                self.run_main(args, [[]])


if __name__ == "__main__":
    unittest.main()
