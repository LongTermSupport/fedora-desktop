"""Tests for helpers.unifi.desired: desired radio state against the controller's devices."""

import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[3]))

from helpers.unifi.desired import DesiredStateError, plan_adoptions, plan_changes


def device(name, mac, radios, device_id="id-1", kind="uap"):
    return {"_id": device_id, "name": name, "mac": mac, "type": kind, "radio_table": radios}


def radio(band, **fields):
    entry = {"radio": band, "name": f"wifi-{band}", "channel": "auto", "ht": 20}
    entry.update(fields)
    return entry


class PlanChangesTest(unittest.TestCase):
    def test_no_desired_devices_means_no_changes(self):
        devices = [device("AP", "aa:aa", [radio("na")])]
        self.assertEqual(plan_changes(devices, {"devices": {}}), [])

    def test_matching_state_means_no_changes(self):
        devices = [device("AP", "aa:aa", [radio("na", channel=36, ht=80)])]
        desired = {"devices": {"AP": {"radios": {"na": {"channel": 36, "ht": 80}}}}}
        self.assertEqual(plan_changes(devices, desired), [])

    def test_a_differing_field_is_reported_and_the_full_radio_table_is_rewritten(self):
        devices = [device("AP", "aa:aa", [radio("ng", channel=6), radio("na", channel=40, ht=160)])]
        desired = {"devices": {"AP": {"radios": {"na": {"channel": 36, "ht": 80}}}}}
        [change] = plan_changes(devices, desired)
        self.assertEqual(change.device_id, "id-1")
        self.assertEqual(change.device_name, "AP")
        self.assertEqual(
            change.diffs,
            [("na", "channel", 40, 36), ("na", "ht", 160, 80)],
        )
        self.assertEqual(change.radio_table[0], radio("ng", channel=6))
        self.assertEqual(change.radio_table[1]["channel"], 36)
        self.assertEqual(change.radio_table[1]["ht"], 80)
        self.assertEqual(change.radio_table[1]["name"], "wifi-na")

    def test_the_input_devices_are_not_mutated(self):
        original = radio("na", channel=40)
        devices = [device("AP", "aa:aa", [original])]
        plan_changes(devices, {"devices": {"AP": {"radios": {"na": {"channel": 36}}}}})
        self.assertEqual(original["channel"], 40)

    def test_a_device_can_be_named_by_mac(self):
        devices = [device("AP", "aa:bb", [radio("6e", channel=69)])]
        [change] = plan_changes(devices, {"devices": {"AA:BB": {"radios": {"6e": {"channel": 21}}}}})
        self.assertEqual(change.diffs, [("6e", "channel", 69, 21)])

    def test_a_field_missing_from_the_device_is_a_change_from_none(self):
        devices = [device("AP", "aa:aa", [radio("na")])]
        desired = {"devices": {"AP": {"radios": {"na": {"min_rssi_enabled": True, "min_rssi": -75}}}}}
        [change] = plan_changes(devices, desired)
        self.assertEqual(
            change.diffs,
            [("na", "min_rssi_enabled", None, True), ("na", "min_rssi", None, -75)],
        )

    def test_changes_come_out_in_device_name_order(self):
        devices = [
            device("b", "bb", [radio("na", channel=40)], device_id="id-b"),
            device("a", "aa", [radio("na", channel=40)], device_id="id-a"),
        ]
        desired = {"devices": {"b": {"radios": {"na": {"channel": 36}}}, "a": {"radios": {"na": {"channel": 36}}}}}
        self.assertEqual([c.device_name for c in plan_changes(devices, desired)], ["a", "b"])

    def test_an_unknown_device_is_an_error(self):
        with self.assertRaisesRegex(DesiredStateError, "no device named or with MAC 'missing'"):
            plan_changes([device("AP", "aa", [radio("na")])], {"devices": {"missing": {"radios": {}}}})

    def test_an_ambiguous_device_name_is_an_error(self):
        devices = [device("AP", "aa", [radio("na")], "1"), device("AP", "bb", [radio("na")], "2")]
        with self.assertRaisesRegex(DesiredStateError, "2 devices match 'AP'"):
            plan_changes(devices, {"devices": {"AP": {"radios": {"na": {"channel": 36}}}}})

    def test_a_radio_the_device_lacks_is_an_error(self):
        with self.assertRaisesRegex(DesiredStateError, "AP has no '6e' radio"):
            plan_changes([device("AP", "aa", [radio("na")])], {"devices": {"AP": {"radios": {"6e": {"channel": 5}}}}})

    def test_a_field_outside_the_allowed_set_is_an_error(self):
        with self.assertRaisesRegex(DesiredStateError, "'radio' is not a settable radio field"):
            plan_changes([device("AP", "aa", [radio("na")])], {"devices": {"AP": {"radios": {"na": {"radio": "ng"}}}}})

    def test_a_desired_device_that_is_not_an_access_point_is_an_error(self):
        with self.assertRaisesRegex(DesiredStateError, "SW is a usw, not an access point"):
            plan_changes([device("SW", "aa", [], kind="usw")], {"devices": {"SW": {"radios": {}}}})

    def test_unknown_top_level_keys_are_an_error(self):
        with self.assertRaisesRegex(DesiredStateError, "unknown key"):
            plan_changes([], {"devices": {}, "wlans": {}})

    def test_devices_may_be_omitted_when_only_adopting(self):
        self.assertEqual(plan_changes([], {"adopt": ["aa:bb"]}), [])


class PlanAdoptionsTest(unittest.TestCase):
    def pending(self, name, mac):
        return {"_id": "x", "name": name, "mac": mac, "type": "usw", "adopted": False}

    def test_a_listed_device_awaiting_adoption_is_adopted(self):
        devices = [self.pending("Flex", "a8:9c:00:00:00:20")]
        self.assertEqual(
            plan_adoptions(devices, {"adopt": ["A8:9C:00:00:00:20"]}),
            [("Flex", "a8:9c:00:00:00:20")],
        )

    def test_an_already_adopted_device_needs_nothing(self):
        devices = [dict(self.pending("Flex", "aa"), adopted=True)]
        self.assertEqual(plan_adoptions(devices, {"adopt": ["aa"]}), [])

    def test_an_unlisted_device_awaiting_adoption_is_left_alone(self):
        self.assertEqual(plan_adoptions([self.pending("Other", "bb")], {"adopt": []}), [])

    def test_no_adopt_key_means_no_adoptions(self):
        self.assertEqual(plan_adoptions([self.pending("Other", "bb")], {"devices": {}}), [])

    def test_a_listed_mac_the_controller_cannot_see_is_an_error(self):
        with self.assertRaisesRegex(DesiredStateError, "no device with MAC 'cc'"):
            plan_adoptions([self.pending("Other", "bb")], {"adopt": ["cc"]})


if __name__ == "__main__":
    unittest.main()
