"""Pure logic: did the suspend wakeup policy actually apply to the devices it targets?

No I/O — `cli.py` reads sysfs and feeds a {device_name: wakeup_state} mapping in here.

WHY THIS IS NOT A `grep -l '^enabled$'` OVER THREE PATHS. `grep` exits 2 when a path does
not exist, so on any machine without an `AC` device and at least one `ucsi-source-psy-*`
the assertion returns 2 — and `failed_when: rc != 1` turns a machine that simply has no
such hardware into a fatal error aborting the whole provisioning run. The obvious repair,
treating a non-match as a pass, has the opposite defect: it prints "all power-delivery
wakeup sources disarmed" on a host with zero devices, reporting blind as clean.

Both failures come from the same omission: neither stated its POPULATION. So the result
here always carries the count it checked, and an empty population is reported in words
rather than as a bare pass.

The target set mirrors the udev rule in
`files/etc/udev/rules.d/99-suspend-wakeup-policy.rules` — keep them in step.
"""

from __future__ import annotations

from dataclasses import dataclass, field

_UCSI_SOURCE_PREFIX = "ucsi-source-psy-"

# The udev rule disarms exactly these. `ucsi-sink-psy-*` is deliberately NOT included:
# the rule names the source (charger) devices only.
_EXACT_TARGETS = frozenset({"AC"})


def is_policy_target(device_name: str) -> bool:
    """True if the udev wakeup policy names this power_supply device."""
    if device_name in _EXACT_TARGETS:
        return True
    return device_name.startswith(_UCSI_SOURCE_PREFIX)


@dataclass(frozen=True)
class Result:
    """What the assertion found, including the size of the set it looked at."""

    total: int
    disarmed: int
    still_armed: list[str] = field(default_factory=list)
    unverifiable: list[str] = field(default_factory=list)

    @property
    def ok(self) -> bool:
        """A host with no targets passes; a target we cannot vouch for does not.

        A state we could not read, or read and did not recognise, is not evidence that
        the policy applied, so it counts against the verdict rather than being quietly
        skipped.
        """
        return not self.still_armed and not self.unverifiable

    def summary(self) -> str:
        """One line naming the population — the thing both previous forms omitted."""
        if self.total == 0:
            return "COVERAGE: 0 of 0 — no power-delivery wakeup devices on this host"
        line = f"COVERAGE: {self.disarmed} of {self.total} power-delivery devices disarmed"
        if self.still_armed:
            line += f" — STILL ARMED: {', '.join(self.still_armed)}"
        if self.unverifiable:
            line += f" — UNVERIFIABLE: {', '.join(self.unverifiable)}"
        return line


def evaluate(devices: dict[str, str | None]) -> Result:
    """Judge a {device_name: wakeup_state_or_None} mapping.

    `None` means the `power/wakeup` attribute was present but unreadable — distinct from
    the device being absent entirely, which simply means it is not in the mapping.

    The state test is an ALLOW-LIST of the two words sysfs actually writes. A deny-list
    ("not `enabled`, therefore disarmed") passes an empty read, a truncated read and a
    garbage read as a clean result — this module's own stated failure mode, reporting
    blind as clean, surviving at per-device granularity. Anything unrecognised is
    unverifiable, because a value we cannot interpret is not evidence of anything.
    """
    total = 0
    disarmed = 0
    still_armed: list[str] = []
    unverifiable: list[str] = []

    for name, state in sorted(devices.items()):
        if not is_policy_target(name):
            continue
        total += 1
        if state is None:
            unverifiable.append(name)
            continue
        match state.strip():
            case "disabled":
                disarmed += 1
            case "enabled":
                still_armed.append(name)
            case _:
                unverifiable.append(name)

    return Result(
        total=total,
        disarmed=disarmed,
        still_armed=still_armed,
        unverifiable=unverifiable,
    )
