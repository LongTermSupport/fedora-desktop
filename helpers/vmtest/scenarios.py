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
  early if `planned` was a guess (§6.6 rule 02);
- a `host_only` scenario is runnable from the host CLI and absent from the
  bridge's enumeration (§10, Plan 00121). "The host may run it" and "the
  sandbox may ask for it" are two properties, and a scenario that handles a
  real credential has the first without the second.
"""

from __future__ import annotations

import json
import re
from collections.abc import Mapping
from dataclasses import dataclass

from helpers.vmtest.upstream import ArtefactSelector

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
_BASE_KEYS = frozenset({"kind", "profile", "tree", "artefacts", "vcpus", "ram_mib"})
_ARTEFACT_KEYS = frozenset({"variant", "subvariant", "prefix", "suffix"})

# An install tree is a variant directory under releases/<v>/ — `Server`,
# `Everything` — and nothing that could walk elsewhere.
_TREE_RE = re.compile(r"^[A-Z][A-Za-z]+$")
_SCENARIO_KEYS = frozenset({"base", "description", "planned", "max_skipped"})
_SCENARIO_OPTIONAL_KEYS = frozenset({"run_env", "host_only"})

# The RUN_BASH_* knobs a scenario may set. A closed list: the negative
# scenarios steer the profile and the optional-play list, and nothing that
# carries a secret or reaches outside run.bash's documented contract.
RUN_ENV_KEYS = frozenset({"RUN_BASH_PROVISIONING_PROFILE", "RUN_BASH_OPTIONAL_PLAYBOOKS", "RUN_BASH_REBOOT"})
# Values travel through an SSH command line; a plain token is all they need.
_RUN_ENV_VALUE_RE = re.compile(r"^[A-Za-z0-9._,:/=-]+$")


class ManifestError(ValueError):
    """The manifest is not a coherent description of the lab; nothing is deployed from it."""


@dataclass(frozen=True)
class Base:
    """A base and what it is built from (§3.5, §4.3).

    `tree` is the ONE install tree an Anaconda (`full`) base installs from, and
    None for a `fast` base, which is imported from a published image and never
    runs Anaconda. `artefacts` selects the published artefact(s) whose hashes
    make up the base's media identity.
    """

    key: str
    name: str
    kind: str
    profile: str
    tree: str | None
    artefacts: tuple[ArtefactSelector, ...]
    vcpus: int
    ram_mib: int


@dataclass(frozen=True)
class Scenario:
    id: str
    base: Base
    description: str
    planned: int | None
    max_skipped: int
    run_env: Mapping[str, str]
    host_only: bool = False

    @property
    def profile(self) -> str:
        return self.base.profile

    @property
    def runnable(self) -> bool:
        return self.planned is not None

    @property
    def bridge_reachable(self) -> bool:
        """Whether the sandbox may name this scenario at all.

        Not the same question as `runnable`. A scenario that handles a real
        credential has to declare a check count — otherwise the run where a
        secret was in play is the one run whose evidence cannot be judged — but
        it must never appear in the enumeration the bridge offers the sandbox.
        """
        return self.runnable and not self.host_only


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


def _require_keys(mapping: Mapping, expected: frozenset, where: str, optional: frozenset = frozenset()) -> None:
    missing = sorted(expected - set(mapping))
    unknown = sorted(set(mapping) - expected - optional)
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


def _parse_tree(kind: str, raw: object, where: str) -> str | None:
    if kind == KIND_FAST:
        if raw is not None:
            raise ManifestError(
                f"{where}: a {KIND_FAST!r} base is imported from a published image and has no "
                f"install tree, but tree is {raw!r}"
            )
        return None
    if not isinstance(raw, str) or not _TREE_RE.match(raw):
        raise ManifestError(
            f"{where}: a {KIND_FULL!r} base installs from exactly one tree; tree must be a "
            f"variant name such as Server or Everything, got {raw!r}"
        )
    return raw


def _parse_artefacts(raw: object, where: str) -> tuple[ArtefactSelector, ...]:
    if not isinstance(raw, list) or not raw:
        raise ManifestError(f"{where}: artefacts must be a non-empty list of selectors")
    selectors: list[ArtefactSelector] = []
    for position, entry in enumerate(raw):
        entry_where = f"{where} artefacts[{position}]"
        if not isinstance(entry, Mapping):
            raise ManifestError(f"{entry_where}: must be a mapping")
        _require_keys(entry, _ARTEFACT_KEYS, entry_where)
        fields = {}
        for field in sorted(_ARTEFACT_KEYS):
            value = entry[field]
            if not isinstance(value, str) or not value:
                raise ManifestError(f"{entry_where}: {field} must be a non-empty string")
            fields[field] = value
        selector = ArtefactSelector(**fields)
        if selector in selectors:
            raise ManifestError(f"{where}: the same artefact selector is listed twice: {selector}")
        selectors.append(selector)
    return tuple(selectors)


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
        tree=_parse_tree(kind, raw["tree"], where),
        artefacts=_parse_artefacts(raw["artefacts"], where),
        vcpus=_positive_int(raw, "vcpus", where),
        ram_mib=_positive_int(raw, "ram_mib", where),
    )


def _parse_run_env(raw: object, where: str) -> dict[str, str]:
    if raw is None:
        return {}
    if not isinstance(raw, Mapping):
        raise ManifestError(f"{where}: run_env must be a mapping of RUN_BASH_* names to values")
    env: dict[str, str] = {}
    for key, value in raw.items():
        if key not in RUN_ENV_KEYS:
            raise ManifestError(f"{where}: run_env key {key!r} is not one of {sorted(RUN_ENV_KEYS)}")
        if not isinstance(value, str) or not _RUN_ENV_VALUE_RE.match(value):
            raise ManifestError(
                f"{where}: run_env {key} must be a plain token matching {_RUN_ENV_VALUE_RE.pattern}, got {value!r}"
            )
        env[key] = value
    return dict(sorted(env.items()))


def _parse_scenario(scenario_id: str, raw: object, bases: Mapping[str, Base]) -> Scenario:
    where = f"scenario {scenario_id!r}"
    if not isinstance(raw, Mapping):
        raise ManifestError(f"{where}: must be a mapping")
    _require_keys(raw, _SCENARIO_KEYS, where, _SCENARIO_OPTIONAL_KEYS)

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

    # Strictly a bool. A truthy string would read as "yes" here and as an error
    # nowhere, so `host_only: "false"` would quietly enumerate a credential-bearing
    # scenario to the sandbox — the one mistake this flag exists to prevent.
    host_only = raw.get("host_only", False)
    if not isinstance(host_only, bool):
        raise ManifestError(
            f"{where}: host_only must be true or false, got {host_only!r}; anything else "
            "would decide whether a credential-bearing scenario is offered to the sandbox "
            "by accident"
        )

    return Scenario(
        id=scenario_id,
        base=base,
        description=description.strip(),
        planned=planned,
        max_skipped=max_skipped,
        run_env=_parse_run_env(raw.get("run_env"), where),
        host_only=host_only,
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
    """The scenario ids the BRIDGE may name: runnable and not host-only, sorted."""
    return tuple(sorted(s.id for s in manifest.scenarios.values() if s.bridge_reachable))


def allowlist_text(manifest: Manifest) -> str:
    """The deployed `scenarios.allowlist`: one id per line. Empty is an error."""
    ids = allowlist(manifest)
    if not ids:
        raise ManifestError(
            "no scenario is reachable from the bridge (none has a planned check count, or "
            "every one is host_only); refusing to produce an empty allowlist that would "
            "look like a lab with no scenarios"
        )
    return "".join(f"{scenario_id}\n" for scenario_id in ids)


def host_only_list(manifest: Manifest) -> tuple[str, ...]:
    """The scenario ids only the HOST CLI may name: runnable and host-only, sorted.

    Derived from the same flag as `allowlist`, so the two are disjoint by
    construction. Two hand-maintained lists would agree until the day one of
    them was edited, and the day that mattered would be the one where a
    credential-bearing scenario appeared on both.
    """
    return tuple(sorted(s.id for s in manifest.scenarios.values() if s.runnable and s.host_only))


def host_only_text(manifest: Manifest) -> str:
    """The deployed `scenarios.host-only`: one id per line, possibly empty.

    Empty is NOT an error here, unlike the bridge allowlist: a lab with no
    credential-bearing scenario is the ordinary case and the safer one.
    """
    return "".join(f"{scenario_id}\n" for scenario_id in host_only_list(manifest))


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
