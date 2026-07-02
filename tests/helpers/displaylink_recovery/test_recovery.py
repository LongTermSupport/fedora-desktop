"""Unit tests for the DisplayLink dock hotplug recovery decision logic.

Repo root must be on sys.path for the `helpers` namespace package to import
(matches the convention in tests/helpers/pyenv/test_resolver.py).
"""

from __future__ import annotations

import os
import sys
import unittest

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", "..")))

from helpers.displaylink_recovery.recovery import (
    Action,
    HeadState,
    SystemState,
    decide,
    wedged_heads,
)


def _state(**overrides) -> SystemState:
    defaults = dict(
        heads=[],
        service_active=True,
        mutter_corruption_detected=False,
        attempted_service_restart=False,
        attempted_usb_reauth=False,
        attempted_module_reload=False,
        drm_client_active=True,
    )
    defaults.update(overrides)
    return SystemState(**defaults)


class TestWedgedHeads(unittest.TestCase):
    def test_no_heads_is_empty(self):
        self.assertEqual(wedged_heads(_state(heads=[])), [])

    def test_connected_with_edid_is_not_wedged(self):
        heads = [HeadState(name="card2-DVI-I-1", status="connected", edid_bytes=256)]
        self.assertEqual(wedged_heads(_state(heads=heads)), [])

    def test_disconnected_with_zero_edid_is_not_wedged(self):
        heads = [HeadState(name="card4-DVI-I-3", status="disconnected", edid_bytes=0)]
        self.assertEqual(wedged_heads(_state(heads=heads)), [])

    def test_connected_with_zero_edid_is_wedged(self):
        heads = [HeadState(name="card3-DVI-I-2", status="connected", edid_bytes=0)]
        result = wedged_heads(_state(heads=heads))
        self.assertEqual([h.name for h in result], ["card3-DVI-I-2"])


class TestDecide(unittest.TestCase):
    def test_no_wedge_no_corruption_is_none(self):
        heads = [HeadState(name="card2-DVI-I-1", status="connected", edid_bytes=256)]
        state = _state(heads=heads)
        self.assertEqual(decide(state), Action.NONE)

    def test_wedge_first_tries_service_restart(self):
        heads = [HeadState(name="card3-DVI-I-2", status="connected", edid_bytes=0)]
        state = _state(heads=heads)
        self.assertEqual(decide(state), Action.RESTART_SERVICE)

    def test_wedge_after_restart_tries_usb_reauth(self):
        heads = [HeadState(name="card3-DVI-I-2", status="connected", edid_bytes=0)]
        state = _state(heads=heads, attempted_service_restart=True)
        self.assertEqual(decide(state), Action.USB_REAUTH)

    def test_wedge_after_reauth_tries_module_reload_when_no_drm_client(self):
        heads = [HeadState(name="card3-DVI-I-2", status="connected", edid_bytes=0)]
        state = _state(
            heads=heads,
            attempted_service_restart=True,
            attempted_usb_reauth=True,
            drm_client_active=False,
        )
        self.assertEqual(decide(state), Action.MODULE_RELOAD)

    def test_wedge_after_reauth_skips_module_reload_when_drm_client_active(self):
        heads = [HeadState(name="card3-DVI-I-2", status="connected", edid_bytes=0)]
        state = _state(
            heads=heads,
            attempted_service_restart=True,
            attempted_usb_reauth=True,
            drm_client_active=True,
        )
        self.assertEqual(decide(state), Action.NOTIFY_MUTTER_CORRUPTION)

    def test_wedge_after_all_safe_options_exhausted_notifies(self):
        heads = [HeadState(name="card3-DVI-I-2", status="connected", edid_bytes=0)]
        state = _state(
            heads=heads,
            attempted_service_restart=True,
            attempted_usb_reauth=True,
            attempted_module_reload=True,
            drm_client_active=False,
        )
        self.assertEqual(decide(state), Action.NOTIFY_MUTTER_CORRUPTION)

    def test_mutter_corruption_notifies(self):
        state = _state(mutter_corruption_detected=True)
        self.assertEqual(decide(state), Action.NOTIFY_MUTTER_CORRUPTION)

    def test_mutter_corruption_takes_priority_over_evdi_wedge(self):
        heads = [HeadState(name="card3-DVI-I-2", status="connected", edid_bytes=0)]
        state = _state(heads=heads, mutter_corruption_detected=True)
        self.assertEqual(decide(state), Action.NOTIFY_MUTTER_CORRUPTION)

    def test_healthy_system_with_prior_attempts_recorded_is_still_none(self):
        heads = [HeadState(name="card2-DVI-I-1", status="connected", edid_bytes=256)]
        state = _state(heads=heads, attempted_service_restart=True, attempted_usb_reauth=True)
        self.assertEqual(decide(state), Action.NONE)


if __name__ == "__main__":
    unittest.main()
