"""Pure decision logic for DisplayLink dock hotplug recovery.

No side effects, no I/O — every function takes plain data and returns a single
`Action` to take next. run_recovery.py is the thin wrapper that gathers real
system state (sysfs, journalctl, /proc) and executes whatever this module
decides.

Background (Plan 00056 / GitHub issue #28): a USB-C DisplayLink dock can wedge
in two independent ways on unplug/replug:

1. An evdi video head goes `connected` but its EDID is never read (0 bytes) —
   recoverable, in ascending order of intrusiveness: restart the
   displaylink-driver.service, force the USB device to re-enumerate (an
   "authorized" toggle), or (only if no compositor holds the DRM device open)
   reload the evdi kernel module.
2. GNOME Shell / mutter's monitor-manager state gets corrupted (its journal
   shows assertions like `meta_monitor_manager_get_logical_monitor_from_number`
   failing), after which GNOME Settings refuses any display-layout change.
   Research for this plan confirmed there is NO reboot-free fix for this on
   Wayland — Meta.restart() explicitly refuses to run under a Wayland
   compositor, and GNOME 50 removed the X11 session backend entirely. That is
   a finding to report, not something to route around: this module only ever
   notifies the user that a logout is needed. It deliberately does NOT offer
   an automated forced-logout action — killing every app in someone's session
   based on a heuristic journal-grep is exactly the kind of hard-to-reverse
   action this project's "Executing actions with care" principle says must
   stay a human decision.
"""

from __future__ import annotations

from dataclasses import dataclass
from enum import Enum


@dataclass(frozen=True)
class HeadState:
    """One DisplayLink DRM connector, read from /sys/class/drm/<name>/."""

    name: str
    status: str  # "connected" | "disconnected"
    edid_bytes: int


@dataclass(frozen=True)
class SystemState:
    heads: list[HeadState]
    service_active: bool
    mutter_corruption_detected: bool
    attempted_service_restart: bool
    attempted_usb_reauth: bool
    attempted_module_reload: bool
    drm_client_active: bool


class Action(Enum):
    NONE = "none"
    RESTART_SERVICE = "restart_service"
    USB_REAUTH = "usb_reauth"
    MODULE_RELOAD = "module_reload"
    NOTIFY_MUTTER_CORRUPTION = "notify_mutter_corruption"


def wedged_heads(state: SystemState) -> list[HeadState]:
    """Heads that are connected but never got an EDID — the evdi wedge signature."""
    return [h for h in state.heads if h.status == "connected" and h.edid_bytes == 0]


def decide(state: SystemState) -> Action:
    """Pick the single next recovery action for the current observed state.

    Mutter state corruption takes priority: it is not something USB/evdi-level
    fixes can touch, and there is no point spending an evdi wedge attempt if
    the shell itself is already corrupted.
    """
    if state.mutter_corruption_detected:
        return Action.NOTIFY_MUTTER_CORRUPTION

    if not wedged_heads(state):
        return Action.NONE

    if not state.attempted_service_restart:
        return Action.RESTART_SERVICE
    if not state.attempted_usb_reauth:
        return Action.USB_REAUTH
    if not state.attempted_module_reload and not state.drm_client_active:
        return Action.MODULE_RELOAD

    # Every safe, automatable option has been tried (or is unsafe right now)
    # and a head is still wedged — this needs a human, not another retry.
    return Action.NOTIFY_MUTTER_CORRUPTION
