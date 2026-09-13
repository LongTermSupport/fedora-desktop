"""The scenario manifest and the check accounting (Plan 00110, DESIGN.md §3.5, §6.2, §6.6).

Pure. `parse_manifest` takes an already-decoded mapping — the JSON Ansible
renders from `vars/vm-test-scenarios.yml` onto the host — and returns a
validated `Manifest`. No YAML here: helpers are stdlib-only, and the tracked
YAML is checked by `scripts/qa-vmtest-manifest.bash`, which converts it with the
same toolchain Ansible uses.

Three properties the data carries so prose does not have to:

- every scenario names the base it requires, and takes its profile FROM that
  base, so a run can never substitute a cheaper base and wear the expensive
  one's name (§3.5);
- a scenario id must satisfy the bridge's argument grammar and must not be a
  word on the hardcoded deny list, so nothing in the manifest is unreachable or
  exec-shaped (§6.2);
- a scenario with no declared `planned` check count is not runnable. The
  response contract's planned-vs-total rule cannot catch a harness that died
  early if `planned` was a guess (§6.6 rule 02).
"""

from __future__ import annotations

import json
import re
from collections.abc import Mapping
from dataclasses import dataclass

SECONDS_PER_DAY = 86400

KIND_FAST = "fast"
KIND_FULL = "full"
KINDS = frozenset({KIND_FAST, KIND_FULL})

PROFILE_SERVER = "server"
PROFILE_DESKTOP = "desktop"
PROFILES = frozenset({PROFILE_SERVER, PROFILE_DESKTOP})

# §6.2: the grammar alone is not the control; membership in the enumeration is.
ARGUMENT_RE = re.compile(r"^[a-z][a-z0-9_-]*$")

# §6.2 / §6.4 step 4: checked BEFORE the allowlist, fails closed. A scenario
# id that appears here could never be requested, so it is a manifest error.
DENY_LIST = frozenset(
    {"exec", "shell", "sh", "bash", "run", "eval", "system", "ansible", "ansible-playbook"}
)

VERDICT_PASS = "pass"
VERDICT_FAIL = "fail"
VERDICT_ERROR = "error"
VERDICTS = frozenset({VERDICT_PASS, VERDICT_FAIL, VERDICT_ERROR})

_TOP_LEVEL_KEYS = frozenset(
    {"vm_test_ttl_upgrade_days", "vm_test_ttl_rebuild_days", "vm_test_bases", "vm_test_scenarios"}
)
_BASE_KEYS = frozenset({"kind", "profile", "vcpus", "ram_mib"})
_SCENARIO_KEYS = frozenset({"base", "description", "planned", "max_skipped"})


class ManifestError(ValueError):
    """The manifest is not a coherent description of the lab; nothing is deployed from it."""


@dataclass(frozen=True)
class Base:
    key: str
    name: str
    kind: str
    profile: str
    vcpus: int
    ram_mib: int


@dataclass(frozen=True)
class Scenario:
    id: str
    base: Base
    description: str
    planned: int | None
    max_skipped: int

    @property
    def profile(self) -> str:
        return self.base.profile

    @property
    def runnable(self) -> bool:
        return self.planned is not None


@dataclass(frozen=True)
class Manifest:
    ttl_upgrade_seconds: int
    ttl_rebuild_seconds: int
    bases: Mapping[str, Base]
    scenarios: Mapping[str, Scenario]


@dataclass(frozen=True)
class Judgement:
    verdict: str
    reason: str


def _is_int(value: object) -> bool:
    # bool is an int subclass; `planned: true` must not read as 1.
    return isinstance(value, int) and not isinstance(value, bool)


def _positive_int(mapping: Mapping, key: str, where: str) -> int:
    value = mapping.get(key)
    if not _is_int(value) or value <= 0:
        raise ManifestError(f"{where}: {key} must be a positive integer, got {value!r}")
    return value


