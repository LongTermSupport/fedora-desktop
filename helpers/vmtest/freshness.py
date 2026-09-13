"""The base freshness policy (Plan 00110, DESIGN.md §4.4).

Pure: takes what the probe read (or failed to read) plus what `base.json`
recorded, returns a named decision. No I/O. The executor that fetches the
signals and reads the record is elsewhere.

The policy is TOTAL over its inputs and FAILS CLOSED per signal:

    artefact_identity unreadable      -> unknown   (block; provenance unchecked)
    artefact_identity differs         -> reinstall (the media changed)
    recipe or base disk changed       -> reinstall (no network needed)
    TTL-R expired                     -> reinstall (backstop for an inert signal)
    package_revision went backwards   -> unknown   (the canonical host regressed)
    package_revision advanced         -> refresh   (even inside TTL-U)
    package_revision unchanged        -> current   (even past TTL-U)
    package_revision unreadable       -> TTL-U decides, and the verdict is DEGRADED

Bodhi release state never changes the decision. It warns.

Why the ordering matters: an earlier draft resolved a partial outage
("identity unreadable, revision readable") through the TTL backstops, which
let a base run as `current` without its media identity ever being checked.
Identity is therefore judged first and alone.
"""

from __future__ import annotations

import re
from dataclasses import dataclass

DECISION_UNKNOWN = "unknown"
DECISION_REINSTALL = "reinstall"
DECISION_REFRESH = "refresh"
DECISION_CURRENT = "current"
DECISIONS = frozenset(
    {DECISION_UNKNOWN, DECISION_REINSTALL, DECISION_REFRESH, DECISION_CURRENT}
)

DIVERGENCE_REVISION_UNREADABLE = "package-revision-unreadable"
DIVERGENCE_RELEASE_NOT_CURRENT = "release-not-current"
DIVERGENCE_RELEASE_STATE_UNREADABLE = "release-state-unreadable"

BODHI_CURRENT = "current"

_SHA256_RE = re.compile(r"^[0-9a-f]{64}$")


class FreshnessInputError(ValueError):
    """The inputs are not a coherent record; no decision can be made from them."""


@dataclass(frozen=True)
class Unreadable:
    """A signal the probe could not read: which URL, and what the transport said.

    Carried as a value rather than None so the reason a verdict is `unknown`
    can name the failing URL and error verbatim (§4.5), and so "not fetched"
    is impossible to confuse with "fetched an empty value".
    """

    url: str
    error: str


@dataclass(frozen=True)
class FreshnessInputs:
    """Everything §4.4 consults, and nothing it does not.

    Times are epoch seconds. `last_upgraded_revision` is the revision the GUEST
    saw at its last upgrade (§4.4a), never the probe's.
    """

    stored_identity: str
    live_identity: str | Unreadable
    stored_recipe_digest: str
    current_recipe_digest: str
    stored_base_sha256: str
    actual_base_sha256: str
    last_upgraded_revision: int
    probe_revision: int | Unreadable
    installed_at: int
    last_upgraded_at: int
    now: int
    ttl_upgrade_seconds: int
    ttl_rebuild_seconds: int
    bodhi_state: str | Unreadable


@dataclass(frozen=True)
class Verdict:
    decision: str
    reason: str
    degraded: bool = False
    divergences: tuple[str, ...] = ()
    warnings: tuple[str, ...] = ()


def _validate(inputs: FreshnessInputs) -> None:
    for name in ("stored_identity", "stored_recipe_digest", "current_recipe_digest",
                 "stored_base_sha256", "actual_base_sha256"):
        value = getattr(inputs, name)
        if not _SHA256_RE.match(value):
            raise FreshnessInputError(f"{name} is not a sha256 digest: {value[:80]!r}")
    if isinstance(inputs.live_identity, str) and not _SHA256_RE.match(inputs.live_identity):
        raise FreshnessInputError(
            f"live_identity is not a sha256 digest: {inputs.live_identity[:80]!r}"
        )
    if inputs.ttl_upgrade_seconds <= 0:
        raise FreshnessInputError(f"ttl_upgrade_seconds must be positive: {inputs.ttl_upgrade_seconds}")
    if inputs.ttl_rebuild_seconds <= 0:
        raise FreshnessInputError(f"ttl_rebuild_seconds must be positive: {inputs.ttl_rebuild_seconds}")
    if inputs.installed_at > inputs.now:
        raise FreshnessInputError(
            f"installed_at ({inputs.installed_at}) is after now ({inputs.now}); the record or the clock is wrong"
        )
    if inputs.last_upgraded_at > inputs.now:
        raise FreshnessInputError(
            f"last_upgraded_at ({inputs.last_upgraded_at}) is after now ({inputs.now}); the record or the clock is wrong"
        )


