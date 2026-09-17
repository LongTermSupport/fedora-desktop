"""Find the restart policies that make an unbounded storm possible (Plan 00132).

Containment stops a storm once it is underway. This finds the CONFIGURATION that
permits one, while nothing is wrong yet — the cheaper defence, and the only one
that prevents rather than limits.

WHAT `podman-run(1)` ACTUALLY SAYS
----------------------------------
``on-failure[:max_retries]`` is the **only** policy that accepts a cap. ``always``
and ``unless-stopped`` retry indefinitely, and for them ``MaximumRetryCount`` is
not consulted at all — so a container can carry a retry count that reads like a
limit and does nothing. That is the trap worth naming: such a configuration looks
bounded in an inspect dump and is not.

Podman also applies no backoff — no ``restart-sec``, no ``restart-delay``, nothing
in the man page at all. This is why a failing container sustains restarts at
engine speed (roughly 2.4 per second was measured) instead of decaying, and why
"it will settle down eventually" is false here.

WHY THIS MATTERS MOST ON A SERVER
---------------------------------
Every restart registers a transient unit with the user's systemd manager, and the
resulting signal traffic is accounted per UID. On a desktop, exhausting that
budget kills the compositor and the human notices immediately. On a server nobody
is watching, so the same storm runs until something else gives — which is exactly
the case where a warning delivered ahead of time is worth most.

This module is PURE: it classifies text and returns findings.
"""

from __future__ import annotations

# The only policy that takes a cap, and the remedy this module recommends.
CAPPED_POLICY = "on-failure"

# Retried for ever. `MaximumRetryCount` is not consulted for either.
UNCAPPED_POLICIES = ("always", "unless-stopped")

# Not a restart policy: the container stays down, which is bounded by definition.
NO_POLICY = ("", "no", "never")

SUGGESTED_RETRIES = 5

_FIELDS = 4


def classify(policy: str, max_retries: int) -> str:
    """One of: ``uncapped``, ``capped``, ``none``, ``unknown``.

    ``unknown`` is deliberate rather than folded into ``none``: a policy this
    module does not recognise is one it cannot vouch for, and reporting it as
    safe would be a claim made with no basis.
    """
    name = (policy or "").strip().lower()

    if name in NO_POLICY:
        return "none"
    if name in UNCAPPED_POLICIES:
        # Regardless of max_retries — see the module docstring.
        return "uncapped"
    if name == CAPPED_POLICY:
        return "capped" if max_retries > 0 else "uncapped"
    return "unknown"


def _read_retries(raw: str) -> int:
    """Retry count, or 0 when the engine did not report one.

    Docker omits the field in some versions and Go templates render an absent
    value as `<no value>`. Absent means no cap was set, which is what 0 denotes.
    """
    try:
        return int(raw.strip())
    except ValueError:
        return 0


def parse_lines(text: str) -> list[dict]:
    """Parse ``id<TAB>name<TAB>policy<TAB>max_retries`` rows.

    A malformed row is dropped rather than guessed at: a container whose policy
    cannot be read is one this module has nothing to say about, and inventing a
    policy for it would produce either a false alarm or false comfort.
    """
    rows: list[dict] = []
    for line in text.splitlines():
        parts = line.split("\t")
        if len(parts) != _FIELDS:
            continue
        container_id, name, policy, raw_retries = parts
        rows.append(
            {
                "container_id": container_id.strip(),
                "container_name": name.strip().lstrip("/"),
                "policy": policy.strip(),
                "max_retries": _read_retries(raw_retries),
            }
        )
    return rows


def _advise(engine: str, name: str, classification: str) -> str:
    """The remedy, naming the container so it can be acted on directly."""
    if classification == "unknown":
        return (
            f"{name} carries a restart policy this check does not recognise — "
            f"confirm it by hand with `{engine} inspect {name}`"
        )
    return (
        f"{name} restarts without limit. Recreate it with "
        f"`--restart={CAPPED_POLICY}:{SUGGESTED_RETRIES}` so a failing container "
        "gives up instead of restarting until the host does."
    )


def audit(rows: list[dict], *, engine: str) -> list[dict]:
    """Findings for every container whose restart policy is unbounded or unclear.

    A capped policy and no policy at all both produce nothing: this is a report
    about risk, and a container that cannot storm carries none.
    """
    findings: list[dict] = []
    for row in rows:
        classification = classify(row.get("policy", ""), row.get("max_retries", 0))
        if classification in ("capped", "none"):
            continue

        name = row.get("container_name", row.get("container_id", "?"))
        findings.append(
            {
                # Its own kind, like `crashloop`: a consumer must be able to tell
                # the finding shapes apart by a field rather than by guessing from
                # which keys happen to be present.
                "kind": "restart-policy",
                "container_id": row.get("container_id", ""),
                "container_name": name,
                "engine": engine,
                "policy": row.get("policy", ""),
                "max_retries": row.get("max_retries", 0),
                "classification": classification,
                "advice": _advise(engine, name, classification),
            }
        )
    return findings
