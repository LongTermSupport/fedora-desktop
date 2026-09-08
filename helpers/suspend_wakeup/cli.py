#!/usr/bin/env python3
"""Executor: assert the suspend wakeup policy applied, and say what it checked.

Thin side-effecting wrapper around the pure logic in core.py. Enumerates
/sys/class/power_supply/*, reads each device's power/wakeup attribute, and hands the
mapping to core.evaluate().

Invoked from the playbook as a module from the repo root:

    python3 -m helpers.suspend_wakeup.cli

Exit 0 = every power-delivery device the udev policy targets reads `disabled`, OR the
host has none. Exit 1 = at least one is still `enabled`, or its attribute could not be
read. A host with no such hardware is NOT a failure — that regression is the whole
reason this is a helper rather than a grep (see core.py).

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
    - **Attribute present but unreadable** (permissions, an IsADirectory, an I/O error)
      -> `None`, which `core.evaluate()` counts against the verdict, because we cannot
      show the policy applied.

    An earlier revision caught bare `OSError` and mapped both of the last two to `None`,
    so a device that merely lacks the attribute hard-failed the entire provisioning run.
    That is the same defect as the `grep -l` this module replaced — treating "absent" as
    "broken" — reintroduced one level down.
    """
    base = pathlib.Path(power_supply_dir)
    if not base.is_dir():
        return {}

    states: dict[str, str | None] = {}
    for device in sorted(base.iterdir()):
        attribute = device / "power" / "wakeup"
        try:
            states[device.name] = attribute.read_text()
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
