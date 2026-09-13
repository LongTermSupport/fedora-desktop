"""Gate a run on the base's freshness (thin executor for helpers.vmtest.freshness).

Reads the base's `base.json`, the probe's marker lines, the manifest and two
facts only the caller has (the current recipe digest and the base disk's
sha256), recomputes the base's LIVE media identity from what upstream says
today, and prints the §4.4 verdict. `vmtest run` consults this before cloning
an overlay (DESIGN.md §9 T6.1).

    python3 -m helpers.vmtest.freshness_gate --fedora-version 44 --manifest scenarios.json \\
        --base-json bases/<name>/base.json --probe probe.txt \\
        --recipe-digest SHA256 --actual-base-sha256 SHA256 [--now EPOCH]

Markers (stdout):
    VMTEST-BASE-RECORD name=… kind=… profile=… compose_label=… revision=… refresh_state=…
    VMTEST-FRESHNESS-VERDICT decision=…<TAB>degraded=…<TAB>divergences=a,b<TAB>reason=…
Exit: 0 when the run may proceed (`current` or `refresh` — a refresh is applied
after the run, §4.4a); 1 when it may not (`reinstall`, `unknown`); 2 when the
inputs themselves are unusable (no verdict is printed).
"""

from __future__ import annotations

import argparse
import pathlib
import sys
import time

from helpers.vmtest import basejson, freshness, scenarios, upstream

ARCH = "x86_64"


class GateInputError(ValueError):
    """The gate's inputs are not a coherent picture of one base; no verdict is possible."""


def _parse_probe(text: str) -> dict:
    """The probe's markers as data; UNREADABLE lines become `Unreadable` values."""
    facts: dict = {"revision": None, "bodhi": None, "artefacts": {}, "trees": {}, "unreadable": {}}
    for line in text.splitlines():
        fields = line.split(" ")
        marker = fields[0]
        if marker == "VMTEST-FRESHNESS-REVISION":
            facts["revision"] = int(fields[1])
        elif marker == "VMTEST-FRESHNESS-BODHI":
            facts["bodhi"] = fields[2]
        elif marker == "VMTEST-FRESHNESS-ARTEFACT":
            base_name, filename, sha = fields[1], fields[2], fields[3]
            extra = dict(f.split("=", 1) for f in fields[4:])
            facts["artefacts"].setdefault(base_name, []).append((filename, sha, extra["label"]))
        elif marker == "VMTEST-FRESHNESS-TREE-CHECKSUM":
            facts["trees"].setdefault(fields[1], {})[fields[2]] = fields[3]
        elif marker == "VMTEST-FRESHNESS-UNREADABLE":
            signal, url = fields[1], fields[2]
            facts["unreadable"][signal] = freshness.Unreadable(url=url, error=" ".join(fields[3:]))
    return facts


