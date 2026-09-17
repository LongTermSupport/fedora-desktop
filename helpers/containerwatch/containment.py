"""Automatic crash-loop containment — the decision (Plan 00132 Phase 6).

THE LINE THIS MODULE CROSSES, AND WHY
-------------------------------------
Everything else under `helpers/containerwatch/` is reporting-only, and the
`qa-nokill-containerwatch.bash` gate enforces that. This module is the single
audited exception, and the gate names it explicitly rather than being relaxed:
one file may stop a container, every other file still may not.

The exception exists because detection alone leaves the original defect intact.
A restart storm exhausts the per-UID D-Bus accounting and takes the session's
compositor with it; on a desktop that costs the logged-in session, and on a
SERVER nobody is watching a panel at all, so a report nobody reads is the whole
defence. Enforcement is the part that works unattended.

WHY A WINDOW AND NOT A TOTAL
----------------------------
``RestartCount`` is cumulative and never resets, so a total would eventually
condemn any long-lived container that had a bad afternoon months ago. The
question worth acting on is what is happening NOW, which is a rate.

The threshold is measured, not chosen by taste: roughly 85,000 restarts
exhausted the quota, and the busiest legitimate container on the same host
managed 19 in its entire lifetime. 100 inside ten minutes sits about four orders
of magnitude away from normal and roughly 0.1% of the way to the failure, so it
trips hours before harm and never on ordinary churn.

STOP, AND ONLY STOP
-------------------
``kill`` denies the workload its shutdown path. ``rm`` destroys it. ``pause`` is
actively wrong here -- a frozen container still holds the transient units whose
accumulation IS the damage. A graceful ``stop`` releases them and is definitive
against every restart policy, because an explicit stop is what `unless-stopped`
and `always` both honour.

This module is PURE: it decides and builds an argv. It runs nothing.
"""

from __future__ import annotations

from dataclasses import dataclass

# Engines whose lifecycle we understand well enough to act on. An engine absent
# from this tuple is reported, never acted on -- running an invented command from
# an unattended timer is a worse failure than leaving a container alone.
CONTAINABLE_ENGINES = ("podman", "docker")

DEFAULT_THRESHOLD = 100
DEFAULT_WINDOW_S = 600

# Seconds the workload gets to shut down before the engine escalates. Long enough
# for a real service to flush, short enough that containment still happens
# promptly on a host already in trouble.
STOP_TIMEOUT_S = 10


@dataclass(frozen=True)
class Decision:
    """Whether to contain, and the reason either way.

    The reason is recorded even when the answer is no: "why was this NOT stopped"
    is the question asked after an incident, and it has to be answerable.
    """

    contain: bool
    reason: str


def _sample_at(sample: object) -> int | None:
    """The timestamp of a sample, or None if it cannot be read as one."""
    if not isinstance(sample, dict):
        return None
    at = sample.get("at")
    if isinstance(at, bool) or not isinstance(at, int):
        return None
    return at


def _sample_count(sample: object) -> int | None:
    if not isinstance(sample, dict):
        return None
    count = sample.get("count")
    if isinstance(count, bool) or not isinstance(count, int):
        return None
    return count


def trim_history(history: list, *, now: int, window_s: int) -> list:
    """Drop samples that fall outside the window, and any that cannot be read.

    A malformed sample is discarded rather than repaired: a guessed timestamp
    would place restarts in a window they may not belong to, and this window
    decides whether to stop someone's container.
    """
    cutoff = now - window_s
    kept = []
    for sample in history:
        at = _sample_at(sample)
        if at is None or _sample_count(sample) is None:
            continue
        if at >= cutoff:
            kept.append(sample)
    return kept


def record_sample(
    history: list, *, at: int, count: int, window_s: int = DEFAULT_WINDOW_S
) -> list:
    """Append this tick's reading and trim in the same step.

    Trimming here rather than at read time keeps the stored history bounded, so
    the report cannot grow without limit on a host left running for months.
    """
    trimmed = trim_history(history, now=at, window_s=window_s)
    return [*trimmed, {"at": at, "count": count}]


