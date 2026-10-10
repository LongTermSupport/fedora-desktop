"""Compare desired access-point radio settings with a UniFi controller's devices.

Pure logic, no I/O. The desired state names each access point by its controller name or
MAC, and per radio band (`ng` 2.4 GHz, `na` 5 GHz, `6e` 6 GHz) the fields to hold. The
result is, per device that differs, the field-level diffs and the full radio table to
send back: the controller replaces a device's radio table wholesale, so fields this
desired state does not mention are carried over unchanged.
"""

import copy
from dataclasses import dataclass, field

#: Radio fields a desired state may set. Anything else is refused rather than passed
#: through, so a typo cannot silently send a field the controller ignores.
SETTABLE_RADIO_FIELDS = frozenset({
    "channel",
    "ht",
    "tx_power_mode",
    "tx_power",
    "min_rssi_enabled",
    "min_rssi",
    "vwire_enabled",
})

_TOP_LEVEL_KEYS = frozenset({"devices", "adopt"})
_DEVICE_KEYS = frozenset({"radios"})


class DesiredStateError(ValueError):
    """The desired state cannot be applied to these devices as written."""


@dataclass
class DeviceChange:
    device_id: str
    device_name: str
    diffs: list = field(default_factory=list)  # (band, field, current, desired)
    radio_table: list = field(default_factory=list)


def _find_device(devices, key):
    wanted = key.lower()
    matches = [d for d in devices if d.get("name") == key or d.get("mac", "").lower() == wanted]
    if not matches:
        raise DesiredStateError(f"no device named or with MAC {key!r}")
    if len(matches) > 1:
        raise DesiredStateError(f"{len(matches)} devices match {key!r}; name it by MAC instead")
    return matches[0]


def _check_keys(mapping, allowed, where):
    unknown = sorted(set(mapping) - allowed)
    if unknown:
        raise DesiredStateError(f"unknown key(s) {unknown} in {where}")


def plan_changes(devices, desired):
    """Return one DeviceChange per device whose radios differ from `desired`."""
    _check_keys(desired, _TOP_LEVEL_KEYS, "the desired state")
    changes = []
    for key, wanted_device in desired.get("devices", {}).items():
        _check_keys(wanted_device, _DEVICE_KEYS, f"device {key!r}")
        dev = _find_device(devices, key)
        name = dev.get("name", key)
        if dev.get("type") != "uap":
            raise DesiredStateError(f"{name} is a {dev.get('type')}, not an access point")
        table = copy.deepcopy(dev.get("radio_table", []))
        by_band = {entry.get("radio"): entry for entry in table}
        diffs = []
        for band, wanted_fields in wanted_device.get("radios", {}).items():
            if band not in by_band:
                raise DesiredStateError(f"{name} has no {band!r} radio")
            entry = by_band[band]
            for field_name, value in wanted_fields.items():
                if field_name not in SETTABLE_RADIO_FIELDS:
                    raise DesiredStateError(f"{field_name!r} is not a settable radio field")
                current = entry.get(field_name)
                if current != value:
                    diffs.append((band, field_name, current, value))
                    entry[field_name] = value
        if diffs:
            changes.append(DeviceChange(dev["_id"], name, diffs, table))
    return sorted(changes, key=lambda change: change.device_name)


def plan_adoptions(devices, desired):
    """Return (name, mac) for each device listed under `adopt` that is not yet adopted.

    Devices are listed by MAC: a device awaiting adoption often has no useful name yet.
    A listed MAC the controller cannot see is an error, not a skip.
    """
    _check_keys(desired, _TOP_LEVEL_KEYS, "the desired state")
    by_mac = {d.get("mac", "").lower(): d for d in devices}
    adoptions = []
    for mac in desired.get("adopt", []):
        dev = by_mac.get(mac.lower())
        if dev is None:
            raise DesiredStateError(f"no device with MAC {mac!r} is visible to the controller")
        if not dev.get("adopted", False):
            adoptions.append((dev.get("name", mac), dev["mac"]))
    return adoptions


CONNECTED = 1


def unsettled(devices, desired):
    """Return why the devices are not yet RUNNING `desired`; empty once they are.

    The controller's own record changes the moment a change is sent; the device takes
    it up only on provisioning. So a listed device must be adopted and connected, and
    each fixed channel must be the one its radio reports running (`radio_table_stats`).
    """
    _check_keys(desired, _TOP_LEVEL_KEYS, "the desired state")
    by_mac = {d.get("mac", "").lower(): d for d in devices}
    reasons = []
    for mac in desired.get("adopt", []):
        dev = by_mac.get(mac.lower())
        if dev is None:
            raise DesiredStateError(f"no device with MAC {mac!r} is visible to the controller")
        name = dev.get("name", mac)
        if not dev.get("adopted", False):
            reasons.append(f"{name} not adopted")
        if dev.get("state") != CONNECTED:
            reasons.append(f"{name} state {dev.get('state')}, want {CONNECTED} (connected)")
    for key, wanted_device in desired.get("devices", {}).items():
        dev = _find_device(devices, key)
        name = dev.get("name", key)
        if dev.get("state") != CONNECTED:
            reasons.append(f"{name} state {dev.get('state')}, want {CONNECTED} (connected)")
        running = {stat.get("radio"): stat for stat in dev.get("radio_table_stats", [])}
        for band, wanted_fields in wanted_device.get("radios", {}).items():
            channel = wanted_fields.get("channel", "auto")
            if channel == "auto":
                continue
            if band not in running:
                reasons.append(f"{name} {band} not running")
            elif running[band].get("channel") != channel:
                reasons.append(
                    f"{name} {band} running channel {running[band].get('channel')}, want {channel}"
                )
    return reasons
