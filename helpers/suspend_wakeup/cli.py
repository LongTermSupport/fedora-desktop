#!/usr/bin/env python3
"""Executor: assert the suspend wakeup policy applied, and say what it checked.

Thin side-effecting wrapper around the pure logic in core.py. Enumerates
/sys/class/power_supply/*, reads each device's power/wakeup attribute, and hands the
mapping to core.evaluate().

Invoked from the playbook as a module from the repo root:

    python3 -m helpers.suspend_wakeup.cli

Exit 0 = every power-delivery device the udev policy targets reads `disabled`, OR the
host has none. Exit 1 = at least one target is still `enabled`, or one could not be
vouched for: its attribute was unreadable, held a value that is neither `enabled` nor
`disabled`, or its device entry did not resolve. A host with no such hardware is NOT a
failure — that regression is the whole reason this is a helper rather than a grep (see
core.py).

The COVERAGE line is the payload and goes to stdout, so the run always records the size
of the set that was checked rather than only speaking up on failure — a gate whose only
visible output is a failure is indistinguishable from a gate that never ran.
"""

from __future__ import annotations

import argparse
import pathlib

from helpers.suspend_wakeup import core

DEFAULT_POWER_SUPPLY_DIR = "/sys/class/power_supply"


def read_wakeup_states(power_supply_dir: str = DEFAULT_POWER_SUPPLY_DIR) -> dict[str, str | None]:
    """Map power_supply devices to their power/wakeup value.

    Three outcomes, and keeping them apart is the whole point:

    - **Attribute present and readable** -> its text. The normal case.
    - **Attribute ABSENT** -> the device is omitted from the mapping entirely. It is not
      wakeup-capable, so there is nothing to disarm and nothing wrong. `BAT0` on the
      reference host is exactly this.
    - **Device cannot be vouched for** -> `None`, which `core.evaluate()` counts against
      the verdict, because we cannot show the policy applied. Two causes reach this: the
      attribute exists but will not read (permissions, an IsADirectory, an I/O error),
      or the device entry itself does not resolve — see the ordering note below.

    Catching bare `OSError` collapses the last two, so a device that merely lacks the
    attribute hard-fails the entire provisioning run. That is the same defect as the
    `grep -l` this module replaced — treating "absent" as "broken" — one level down.

    The DEVICE is resolved before its attribute, because the two absences are not the
    same: a device whose entry does not resolve at all (a dangling symlink, hardware
    that vanished mid-enumeration) would otherwise raise the same `FileNotFoundError`
    and be dropped from the population silently, shrinking the denominator of the very
    line that exists to state it.

    That ordering has a deliberate consequence. A `ucsi-source-psy-*` unplugged in the
    window between `iterdir()` and its `stat()` is now unverifiable — a failed run rather
    than a silently smaller population. That is the trade this module exists to make: a
    loud, re-runnable failure beats a quiet under-count, and the check is the last task
    in the play, so the abort lands after all three layers are already installed.
    """
    base = pathlib.Path(power_supply_dir)
    if not base.is_dir():
        return {}

    states: dict[str, str | None] = {}
    for device in sorted(base.iterdir()):
        if not device.exists():
            # False for a dangling symlink AND for an entry we cannot stat at all
            # (EACCES, ELOOP). Either way the device is real enough to be enumerated
            # but cannot be vouched for, so it stays in the population as unverifiable.
            states[device.name] = None
            continue
        try:
            states[device.name] = (device / "power" / "wakeup").read_text()
        except FileNotFoundError:
            continue  # not wakeup-capable — nothing to disarm, not a fault
        except OSError:
            states[device.name] = None  # exists but unreadable — a real problem
    return states


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--power-supply-dir",
        default=DEFAULT_POWER_SUPPLY_DIR,
        help=f"sysfs power_supply class directory (default: {DEFAULT_POWER_SUPPLY_DIR}).",
    )
    args = parser.parse_args(argv)

    result = core.evaluate(read_wakeup_states(args.power_supply_dir))
    print(result.summary())
    return 0 if result.ok else 1


if __name__ == "__main__":
    raise SystemExit(main())