def _require_keys(mapping: Mapping, expected: frozenset, where: str) -> None:
    missing = sorted(expected - set(mapping))
    unknown = sorted(set(mapping) - expected)
    if missing:
        raise ManifestError(f"{where}: missing {', '.join(missing)}")
    if unknown:
        raise ManifestError(f"{where}: unknown key(s) {', '.join(unknown)}")


def _identifier(raw: object, where: str) -> str:
    if not isinstance(raw, str) or not ARGUMENT_RE.match(raw):
        raise ManifestError(
            f"{where}: {raw!r} does not match the argument grammar {ARGUMENT_RE.pattern}"
        )
    if raw in DENY_LIST:
        raise ManifestError(f"{where}: {raw!r} is on the bridge deny list and could never be requested")
    return raw


def _parse_base(key: str, raw: object, fedora_version: int) -> Base:
    where = f"base {key!r}"
    if not isinstance(raw, Mapping):
        raise ManifestError(f"{where}: must be a mapping")
    _require_keys(raw, _BASE_KEYS, where)
    kind = raw["kind"]
    if kind not in KINDS:
        raise ManifestError(f"{where}: kind {kind!r} is not one of {sorted(KINDS)}")
    profile = raw["profile"]
    if profile not in PROFILES:
        raise ManifestError(f"{where}: profile {profile!r} is not one of {sorted(PROFILES)}")
    return Base(
        key=key,
        name=f"{key}-{fedora_version}",
        kind=kind,
        profile=profile,
        vcpus=_positive_int(raw, "vcpus", where),
        ram_mib=_positive_int(raw, "ram_mib", where),
    )


def _parse_scenario(scenario_id: str, raw: object, bases: Mapping[str, Base]) -> Scenario:
    where = f"scenario {scenario_id!r}"
    if not isinstance(raw, Mapping):
        raise ManifestError(f"{where}: must be a mapping")
    _require_keys(raw, _SCENARIO_KEYS, where)

    base = bases.get(raw["base"])
    if base is None:
        raise ManifestError(f"{where}: base {raw['base']!r} is not declared in vm_test_bases")

    description = raw["description"]
    if not isinstance(description, str) or not description.strip():
        raise ManifestError(f"{where}: description must be a non-empty string")

    planned = raw["planned"]
    if planned is not None and (not _is_int(planned) or planned <= 0):
        raise ManifestError(
            f"{where}: planned must be a positive integer, or null while the guest "
            f"script has not declared its check count; got {planned!r}"
        )

    max_skipped = raw["max_skipped"]
    if not _is_int(max_skipped) or max_skipped < 0:
        raise ManifestError(f"{where}: max_skipped must be a non-negative integer, got {max_skipped!r}")
    # §6.6 rule 03 needs passed >= 1, so the cap must leave room for it.
    if planned is not None and max_skipped >= planned:
        raise ManifestError(
            f"{where}: max_skipped ({max_skipped}) must be below planned ({planned}); "
            "at least one check has to pass"
        )

    return Scenario(
        id=scenario_id,
        base=base,
        description=description.strip(),
        planned=planned,
        max_skipped=max_skipped,
    )