def _release_state_signals(inputs: FreshnessInputs) -> tuple[tuple[str, ...], tuple[str, ...]]:
    """Bodhi warns and diverges; it never decides (§4.4)."""
    state = inputs.bodhi_state
    if isinstance(state, Unreadable):
        return (
            (DIVERGENCE_RELEASE_STATE_UNREADABLE,),
            (f"release state not checked: {state.url}: {state.error}",),
        )
    if state != BODHI_CURRENT:
        return (
            (DIVERGENCE_RELEASE_NOT_CURRENT,),
            (f"the lab is testing a release whose Bodhi state is {state!r}, not "
             f"{BODHI_CURRENT!r}; nobody is on it",),
        )
    return ((), ())


def decide(inputs: FreshnessInputs) -> Verdict:
    """The §4.4 policy. Total: every input combination returns a named Verdict."""
    _validate(inputs)
    divergences, warnings = _release_state_signals(inputs)

    def verdict(decision: str, reason: str, *, degraded: bool = False,
                extra_divergences: tuple[str, ...] = ()) -> Verdict:
        return Verdict(
            decision=decision,
            reason=reason,
            degraded=degraded,
            divergences=divergences + extra_divergences,
            warnings=warnings,
        )

    # 1. Identity first and alone. Unreadable blocks regardless of every other
    #    axis, including the local reinstall triggers: a reinstall fetches media
    #    whose identity must be established, and it cannot be.
    identity = inputs.live_identity
    if isinstance(identity, Unreadable):
        return verdict(
            DECISION_UNKNOWN,
            f"upstream freshness signal unavailable ({identity.url}: {identity.error}) "
            "— cannot prove the base is current",
        )
    if identity != inputs.stored_identity:
        return verdict(
            DECISION_REINSTALL,
            f"artefact identity differs: base recorded {inputs.stored_identity[:12]}…, "
            f"upstream now {identity[:12]}…; the media changed",
        )

    # 2. Local reinstall triggers — no network involved.
    if inputs.current_recipe_digest != inputs.stored_recipe_digest:
        return verdict(DECISION_REINSTALL, "recipe digest changed; the base is built differently now")
    if inputs.actual_base_sha256 != inputs.stored_base_sha256:
        return verdict(
            DECISION_REINSTALL,
            "base.qcow2 sha256 does not match the record; the base disk was altered",
        )

    # 3. TTL-R backstop for an identity signal that is correctly inert for a
    #    whole release (§4.1).
    base_age = inputs.now - inputs.installed_at
    if base_age >= inputs.ttl_rebuild_seconds:
        return verdict(
            DECISION_REINSTALL,
            f"TTL-R expired: base installed {base_age}s ago, limit {inputs.ttl_rebuild_seconds}s",
        )

    # 4. The revision is the refresh trigger; TTL-U is a backstop used ONLY
    #    when the revision could not be read.
    revision = inputs.probe_revision
    if isinstance(revision, Unreadable):
        since_upgrade = inputs.now - inputs.last_upgraded_at
        backstop = (DIVERGENCE_REVISION_UNREADABLE,)
        if since_upgrade >= inputs.ttl_upgrade_seconds:
            return verdict(
                DECISION_REFRESH,
                f"package revision unreadable ({revision.url}: {revision.error}); "
                f"TTL-U backstop: last upgraded {since_upgrade}s ago, limit "
                f"{inputs.ttl_upgrade_seconds}s",
                degraded=True,
                extra_divergences=backstop,
            )
        return verdict(
            DECISION_CURRENT,
            f"package revision unreadable ({revision.url}: {revision.error}); "
            f"inside TTL-U: last upgraded {since_upgrade}s ago, limit "
            f"{inputs.ttl_upgrade_seconds}s — currency NOT verified",
            degraded=True,
            extra_divergences=backstop,
        )
    if revision < inputs.last_upgraded_revision:
        return verdict(
            DECISION_UNKNOWN,
            f"package revision went backwards: probe read {revision}, guest last saw "
            f"{inputs.last_upgraded_revision}; the canonical host regressed, not a mirror",
        )
    if revision > inputs.last_upgraded_revision:
        return verdict(
            DECISION_REFRESH,
            f"package revision advanced: {inputs.last_upgraded_revision} -> {revision}",
        )
    return verdict(
        DECISION_CURRENT,
        f"package revision unchanged at {revision}; checked and refresh unnecessary",
    )