def _live_identity(record: basejson.BaseRecord, base: scenarios.Base, facts: dict, recipe_digest: str):
    """Recompute the identity from upstream's current answer, or say which signal blocked it."""
    if "artefacts" in facts["unreadable"]:
        return facts["unreadable"]["artefacts"]
    per_base = facts["unreadable"].get(f"artefact {record.name}")
    if per_base is not None:
        return per_base
    listed = facts["artefacts"].get(record.name)
    if not listed:
        raise GateInputError(f"the probe printed no artefact for {record.name}")
    labels = {label for _, _, label in listed}
    if len(labels) != 1:
        raise GateInputError(f"the probe lists {record.name} artefacts under several compose labels: {sorted(labels)}")
    treeinfo = None
    if base.tree is not None:
        blocked = facts["unreadable"].get(f"tree:{base.tree}")
        if blocked is not None:
            return blocked
        treeinfo = facts["trees"].get(base.tree)
        if not treeinfo:
            raise GateInputError(f"the probe printed no checksums for tree {base.tree}")
    fingerprint = upstream.BaseFingerprint(
        base_name=record.name,
        base_kind=record.kind,
        compose_label=labels.pop(),
        artefacts=tuple(upstream.ArtefactRef(name, sha) for name, sha, _ in sorted(listed)),
        treeinfo_checksums=treeinfo,
        recipe_digest=recipe_digest,
    )
    return upstream.artefact_identity(fingerprint)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--fedora-version", type=int, required=True)
    parser.add_argument("--manifest", type=pathlib.Path, required=True)
    parser.add_argument("--base-json", type=pathlib.Path, required=True)
    parser.add_argument("--probe", type=pathlib.Path, required=True, help="the probe's marker lines")
    parser.add_argument("--recipe-digest", required=True, help="sha256 of the CURRENT build recipe")
    parser.add_argument("--actual-base-sha256", required=True, help="sha256 the caller established for base.qcow2")
    parser.add_argument("--now", type=int, default=None, help="epoch seconds (default: the clock)")
    parser.add_argument(
        "--record-only",
        action="store_true",
        help="print the VMTEST-BASE-RECORD marker and stop; no verdict (the caller needs the record's "
        "size/mtime/sha256 before it can say what the disk's actual sha256 is)",
    )
    args = parser.parse_args(argv)

    try:
        manifest = scenarios.load_manifest(args.manifest.read_text(encoding="utf-8"), args.fedora_version)
        record = basejson.parse_record(args.base_json.read_text(encoding="utf-8"))
        facts = _parse_probe(args.probe.read_text(encoding="utf-8"))
    except (OSError, scenarios.ManifestError) as exc:
        print(f"ERROR: manifest: {exc}", file=sys.stderr)
        return 2
    except basejson.BaseRecordError as exc:
        print(f"ERROR: base.json: {exc}", file=sys.stderr)
        return 2
    except (ValueError, IndexError, KeyError) as exc:
        print(f"ERROR: probe output: {exc}", file=sys.stderr)
        return 2

    base = next((b for b in manifest.bases.values() if b.name == record.name), None)
    if base is None or base.kind != record.kind or base.profile != record.profile:
        print(
            f"ERROR: base.json describes {record.name} ({record.kind}, {record.profile}) but the manifest "
            "declares no such base with that kind and profile",
            file=sys.stderr,
        )
        return 2

    print(
        f"VMTEST-BASE-RECORD name={record.name} kind={record.kind} profile={record.profile} "
        f"compose_label={record.compose_label} revision={record.last_upgraded_revision} "
        f"refresh_state={record.refresh_state} base_sha256={record.base_sha256} "
        f"base_size={record.base_size} base_mtime={record.base_mtime}"
    )
    if args.record_only:
        return 0

    # The live identity is computed with the RECORD's recipe digest so it compares
    # media only; a changed recipe is reported by the policy's own recipe check,
    # not disguised as "the media changed".
    try:
        live_identity = _live_identity(record, base, facts, record.recipe_digest)
    except (GateInputError, upstream.UpstreamParseError) as exc:
        print(f"ERROR: cannot establish the live identity: {exc}", file=sys.stderr)
        return 2

    if facts["revision"] is not None:
        probe_revision = facts["revision"]
    else:
        probe_revision = facts["unreadable"].get(
            "revision", freshness.Unreadable(url="(revision)", error="the probe printed neither a revision nor an unreadable line")
        )
    if facts["bodhi"] is not None:
        bodhi_state = facts["bodhi"]
    else:
        bodhi_state = facts["unreadable"].get(
            "bodhi", freshness.Unreadable(url="(bodhi)", error="the probe printed neither a state nor an unreadable line")
        )

    try:
        verdict = freshness.decide(
            freshness.FreshnessInputs(
                stored_identity=record.artefact_identity,
                live_identity=live_identity,
                stored_recipe_digest=record.recipe_digest,
                current_recipe_digest=args.recipe_digest,
                stored_base_sha256=record.base_sha256,
                actual_base_sha256=args.actual_base_sha256,
                last_upgraded_revision=record.last_upgraded_revision,
                probe_revision=probe_revision,
                installed_at=record.installed_at,
                last_upgraded_at=record.last_upgraded_at,
                now=args.now if args.now is not None else int(time.time()),
                ttl_upgrade_seconds=manifest.ttl_upgrade_seconds,
                ttl_rebuild_seconds=manifest.ttl_rebuild_seconds,
                bodhi_state=bodhi_state,
            )
        )
    except freshness.FreshnessInputError as exc:
        print(f"ERROR: freshness inputs: {exc}", file=sys.stderr)
        return 2

    for warning in verdict.warnings:
        print(f"WARNING: {warning}", file=sys.stderr)
    print(
        "VMTEST-FRESHNESS-VERDICT "
        f"decision={verdict.decision}\tdegraded={'true' if verdict.degraded else 'false'}\t"
        f"divergences={','.join(verdict.divergences)}\treason={' '.join(verdict.reason.split())}"
    )
    return 0 if verdict.decision in (freshness.DECISION_CURRENT, freshness.DECISION_REFRESH) else 1


if __name__ == "__main__":
    sys.exit(main())