def parse_manifest(document: object, fedora_version: int) -> Manifest:
    """Validate a decoded manifest and resolve every scenario's base."""
    if not isinstance(document, Mapping):
        raise ManifestError(f"manifest must be a mapping, got {type(document).__name__}")
    _require_keys(document, _TOP_LEVEL_KEYS, "manifest")

    ttl_upgrade_days = _positive_int(document, "vm_test_ttl_upgrade_days", "manifest")
    ttl_rebuild_days = _positive_int(document, "vm_test_ttl_rebuild_days", "manifest")
    if ttl_rebuild_days <= ttl_upgrade_days:
        raise ManifestError(
            f"manifest: vm_test_ttl_rebuild_days ({ttl_rebuild_days}) must exceed "
            f"vm_test_ttl_upgrade_days ({ttl_upgrade_days}); a rebuild backstop shorter than "
            "the refresh backstop turns every refresh into a reinstall"
        )

    raw_bases = document["vm_test_bases"]
    if not isinstance(raw_bases, Mapping) or not raw_bases:
        raise ManifestError("manifest: vm_test_bases must be a non-empty mapping")
    bases = {
        _identifier(key, "vm_test_bases"): _parse_base(key, raw, fedora_version)
        for key, raw in raw_bases.items()
    }

    raw_scenarios = document["vm_test_scenarios"]
    if not isinstance(raw_scenarios, Mapping) or not raw_scenarios:
        raise ManifestError("manifest: vm_test_scenarios must be a non-empty mapping")
    scenarios = {
        _identifier(scenario_id, "vm_test_scenarios"): _parse_scenario(scenario_id, raw, bases)
        for scenario_id, raw in raw_scenarios.items()
    }

    used = {scenario.base.key for scenario in scenarios.values()}
    orphans = sorted(set(bases) - used)
    if orphans:
        raise ManifestError(
            f"manifest: base(s) {', '.join(repr(o) for o in orphans)} are declared but no "
            "scenario uses them; they would be built and kept for nothing"
        )

    return Manifest(
        ttl_upgrade_seconds=ttl_upgrade_days * SECONDS_PER_DAY,
        ttl_rebuild_seconds=ttl_rebuild_days * SECONDS_PER_DAY,
        bases=bases,
        scenarios=scenarios,
    )


def load_manifest(text: str, fedora_version: int) -> Manifest:
    """Parse the JSON form of the manifest, as rendered onto the host."""
    try:
        document = json.loads(text)
    except json.JSONDecodeError as exc:
        raise ManifestError(f"manifest is not valid JSON: {exc}") from exc
    return parse_manifest(document, fedora_version)


def allowlist(manifest: Manifest) -> tuple[str, ...]:
    """The scenario ids `run-scenario` may name: runnable ones only, sorted."""
    return tuple(sorted(s.id for s in manifest.scenarios.values() if s.runnable))


def allowlist_text(manifest: Manifest) -> str:
    """The deployed `scenarios.allowlist`: one id per line. Empty is an error."""
    ids = allowlist(manifest)
    if not ids:
        raise ManifestError(
            "no scenario is runnable (none has a planned check count); refusing to "
            "produce an empty allowlist that would look like a lab with no scenarios"
        )
    return "".join(f"{scenario_id}\n" for scenario_id in ids)


def judge_checks(
    *,
    planned: int,
    total: int | None,
    passed: int | None,
    failed: int | None,
    skipped: int | None,
    max_skipped: int,
) -> Judgement:
    """§6.6 rule 03, as one total function over the counters.

    `fail` means the product ran and an assertion did not hold; it outranks
    every `error` condition because it is the more informative verdict. `error`
    covers everything else that is not a proven pass: unfinished counters, a
    harness that died early, counters that do not add up, nothing asserted, or
    more skipped than the scenario allows.
    """
    counters = {"total": total, "passed": passed, "failed": failed, "skipped": skipped}
    unfinished = sorted(name for name, value in counters.items() if value is None)
    if unfinished:
        return Judgement(VERDICT_ERROR, f"run did not finish: {', '.join(unfinished)} never written")
    negative = sorted(name for name, value in counters.items() if value < 0)
    if negative:
        return Judgement(VERDICT_ERROR, f"negative counter(s): {', '.join(negative)}")

    if failed > 0:
        return Judgement(VERDICT_FAIL, f"{failed} of {total} checks failed")

    if total != planned:
        return Judgement(
            VERDICT_ERROR,
            f"planned {planned} checks but total {total} ran; the harness did not complete",
        )
    if passed + skipped != total:
        return Judgement(
            VERDICT_ERROR,
            f"counters do not add up: passed {passed} + skipped {skipped} != total {total}",
        )
    if passed < 1:
        return Judgement(VERDICT_ERROR, "passed 0 checks; a run that asserted nothing cannot pass")
    if skipped > max_skipped:
        return Judgement(
            VERDICT_ERROR,
            f"{skipped} skipped exceeds max_skipped {max_skipped}; the run stopped asserting",
        )
    return Judgement(VERDICT_PASS, f"{passed} passed, {skipped} skipped, {failed} failed of {planned} planned")