def restarts_in_window(history: list, *, now: int, window_s: int) -> int | None:
    """Restarts observed across the window, or None when that is not measurable.

    None rather than 0 when there is a single sample: one reading is a level, not
    a rate, and treating it as zero movement would be a claim the data does not
    support -- in the direction of stopping nothing, which is the safe direction.
    """
    window = trim_history(history, now=now, window_s=window_s)
    if len(window) < 2:
        return None

    counts = [_sample_count(s) for s in window]
    first, last = counts[0], counts[-1]
    if first is None or last is None:
        return None

    delta = last - first
    if delta < 0:
        # RestartCount resets to 0 when a container is recreated in the same id
        # slot, so a backwards count is a different container, not a storm.
        return None
    return delta


def should_contain(
    *, history: list, now: int, window_s: int, threshold: int
) -> bool:
    """Has this container restarted enough, recently enough, to act on?"""
    delta = restarts_in_window(history, now=now, window_s=window_s)
    return delta is not None and delta >= threshold


def _allowlisted(name: str, allowlist: list) -> bool:
    """Deliberately name-only, matching `crashloop.matches_allowlist`.

    An entry carrying a `cmd_pattern` is a statement about PROCESSES, and must
    not silently also exempt a container from containment.
    """
    for entry in allowlist:
        if not isinstance(entry, dict) or entry.get("cmd_pattern") is not None:
            continue
        if entry.get("container_name") == name:
            return True
    return False


def decide(
    candidate: dict,
    *,
    now: int,
    allowlist: list,
    enabled: bool,
    window_s: int = DEFAULT_WINDOW_S,
    threshold: int = DEFAULT_THRESHOLD,
) -> Decision:
    """Whether to stop this container, with the reason recorded either way.

    The guards are ordered cheapest-and-most-absolute first, so the recorded
    reason names the most fundamental objection rather than an incidental one.
    """
    name = candidate.get("container_name", "")

    if not enabled:
        return Decision(False, "containment disabled by configuration")

    if _allowlisted(name, allowlist):
        return Decision(False, f"{name} is on the allowlist")

    engine = candidate.get("engine", "")
    if engine not in CONTAINABLE_ENGINES:
        return Decision(False, f"engine '{engine}' cannot be contained safely")

    # THE STRUCTURAL GUARD, and the one that makes the rest of this safe to run
    # enabled by default.
    #
    # Only a container configured to restart FOR EVER can be in a policy-driven
    # loop, and only that loop is this module's business. A container with no
    # restart policy cannot be restarted by the engine at all, so whatever is
    # cycling it is something else — a systemd unit, a compose supervisor, a
    # person — and stopping it would neither address the cause nor stay stopped.
    # A capped `on-failure:N` already gives up by itself, which is the requested
    # behaviour rather than a fault.
    #
    # This is stronger than any threshold: it takes whole classes of container out
    # of reach by construction. Every CCY session runs with no restart policy, so
    # none of them is reachable here regardless of how it behaves.
    policy = candidate.get("restart_policy", "")
    if policy != "uncapped":
        return Decision(
            False,
            f"{name} restart policy is '{policy or 'unknown'}', not unbounded — "
            "containment only acts on containers set to restart for ever",
        )

    if not candidate.get("running", False):
        return Decision(False, f"{name} is not running")

    history = candidate.get("history") or []
    delta = restarts_in_window(history, now=now, window_s=window_s)
    if delta is None:
        return Decision(False, f"{name} has no measurable restart rate yet")
    if delta < threshold:
        return Decision(False, f"{name} restarted {delta} time(s) in the window")

    return Decision(True, f"{name} restarted {delta} time(s) within {window_s}s")


def build_stop_argv(*, engine: str, container_id: str) -> list[str]:
    """The exact command to stop a container. Never kill, remove, or pause.

    Raises on an engine this module does not support, rather than improvising:
    an unattended timer must not run a command nobody wrote.
    """
    if engine not in CONTAINABLE_ENGINES:
        raise ValueError(f"refusing to build a stop command for engine '{engine}'")
    return [engine, "stop", "--time", str(STOP_TIMEOUT_S), container_id]
