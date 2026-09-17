"""Crash-loop detection for the container watchdog (Plan 00132).

A container restarting without bound is a HOST stability problem, not merely an
untidy one. Every restart cycle registers a transient ``libpod-*.scope`` with the
user's systemd manager, which emits unit and job signals on the **session** bus
-- the same bus the desktop depends on. Sustained long enough, the per-UID D-Bus
accounting gives out, peers are disconnected, and GNOME Shell exits. On Wayland
the compositor IS the display server, so every GUI client dies with it.

That is not hypothetical: it happened, and the numbers below come from the real
incident rather than from judgement. See
``CLAUDE/Plan/00132-crashloop-killed-shell-then-greeter-suspended-on-ac/``.

TWO GATES, BECAUSE NEITHER IS SUFFICIENT ALONE
----------------------------------------------
``rate``
    Restart delta between two ticks. Catches a loop as it develops, including a
    brand-new one whose cumulative count is still tiny.

``absolute``
    Cumulative count over a high floor. Catches a loop that was ALREADY running
    when the watchdog started, which the rate gate cannot see on its first tick
    because it has no previous sample to difference against.

WHY ``absolute`` IS GATED ON ``running``
----------------------------------------
``RestartCount`` is cumulative and never resets. Without the gate, the absolute
test keeps firing for ever on a container that has already been stopped and dealt
with. That defect was found by running the true negative after stopping the real
loop -- the rate gate went correctly silent while the absolute gate went on
reporting 131,377 restarts on an exited container.

An alarm that cannot be cleared is worse than no alarm, because the operator
learns to ignore it -- and what they would learn to ignore is precisely the
signal this module exists to deliver. A container that is not running cannot be
looping.

This module is PURE: no subprocess, no podman, no I/O. The caller supplies the
sampled counts, so every branch above is unit-testable without a container.
"""

from __future__ import annotations

# Both defaults come from the measured incident, not from taste.
#
# 100 restarts inside one window is ~0.1% of the way to the failure that killed
# the desktop (roughly 85,000 restarts exhausted the quota, over about ten
# hours), while the busiest legitimate container on that host accumulated 19 in
# its entire lifetime. The gap between those two numbers is where the threshold
# lives, and it is about four orders of magnitude wide.
DEFAULT_RATE_PER_TICK = 10
DEFAULT_TICK_S = 120
DEFAULT_ABSOLUTE = 1000

_SECONDS_PER_MIN = 60


def restart_delta(*, previous: int | None, current: int) -> int | None:
    """Restarts between two samples, or None when that is not measurable.

    None and 0 are deliberately distinct. 0 means "measured, and nothing
    happened"; None means "no basis to measure". Collapsing them would let a
    first tick assert a container is healthy on no evidence.

    A NEGATIVE difference is also None: ``RestartCount`` resets to 0 when a
    container is removed and recreated, so a backwards count is a different
    container in the same id slot, not a rate.
    """
    if previous is None:
        return None
    delta = current - previous
    if delta < 0:
        return None
    return delta


def scaled_rate_threshold(*, per_tick: int, tick_s: int, elapsed_s: float) -> int:
    """Scale a per-tick bound to the time that actually elapsed.

    Rounds DOWN, then clamps to at least 1. Rounding up would make a short
    interval stricter than the production tick and manufacture findings the real
    probe would not raise; a zero threshold would flag every container on the
    host, since every delta is >= 0. Both failure directions are toward silence.
    """
    if elapsed_s <= 0 or tick_s <= 0:
        return 1
    scaled = int(per_tick * elapsed_s / tick_s)
    return max(1, scaled)


def matches_allowlist(finding: dict, allowlist: list[dict]) -> bool:
    """True if `finding` is deliberately suppressed by `allowlist`.

    Deliberately NOT ``core.matches_allowlist``. That one is written for
    process-shaped findings and matches ``cmd`` with fnmatch; a crash-loop
    finding has no ``cmd``, and ``fnmatch("", "*")`` is **True**. Reusing it
    meant an entry carrying ``cmd_pattern: "*"`` — a perfectly reasonable way to
    quieten one container's CPU findings — would ALSO have muted every
    crash-loop alarm on the host, silently, as a side effect of tuning something
    unrelated.

    So only an UNQUALIFIED container name suppresses here. An entry that carries
    a ``cmd_pattern`` is a statement about processes ("allow this command inside
    that container"), not about the container's right to restart without bound,
    and it is ignored.
    """
    name = finding.get("container_name", "")
    for entry in allowlist:
        if entry.get("cmd_pattern") is not None:
            continue
        want_name = entry.get("container_name")
        if want_name is not None and want_name == name:
            return True
    return False


