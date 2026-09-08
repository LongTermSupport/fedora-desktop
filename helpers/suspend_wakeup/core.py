"""Pure logic: did the suspend wakeup policy actually apply to the devices it targets?

No I/O — `cli.py` reads sysfs and feeds a {device_name: wakeup_state} mapping in here.

WHY THIS IS NOT A `grep -l '^enabled$'` OVER THREE PATHS. That is exactly what it was
(Plan 00104, `80024ce`), and it was a regression. `grep` exits 2 when a path does not
exist, so on any machine without an `AC` device and at least one `ucsi-source-psy-*` the
assertion returned 2 — and `failed_when: rc != 1` turned a machine that simply has no
such hardware into a fatal error aborting the whole provisioning run. The shape before it
had the opposite defect: it printed "all power-delivery wakeup sources disarmed" on a host
with zero devices, reporting blind as clean.

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
    unreadable: list[str] = field(default_factory=list)

    @property
    def ok(self) -> bool:
        """A host with no targets passes; an unreadable target does not.

        An attribute we could not read is not evidence that the policy applied, so it
        counts against the verdict rather than being quietly skipped.
        """
        return not self.still_armed and not self.unreadable

    def summary(self) -> str:
        """One line naming the population — the thing both previous forms omitted."""
        if self.total == 0:
            return "COVERAGE: 0 of 0 — no power-delivery wakeup devices on this host"
        line = f"COVERAGE: {self.disarmed} of {self.total} power-delivery devices disarmed"
        if self.still_armed:
            line += f" — STILL ARMED: {', '.join(self.still_armed)}"
        if self.unreadable:
            line += f" — UNREADABLE: {', '.join(self.unreadable)}"
        return line


def evaluate(devices: dict[str, str | None]) -> Result:
    """Judge a {device_name: wakeup_state_or_None} mapping.

    `None` means the `power/wakeup` attribute was absent or unreadable — distinct from
    the device being absent entirely, which simply means it is not in the mapping.
    """
    total = 0
    disarmed = 0
    still_armed: list[str] = []
    unreadable: list[str] = []

    for name, state in sorted(devices.items()):
        if not is_policy_target(name):
            continue
        total += 1
        if state is None:
            unreadable.append(name)
        elif state.strip() == "enabled":
            still_armed.append(name)
        else:
            disarmed += 1

    return Result(
        total=total,
        disarmed=disarmed,
        still_armed=still_armed,
        unreadable=unreadable,
    )