def previous_sample(report: dict) -> tuple[dict[str, int], int | None]:
    """Recover the last tick's restart counts and timestamp from a report.

    The watchdog already writes ``report.json`` every tick and the CLI already
    reads it, so it IS the state store and no second one is introduced.

    A report lacking ``restart_counts`` yields an EMPTY mapping, not zeros.
    Every report written before this feature existed is in that state, and
    reading them as "every container was at zero" would difference the whole
    host against zero on the first tick after an upgrade and flag all of it.
    """
    raw = report.get("restart_counts")
    counts: dict[str, int] = {}
    if isinstance(raw, dict):
        for container_id, value in raw.items():
            # bool is an int subclass, and a JSON true here means the file is not
            # what we think it is -- drop the entry rather than count it as 1.
            if isinstance(value, int) and not isinstance(value, bool):
                counts[str(container_id)] = value

    generated_at = report.get("generated_at")
    if not isinstance(generated_at, int) or isinstance(generated_at, bool):
        generated_at = None
    return counts, generated_at


def elapsed_since(*, previous_at: int | None, now: int) -> float | None:
    """Seconds between the previous report and now, or None if not usable.

    A backwards clock yields None rather than a negative number: negative
    elapsed would scale the rate threshold down to its clamp of 1 and flag any
    container that restarted even once.
    """
    if previous_at is None:
        return None
    delta = now - previous_at
    if delta <= 0:
        return None
    return float(delta)


def make_finding(
    *,
    container_id: str,
    container_name: str,
    engine: str,
    restart_count: int,
    restart_delta: int | None,
    elapsed_s: float | None,
    reasons: list[str],
) -> dict:
    """Assemble one crash-loop finding.

    ``kind`` is load-bearing: the watchdog's other findings are process-shaped
    (``host_pid``, ``cmd``, ``cpu_pct``) and this one is container-shaped with no
    process at all, so a consumer must be able to tell them apart by a field
    rather than by guessing from which keys happen to be present.
    """
    per_min: int | None = None
    if restart_delta is not None and elapsed_s:
        per_min = round(restart_delta * _SECONDS_PER_MIN / elapsed_s)

    return {
        "kind": "crashloop",
        "container_id": container_id,
        "container_name": container_name,
        "engine": engine,
        "restart_count": restart_count,
        "restart_delta": restart_delta,
        "restarts_per_min": per_min,
        "elapsed_s": elapsed_s,
        "reasons": reasons,
    }


def evaluate(
    *,
    previous: dict[str, int],
    current: dict[str, int],
    running: dict[str, bool],
    elapsed_s: float,
    rate_per_tick: int = DEFAULT_RATE_PER_TICK,
    tick_s: int = DEFAULT_TICK_S,
    absolute_threshold: int = DEFAULT_ABSOLUTE,
    names: dict[str, str] | None = None,
    engines: dict[str, str] | None = None,
) -> list[dict]:
    """Flag crash-looping containers, worst first.

    Only containers in `current` are considered: one that vanished between ticks
    is gone, not looping.
    """
    names = names or {}
    engines = engines or {}
    threshold = scaled_rate_threshold(
        per_tick=rate_per_tick, tick_s=tick_s, elapsed_s=elapsed_s
    )

    findings: list[dict] = []
    for container_id, count in current.items():
        delta = restart_delta(previous=previous.get(container_id), current=count)
        reasons: list[str] = []

        if delta is not None and delta >= threshold:
            reasons.append("rate")

        # See the module docstring: the `running` check is the regression guard,
        # not a nicety. An absent entry is treated as not running, so an
        # unreadable state fails toward silence rather than toward a false alarm
        # nobody can clear.
        if count >= absolute_threshold and running.get(container_id, False):
            reasons.append("absolute")

        if not reasons:
            continue

        findings.append(
            make_finding(
                container_id=container_id,
                container_name=names.get(container_id, container_id),
                engine=engines.get(container_id, "podman"),
                restart_count=count,
                restart_delta=delta,
                elapsed_s=elapsed_s if delta is not None else None,
                reasons=reasons,
            )
        )

    # Worst first, so a truncated report still carries the finding that matters.
    # Rate leads because it describes what is happening NOW; the cumulative count
    # breaks ties and orders two containers looping at the same speed.
    findings.sort(
        key=lambda f: (f["restart_delta"] or 0, f["restart_count"]), reverse=True
    )
    return findings
